package btrfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// replicateHelperEnv carries the state directory of
// TestBtrfsReplicateIntegration to the replication its unit runs.
const replicateHelperEnv = "LINUXIO_BTRFS_REPLICATE_HELPER"

// stubReplicationState points every file a replication run reads or writes
// at dir.
func stubReplicationState(t *testing.T, dir string) {
	t.Helper()
	stubStatePaths(t, dir)
	oldJobs, oldDir, oldRules := ReplicationPath, ReplicationDir, RulesPath
	ReplicationPath = filepath.Join(dir, "btrfs-replication.json")
	ReplicationDir = filepath.Join(dir, "btrfs-replication")
	RulesPath = filepath.Join(dir, "btrfs-snapshots.json")
	t.Cleanup(func() { ReplicationPath, ReplicationDir, RulesPath = oldJobs, oldDir, oldRules })
}

// itMount creates a single-device pool on a new loop device, mounts it under
// dir and returns its UUID and mount point.
func itMount(t *testing.T, ctx context.Context, dir, name string) (string, string) {
	t.Helper()
	dev := loopDevice(t, filepath.Join(dir, name+".img"), 1<<30)
	uuid, err := CreatePool(ctx, "linuxio-it-"+name, "single", "single", []string{dev}, true, nil)
	if err != nil {
		t.Fatalf("create pool %s: %v", name, err)
	}
	mnt := filepath.Join(dir, name)
	if err := MountPool(ctx, dev, mnt, []string{"noatime"}, false); err != nil {
		t.Fatalf("mount pool %s: %v", name, err)
	}
	t.Cleanup(func() { _ = unix.Unmount(mnt, 0) })
	return uuid, mnt
}

// waitForState polls the unit's ActiveState until it is one of want, and
// returns it.
func waitForState(t *testing.T, ctx context.Context, unit string, timeout time.Duration, want ...string) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		state, err := systemd.GetActiveState(ctx, unit)
		if err != nil {
			t.Fatalf("active state of %s: %v", unit, err)
		}
		if slices.Contains(want, state) {
			return state
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s is %s after %s, want one of %v\n%s", unit, state, timeout, want, unitJournal(ctx, unit))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// unitJournal is the unit's journal, for a failure message.
func unitJournal(ctx context.Context, unit string) string {
	out, err := exec.CommandContext(ctx, "journalctl", "--no-pager", "-o", "cat", "-n", "50", "-u", unit).CombinedOutput()
	if err != nil {
		return fmt.Sprintf("journalctl: %v", err)
	}
	return string(out)
}

// mustIT fails the test with what when err is set.
func mustIT(t *testing.T, what string, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", what, err)
	}
}

// itReplicationJob builds a source and a target pool on loop devices, a
// manual snapshot of a subvolume with data on the source, and a stored local
// job that replicates it to the target. It returns the job and the snapshot.
func itReplicationJob(t *testing.T, ctx context.Context, dir string) (BtrfsReplicationJob, BtrfsSnapshot) {
	t.Helper()
	srcUUID, srcMnt := itMount(t, ctx, dir, "src")
	_, dstMnt := itMount(t, ctx, dir, "dst")
	_, err := CreateSubvolume(ctx, srcUUID, "share")
	mustIT(t, "create subvolume", err)
	mustIT(t, "write", os.WriteFile(filepath.Join(srcMnt, "share", "data.bin"), make([]byte, 32<<20), 0o644))
	snap, err := CreateSnapshot(ctx, srcUUID, "share", KindManual, time.Now())
	mustIT(t, "snapshot", err)
	job := BtrfsReplicationJob{
		ID: "aaaaaaaa-0000-4000-8000-0000000000e2", UUID: srcUUID, Path: "share",
		Target:  BtrfsReplicationTarget{Kind: TargetLocal, Path: filepath.Join(dstMnt, "copies")},
		Enabled: true, After: AfterManual,
	}
	mustIT(t, "validate job", ValidateJob(ctx, &job))
	_, err = UpdateJobs(ctx, func(jobs *[]BtrfsReplicationJob) error {
		*jobs = []BtrfsReplicationJob{job}
		return nil
	})
	mustIT(t, "store job", err)
	return job, snap
}

// itReplicateUnit is a runtime unit that runs this test binary as
// TestBtrfsReplicateUnitHelper with the state in dir. Its name matches no
// LinuxIO drop-in, so the host's own units and run records are left alone.
// The unit is stopped and removed when the test ends.
func itReplicateUnit(t *testing.T, dir string) UnitFile {
	t.Helper()
	exe, err := os.Executable()
	mustIT(t, "test binary", err)
	name := fmt.Sprintf("btrfs-it-replicate-%d.service", os.Getpid())
	u := UnitFile{
		Name: name,
		Path: filepath.Join("/run/systemd/system", name),
		// "$$" is systemd's escape for a literal "$".
		Content: fmt.Appendf(nil, "[Unit]\nDescription=LinuxIO btrfs replication integration test\n\n[Service]\nType=oneshot\nEnvironment=%s=%s\nExecStart=%q -test.run=^TestBtrfsReplicateUnitHelper$$ -test.count=1 -test.v\n",
			replicateHelperEnv, dir, exe),
	}
	t.Cleanup(func() {
		ctx := context.Background()
		_ = systemd.StopUnit(ctx, name)
		waitForState(t, ctx, name, time.Minute, "inactive", "failed")
		_ = os.Remove(u.Path)
		_ = systemd.ResetFailedUnit(ctx, name)
		_ = systemd.DaemonReload(ctx)
	})
	return u
}

