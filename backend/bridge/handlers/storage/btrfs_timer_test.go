package storage

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
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

// assertOnlyTheTimer checks a save wrote the timer and no service: both
// services are package units.
func assertOnlyTheTimer(t *testing.T, timerPath string) {
	t.Helper()
	got, err := os.ReadFile(timerPath)
	if err != nil || string(got) != string(btrfs.RenderTimer()) {
		t.Errorf("%s = %q, %v; want %q", timerPath, got, err, btrfs.RenderTimer())
	}
	for _, name := range []string{btrfs.SnapshotServiceName, btrfs.ReplicateServiceName} {
		if _, err := os.Lstat(filepath.Join(filepath.Dir(timerPath), name)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("a save wrote %s: %v", name, err)
		}
	}
}

func TestSyncBtrfsTimerEnablesWhenRuleEnabled(t *testing.T) {
	rec := &recordingBtrfsTimerOps{}
	timerPath := filepath.Join(t.TempDir(), btrfs.SnapshotTimerName)
	rules := []btrfs.BtrfsSnapshotRule{{UUID: "u", Path: "@"}, {UUID: "u", Path: "@home", Enabled: true}}
	if err := syncBtrfsTimer(context.Background(), rec.ops(), timerPath, rules, nil); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	assertOnlyTheTimer(t, timerPath)
	want := []string{"reload", "enable:" + btrfs.SnapshotTimerName, "start:" + btrfs.SnapshotTimerName}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("calls = %v, want %v", rec.calls, want)
	}
}

func TestSyncBtrfsTimerDisablesWhenNone(t *testing.T) {
	rec := &recordingBtrfsTimerOps{}
	timerPath := filepath.Join(t.TempDir(), btrfs.SnapshotTimerName)
	jobs := []btrfs.BtrfsReplicationJob{{ID: "a", UUID: "u", Path: "@"}}
	if err := syncBtrfsTimer(context.Background(), rec.ops(), timerPath, []btrfs.BtrfsSnapshotRule{{UUID: "u", Path: "@"}}, jobs); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	assertOnlyTheTimer(t, timerPath)
	want := []string{"reload", "stop:" + btrfs.SnapshotTimerName, "disable:" + btrfs.SnapshotTimerName}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Fatalf("calls = %v, want %v", rec.calls, want)
	}
}

// The timer's run starts the replication, so an enabled job alone keeps it
// running.
func TestSyncBtrfsTimerEnablesWhenJobEnabled(t *testing.T) {
	rec := &recordingBtrfsTimerOps{}
	timerPath := filepath.Join(t.TempDir(), btrfs.SnapshotTimerName)
	rules := []btrfs.BtrfsSnapshotRule{{UUID: "u", Path: "@"}}
	jobs := []btrfs.BtrfsReplicationJob{{ID: "a", UUID: "u", Path: "@", Enabled: true}}
	if err := syncBtrfsTimer(context.Background(), rec.ops(), timerPath, rules, jobs); err != nil {
		t.Fatalf("syncBtrfsTimer: %v", err)
	}
	assertOnlyTheTimer(t, timerPath)
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

// Removing the package deletes the snapshot timer, which LinuxIO wrote while it ran:
// each path is in the removal lists of both packages.
func TestPackageRemovalDeletesTheSnapshotTimer(t *testing.T) {
	for _, script := range []string{"debian/postrm", "rpm/linuxio.spec"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packaging", script))
		if err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{defaultBtrfsTimerPath} {
			if !slices.Contains(strings.Fields(string(data)), path) {
				t.Errorf("packaging/%s does not delete %s", script, path)
			}
		}
	}
}
