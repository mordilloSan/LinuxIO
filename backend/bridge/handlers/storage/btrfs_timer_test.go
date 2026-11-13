package storage

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

type recordingBtrfsTimerOps struct {
	calls         []string
	unitFileState string
	activeState   string // of the replication unit; "" is inactive
}

func (r *recordingBtrfsTimerOps) ops() btrfsTimerOps {
	record := func(verb string) func(context.Context, string) error {
		return func(_ context.Context, unit string) error {
			r.calls = append(r.calls, verb+":"+unit)
			return nil
		}
	}
	return btrfsTimerOps{
		daemonReload: func(context.Context) error {
			r.calls = append(r.calls, "reload")
			return nil
		},
		disableUnit:      record("disable"),
		enableUnit:       record("enable"),
		startUnit:        record("start"),
		stopUnit:         record("stop"),
		getUnitFileState: func(context.Context, string) (string, error) { return r.unitFileState, nil },
		getActiveState: func(_ context.Context, unit string) (string, error) {
			r.calls = append(r.calls, "state:"+unit)
			return cmp.Or(r.activeState, "inactive"), nil
		},
	}
}

func testBtrfsTimerPaths(t *testing.T) btrfsTimerPaths {
	t.Helper()
	dir := t.TempDir()
	return btrfsTimerPaths{
		unitPath:      filepath.Join(dir, btrfs.SnapshotServiceName),
		timerPath:     filepath.Join(dir, btrfs.SnapshotTimerName),
		replicatePath: filepath.Join(dir, btrfs.ReplicateServiceName),
	}
}

func assertBtrfsUnitFiles(t *testing.T, paths btrfsTimerPaths) {
	t.Helper()
	for path, want := range map[string][]byte{paths.unitPath: btrfs.RenderUnit(), paths.timerPath: btrfs.RenderTimer()} {
		got, err := os.ReadFile(path)
		if err != nil || string(got) != string(want) {
			t.Errorf("%s = %q, %v; want %q", path, got, err, want)
		}
	}
}

func TestSyncBtrfsTimerEnablesWhenRuleEnabled(t *testing.T) {
	rec := &recordingBtrfsTimerOps{}
	paths := testBtrfsTimerPaths(t)
	rules := []btrfs.BtrfsSnapshotRule{{UUID: "u", Path: "@"}, {UUID: "u", Path: "@home", Enabled: true}}
	if err := syncBtrfsTimer(context.Background(), rec.ops(), paths, rules, nil); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	assertBtrfsUnitFiles(t, paths)
	assertReplicateUnit(t, paths, false)
	want := []string{"reload", "enable:" + btrfs.SnapshotTimerName, "start:" + btrfs.SnapshotTimerName}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("calls = %v, want %v", rec.calls, want)
	}
}

func TestSyncBtrfsTimerDisablesWhenNone(t *testing.T) {
	rec := &recordingBtrfsTimerOps{}
	paths := testBtrfsTimerPaths(t)
	jobs := []btrfs.BtrfsReplicationJob{{ID: "a", UUID: "u", Path: "@"}}
	if err := syncBtrfsTimer(context.Background(), rec.ops(), paths, []btrfs.BtrfsSnapshotRule{{UUID: "u", Path: "@"}}, jobs); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	assertBtrfsUnitFiles(t, paths)
	assertReplicateUnit(t, paths, false)
	want := []string{"reload", "stop:" + btrfs.SnapshotTimerName, "disable:" + btrfs.SnapshotTimerName}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("calls = %v, want %v", rec.calls, want)
	}
}

// assertReplicateUnit checks the replication unit file is written as this
// build renders it, or absent.
func assertReplicateUnit(t *testing.T, paths btrfsTimerPaths, written bool) {
	t.Helper()
	got, err := os.ReadFile(paths.replicatePath)
	switch {
	case written && (err != nil || string(got) != string(btrfs.RenderReplicateUnit())):
		t.Errorf("replication unit = %q, %v; want %q", got, err, btrfs.RenderReplicateUnit())
	case !written && !errors.Is(err, fs.ErrNotExist):
		t.Errorf("replication unit = %q, %v; want none", got, err)
	}
}