// itReplicated checks the helper ran once, the job's outcome was recorded
// without an error and the target holds a fully received copy of snap.
func itReplicated(t *testing.T, ctx context.Context, dir string, job BtrfsReplicationJob, snap BtrfsSnapshot) {
	t.Helper()
	runs, err := os.ReadFile(filepath.Join(dir, "runs"))
	if err != nil || strings.Count(string(runs), "\n") != 1 {
		t.Fatalf("helper runs = %q, %v; want one", runs, err)
	}
	jobs, err := LoadJobs()
	mustIT(t, "load jobs", err)
	if len(jobs) != 1 || jobs[0].LastRun == nil || jobs[0].LastError != nil {
		t.Fatalf("recorded job = %+v, want a run without an error", jobs)
	}
	targetID, err := ensureTarget(ctx, job)
	mustIT(t, "target", err)
	remote, err := ListTargetSnapshots(ctx, job, targetID)
	mustIT(t, "list target", err)
	if !onTarget(receivedOnly(remote), snap) {
		t.Fatalf("target holds %+v, want a received copy of %s", remote, path.Base(snap.Path))
	}
}

// TestBtrfsReplicateIntegration starts a replication in a oneshot unit the
// way the snapshot pass does, through UnitFile.Start, against a running
// systemd and two pools on loop devices. It shows that the start returns
// while the replication still runs, that the next pass's start joins that
// run instead of queuing another, and that the replication then completes:
// the copy is fully received on the target and the job's outcome recorded.
// The unit runs this test binary as TestBtrfsReplicateUnitHelper, which holds
// the replication until the test releases it. Run it with
// `make test-btrfs-integration`.
func TestBtrfsReplicateIntegration(t *testing.T) {
	if os.Getenv("LINUXIO_RUN_BTRFS_INTEGRATION") != "1" {
		t.Skip("set LINUXIO_RUN_BTRFS_INTEGRATION=1 and run as root")
	}
	if os.Geteuid() != 0 {
		t.Fatal("the btrfs replication integration test needs root")
	}
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		t.Skip("systemd is not the running init")
	}
	ctx := context.Background()
	dir := t.TempDir()
	stubReplicationState(t, dir)
	job, snap := itReplicationJob(t, ctx, dir)
	u := itReplicateUnit(t, dir)

	// The pass's start returns with the replication held, so before the
	// replication could have finished.
	mustIT(t, "start", u.Start(ctx))
	waitForState(t, ctx, u.Name, 30*time.Second, "activating")
	waitForFile(t, filepath.Join(dir, "waiting"), 30*time.Second)
	first, err := systemd.GetInvocationID(ctx, u.Name)
	if err != nil || first == "" {
		t.Fatalf("invocation id = %q, %v", first, err)
	}

	// The next pass's start while the send runs joins the running start job.
	mustIT(t, "second start", u.Start(ctx))
	waitForState(t, ctx, u.Name, time.Second, "activating")
	again, err := systemd.GetInvocationID(ctx, u.Name)
	if err != nil || again != first {
		t.Fatalf("invocation after the second start = %q, %v; want %q", again, err, first)
	}

	mustIT(t, "release", os.WriteFile(filepath.Join(dir, "release"), nil, 0o600))
	if state := waitForState(t, ctx, u.Name, 5*time.Minute, "inactive", "failed"); state != "inactive" {
		t.Fatalf("replication unit ended %s\n%s", state, unitJournal(ctx, u.Name))
	}
	// A start merged into the running job does not run the unit again.
	time.Sleep(2 * time.Second)
	itReplicated(t, ctx, dir, job, snap)
}

// waitForFile waits until path exists.
func waitForFile(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		_, err := os.Stat(path)
		if err == nil {
			return
		}
		if !errors.Is(err, fs.ErrNotExist) || time.Now().After(deadline) {
			t.Fatalf("waiting for %s: %v", path, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// TestBtrfsReplicateUnitHelper is the replication the unit of
// TestBtrfsReplicateIntegration runs. It counts its runs, says it is
// waiting, holds until the test releases it, then runs the stored jobs as
// the replicate subcommand does.
func TestBtrfsReplicateUnitHelper(t *testing.T) {
	dir := os.Getenv(replicateHelperEnv)
	if dir == "" {
		t.Skip("run by TestBtrfsReplicateIntegration in its unit")
	}
	stubReplicationState(t, dir)
	f, err := os.OpenFile(filepath.Join(dir, "runs"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fmt.Fprintln(f, os.Getpid())
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
	mustIT(t, "waiting", os.WriteFile(filepath.Join(dir, "waiting"), nil, 0o600))
	waitForFile(t, filepath.Join(dir, "release"), 5*time.Minute)
	jobs, err := LoadJobs()
	if err != nil {
		t.Fatal(err)
	}
	replicated, sent, errs := RunReplication(context.Background(), jobs, nil)
	if replicated != 1 || sent != 1 || len(errs) != 0 {
		t.Fatalf("replicated = %d, sent = %d, errs = %v; want 1, 1, none", replicated, sent, errs)
	}
}
