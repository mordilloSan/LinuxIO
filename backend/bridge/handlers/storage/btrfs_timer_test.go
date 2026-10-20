package storage

import (
	"context"
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
	}
}

func testBtrfsTimerPaths(t *testing.T) btrfsTimerPaths {
	t.Helper()
	dir := t.TempDir()
	return btrfsTimerPaths{
		unitPath:  filepath.Join(dir, btrfs.SnapshotServiceName),
		timerPath: filepath.Join(dir, btrfs.SnapshotTimerName),
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
	want := []string{"reload", "stop:" + btrfs.SnapshotTimerName, "disable:" + btrfs.SnapshotTimerName}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("calls = %v, want %v", rec.calls, want)
	}
}

// The timer's run also replicates, so an enabled job alone keeps it running.
func TestSyncBtrfsTimerEnablesWhenJobEnabled(t *testing.T) {
	rec := &recordingBtrfsTimerOps{}
	paths := testBtrfsTimerPaths(t)
	rules := []btrfs.BtrfsSnapshotRule{{UUID: "u", Path: "@"}}
	jobs := []btrfs.BtrfsReplicationJob{{ID: "a", UUID: "u", Path: "@", Enabled: true}}
	if err := syncBtrfsTimer(context.Background(), rec.ops(), paths, rules, jobs); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	want := []string{"reload", "enable:" + btrfs.SnapshotTimerName, "start:" + btrfs.SnapshotTimerName}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("calls = %v, want %v", rec.calls, want)
	}
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