// The timer's run starts the replication, so an enabled job alone keeps it
// running, and the replication unit is written beside the snapshot units.
func TestSyncBtrfsTimerEnablesWhenJobEnabled(t *testing.T) {
	rec := &recordingBtrfsTimerOps{}
	paths := testBtrfsTimerPaths(t)
	rules := []btrfs.BtrfsSnapshotRule{{UUID: "u", Path: "@"}}
	jobs := []btrfs.BtrfsReplicationJob{{ID: "a", UUID: "u", Path: "@", Enabled: true}}
	if err := syncBtrfsTimer(context.Background(), rec.ops(), paths, rules, jobs); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	assertBtrfsUnitFiles(t, paths)
	assertReplicateUnit(t, paths, true)
	want := []string{"reload", "enable:" + btrfs.SnapshotTimerName, "start:" + btrfs.SnapshotTimerName}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("calls = %v, want %v", rec.calls, want)
	}

	// A stale file from an older build is rewritten.
	if err := os.WriteFile(paths.replicatePath, []byte("[Service]\nExecStart=/bin/old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := syncBtrfsTimer(context.Background(), rec.ops(), paths, rules, jobs); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	assertReplicateUnit(t, paths, true)
}

// Disabling the last job removes the replication unit while rules keep the
// timer running; disabling everything also stops and disables the timer.
func TestSyncBtrfsTimerRemovesTheReplicateUnitWithTheLastJob(t *testing.T) {
	ctx := context.Background()
	paths := testBtrfsTimerPaths(t)
	jobs := []btrfs.BtrfsReplicationJob{{ID: "a", UUID: "u", Path: "@", Enabled: true}}
	if err := syncBtrfsTimer(ctx, (&recordingBtrfsTimerOps{}).ops(), paths, nil, jobs); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	assertReplicateUnit(t, paths, true)

	jobs[0].Enabled = false
	rules := []btrfs.BtrfsSnapshotRule{{UUID: "u", Path: "@", Enabled: true}}
	rec := &recordingBtrfsTimerOps{}
	if err := syncBtrfsTimer(ctx, rec.ops(), paths, rules, jobs); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	assertReplicateUnit(t, paths, false)
	want := []string{"state:" + btrfs.ReplicateServiceName, "reload", "enable:" + btrfs.SnapshotTimerName, "start:" + btrfs.SnapshotTimerName}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("calls = %v, want %v", rec.calls, want)
	}

	// With the file gone there is nothing to ask systemd about.
	if err := syncBtrfsTimer(ctx, (&recordingBtrfsTimerOps{}).ops(), paths, rules, jobs); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}

	// The last rule and the last job together.
	if err := syncBtrfsTimer(ctx, (&recordingBtrfsTimerOps{}).ops(), paths, nil, []btrfs.BtrfsReplicationJob{{ID: "a", UUID: "u", Path: "@", Enabled: true}}); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	rec = &recordingBtrfsTimerOps{activeState: "failed"}
	if err := syncBtrfsTimer(ctx, rec.ops(), paths, []btrfs.BtrfsSnapshotRule{{UUID: "u", Path: "@"}}, jobs); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	assertReplicateUnit(t, paths, false)
	want = []string{"state:" + btrfs.ReplicateServiceName, "reload", "stop:" + btrfs.SnapshotTimerName, "disable:" + btrfs.SnapshotTimerName}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("calls = %v, want %v", rec.calls, want)
	}
}

// A replication still running keeps its unit file: removing it would unload
// the unit's end hook at the reload and lose the run's record.
func TestSyncBtrfsTimerKeepsTheUnitOfARunningReplication(t *testing.T) {
	ctx := context.Background()
	paths := testBtrfsTimerPaths(t)
	if err := syncBtrfsTimer(ctx, (&recordingBtrfsTimerOps{}).ops(), paths, nil, []btrfs.BtrfsReplicationJob{{ID: "a", UUID: "u", Path: "@", Enabled: true}}); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	for _, state := range []string{"activating", "active", "deactivating", "reloading", "unknown"} {
		if err := syncBtrfsTimer(ctx, (&recordingBtrfsTimerOps{activeState: state}).ops(), paths, nil, nil); err != nil {
			t.Fatalf("%s: syncBtrfsTimer: %v", state, err)
		}
		assertReplicateUnit(t, paths, true)
	}
	// The next save after the run ended removes it.
	if err := syncBtrfsTimer(ctx, (&recordingBtrfsTimerOps{}).ops(), paths, nil, nil); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	assertReplicateUnit(t, paths, false)
}

// A state systemd could not give fails the save rather than guessing.
func TestSyncBtrfsTimerFailsWhenTheReplicationStateIsUnknown(t *testing.T) {
	ctx := context.Background()
	paths := testBtrfsTimerPaths(t)
	if err := syncBtrfsTimer(ctx, (&recordingBtrfsTimerOps{}).ops(), paths, nil, []btrfs.BtrfsReplicationJob{{ID: "a", UUID: "u", Path: "@", Enabled: true}}); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	boom := errors.New("boom")
	ops := (&recordingBtrfsTimerOps{}).ops()
	ops.getActiveState = func(context.Context, string) (string, error) { return "", boom }
	if err := syncBtrfsTimer(ctx, ops, paths, nil, nil); !errors.Is(err, boom) {
		t.Fatalf("syncBtrfsTimer = %v, want %v", err, boom)
	}
	assertReplicateUnit(t, paths, true)
}

func TestBtrfsTimerEnabledMissingUnitIsDisabled(t *testing.T) {
	ops := (&recordingBtrfsTimerOps{}).ops()
	ops.getUnitFileState = func(context.Context, string) (string, error) {
		return "", godbus.Error{Name: "org.freedesktop.DBus.Error.FileNotFound", Body: []any{"No such file or directory"}}
	}
	if got, err := btrfsTimerEnabled(context.Background(), ops); err != nil || got {
		t.Fatalf("btrfsTimerEnabled(missing unit) = %v, %v; want false, nil", got, err)
	}
}

func TestBtrfsTimerEnabledReadsUnitFileState(t *testing.T) {
	for state, want := range map[string]bool{"enabled": true, "enabled-runtime": true, "disabled": false, "": false} {
		rec := &recordingBtrfsTimerOps{unitFileState: state}
		if got, err := btrfsTimerEnabled(context.Background(), rec.ops()); err != nil || got != want {
			t.Errorf("btrfsTimerEnabled(%q) = %v, %v; want %v", state, got, err, want)
		}
	}
}

func TestEnsureBtrfsHealthTimerEnablesAndStarts(t *testing.T) {
	rec := &recordingBtrfsTimerOps{}
	if err := ensureBtrfsHealthTimer(context.Background(), rec.ops()); err != nil {
		t.Fatalf("ensureBtrfsHealthTimer: %v", err)
	}
	// The degraded mount runs at boot only, so it is enabled but not started.
	if want := []string{"enable:" + btrfs.HealthTimerName, "start:" + btrfs.HealthTimerName, "enable:" + btrfs.DegradedServiceName}; !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("calls = %v, want %v", rec.calls, want)
	}
}
