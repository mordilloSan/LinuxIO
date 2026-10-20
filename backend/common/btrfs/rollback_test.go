package btrfs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

var (
	rollbackSnap = SnapshotPath("@", snapshotTime.Add(-time.Hour), KindManual)
	rollbackOld  = "@.linuxio-rollback-20261019T101500Z"
	dataSnap     = SnapshotPath("data", snapshotTime.Add(-time.Hour), KindTimeline)
	dataOld      = "data.linuxio-rollback-20261019T101500Z"
	dataRestore  = "data.linuxio-restore-20261019T101500Z"
	dataMounts   = []Mount{{Mountpoint: "/srv/data", UUID: rootUUID, Subvol: "data", SubvolID: 302,
		Options: []string{"rw", "noatime", "space_cache=v2", "subvolid=302", "subvol=/data"}}}
	dataMountOpts = "rw,noatime,space_cache=v2,subvol=/data"
)

func TestRollbackPlanRootRequiresReboot(t *testing.T) {
	plan, err := PlanRollback("@", rollbackSnap, subvolMounts, 256, 256, snapshotTime)
	if err != nil {
		t.Fatalf("PlanRollback: %v", err)
	}
	want := RollbackPlan{
		Source: "@",
		Steps: []RollbackStep{
			{Op: "snapshot-ro", From: "@", To: "linuxio-snapshots/@/20261019T101500Z-pre-rollback"},
			{Op: "snapshot-rw", From: rollbackSnap, To: "@.linuxio-restore-20261019T101500Z"},
			{Op: "rename", From: "@", To: rollbackOld},
			{Op: "rename", From: "@.linuxio-restore-20261019T101500Z", To: "@"},
			{Op: "set-default", To: "@"},
		},
		RebootRequired: true, KeepOld: true, OldPath: rollbackOld,
	}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("plan = %+v\nwant  %+v", plan, want)
	}
}

func TestRollbackPlanDataRemounts(t *testing.T) {
	plan, err := PlanRollback("data", dataSnap, dataMounts, 5, 302, snapshotTime)
	if err != nil {
		t.Fatalf("PlanRollback: %v", err)
	}
	want := RollbackPlan{
		Source: "data",
		Steps: append(dataSteps(),
			// subvolid is dropped: it would still name the renamed old subvolume.
			RollbackStep{Op: "remount", From: "/srv/data", To: dataMountOpts}),
		KeepOld: true, OldPath: dataOld,
	}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("plan = %+v\nwant  %+v", plan, want)
	}
}

func TestRollbackPlanUnmountedDeletesOld(t *testing.T) {
	// "@" and "@home" are mounted but "data" is not: only mounts of the source
	// count (subvolMounts[3] is another filesystem).
	plan, err := PlanRollback("data", dataSnap, subvolMounts[:3], 256, 302, snapshotTime)
	if err != nil {
		t.Fatalf("PlanRollback: %v", err)
	}
	want := RollbackPlan{
		Source:  "data",
		Steps:   append(dataSteps(), RollbackStep{Op: "delete", From: dataOld}),
		OldPath: dataOld,
	}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("plan = %+v\nwant  %+v", plan, want)
	}
}

func dataSteps() []RollbackStep {
	return []RollbackStep{
		{Op: "snapshot-ro", From: "data", To: "linuxio-snapshots/data/20261019T101500Z-pre-rollback"},
		{Op: "snapshot-rw", From: dataSnap, To: dataRestore},
		{Op: "rename", From: "data", To: dataOld},
		{Op: "rename", From: dataRestore, To: "data"},
	}
}

func TestRollbackPlanRejectsForeignSnapshot(t *testing.T) {
	for _, snap := range []string{
		SnapshotPath("@home", snapshotTime, KindManual), // another subvolume's snapshot
		"linuxio-snapshots/@-2026-10-19",                // not LinuxIO-shaped
		"@home",
	} {
		_, err := PlanRollback("@", snap, subvolMounts, 256, 256, snapshotTime)
		if err == nil || !strings.Contains(err.Error(), "snapshot does not belong to subvolume") {
			t.Errorf("PlanRollback(@, %q) = %v, want foreign-snapshot refusal", snap, err)
		}
	}
	if _, err := PlanRollback("../x", SnapshotPath("../x", snapshotTime, KindManual), nil, 5, 300, snapshotTime); err == nil {
		t.Error("invalid source path accepted")
	}
}

func TestRollbackPlanRejectsSnapshotSource(t *testing.T) {
	// Snapshots are rolled back to, never rolled back themselves.
	for _, source := range []string{"linuxio-snapshots", "linuxio-snapshots/@", SnapshotPath("@", snapshotTime, KindManual)} {
		_, err := PlanRollback(source, SnapshotPath(source, snapshotTime, KindManual), nil, 5, 300, snapshotTime)
		if err == nil || !strings.Contains(err.Error(), "reserved for snapshots") {
			t.Errorf("PlanRollback(%q) = %v, want reserved-path refusal", source, err)
		}
	}
}

// stubRollback mounts the top level at a temp dir with the given extra
// mounts, lists "@" (now id 400) and "data", records mutating btrfs calls and
// renames, and runs umount/mount through script (a shell snippet per command).
type rollbackStub struct {
	top       string
	btrfs     [][]string
	renames   [][]string
	cmds      [][]string
	renameErr map[int]error // by rename call index
}

func stubRollback(t *testing.T, mounts []Mount, script map[string]string) *rollbackStub {
	t.Helper()
	s := &rollbackStub{top: t.TempDir()}
	stubMounts(t, append([]Mount{{Mountpoint: s.top, UUID: rootUUID, SubvolID: 5}}, mounts...))

	oldBtrfs := runBtrfs
	runBtrfs = func(_ context.Context, args ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(joined, "subvolume list -a -p -u -q -R "):
			return snapshotRow(-500, "@") + snapshotRow(-598, "data") + snapshotRow(-656, rollbackOld), nil
		case strings.HasPrefix(joined, "subvolume list -a "):
			return "", nil
		case strings.HasPrefix(joined, "qgroup show "):
			return "", nil // quotas on, no data
		case strings.HasPrefix(joined, "subvolume get-default "):
			return "ID 256 gen 1 top level 5 path " + rollbackOld, nil
		}
		s.btrfs = append(s.btrfs, args)
		return "", nil
	}
	oldRename := renamePath
	renamePath = func(from, to string) error {
		s.renames = append(s.renames, []string{from, to})
		return s.renameErr[len(s.renames)-1]
	}
	oldExec := execCommand
	execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		base := filepath.Base(name)
		s.cmds = append(s.cmds, append([]string{base}, args...))
		if body, ok := script[base]; ok {
			return exec.CommandContext(ctx, "sh", "-c", body)
		}
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { runBtrfs, renamePath, execCommand = oldBtrfs, oldRename, oldExec })
	return s
}

func TestRollbackPlanExecuteRoot(t *testing.T) {
	s := stubRollback(t, subvolMounts[1:3], nil)
	plan, err := PlanRollback("@", rollbackSnap, subvolMounts[:3], 256, 256, snapshotTime)
	if err != nil {
		t.Fatalf("PlanRollback: %v", err)
	}
	var reported []int
	res, err := ExecuteRollback(context.Background(), rootUUID, plan, func(step int, _ string) { reported = append(reported, step) })
	if err != nil {
		t.Fatalf("ExecuteRollback: %v", err)
	}
	top := s.top
	restore := "@.linuxio-restore-20261019T101500Z"
	wantBtrfs := [][]string{
		{"subvolume", "snapshot", "-r", filepath.Join(top, "@"), filepath.Join(top, "linuxio-snapshots/@/20261019T101500Z-pre-rollback")},
		{"subvolume", "snapshot", filepath.Join(top, rollbackSnap), filepath.Join(top, restore)},
		{"subvolume", "set-default", "400", top},
	}
	if !reflect.DeepEqual(s.btrfs, wantBtrfs) {
		t.Fatalf("btrfs calls = %v\nwant %v", s.btrfs, wantBtrfs)
	}
	wantRenames := [][]string{
		{filepath.Join(top, "@"), filepath.Join(top, rollbackOld)},
		{filepath.Join(top, restore), filepath.Join(top, "@")},
	}
	if !reflect.DeepEqual(s.renames, wantRenames) {
		t.Fatalf("renames = %v, want %v", s.renames, wantRenames)
	}
	if len(s.cmds) != 0 {
		t.Fatalf("unexpected commands: %v", s.cmds)
	}
	if want := []int{0, 1, 2, 3, 4}; !reflect.DeepEqual(reported, want) {
		t.Fatalf("reported steps = %v, want %v", reported, want)
	}
	if !res.RebootRequired || res.Remounted || res.LeftoverPath == nil || *res.LeftoverPath != rollbackOld {
		t.Fatalf("result = %+v", res)
	}
}

func TestRollbackPlanExecuteRemountsAndDeletes(t *testing.T) {
	s := stubRollback(t, dataMounts, nil)
	plan, err := PlanRollback("data", dataSnap, dataMounts, 5, 302, snapshotTime)
	if err != nil {
		t.Fatalf("PlanRollback: %v", err)
	}
	res, err := ExecuteRollback(context.Background(), rootUUID, plan, func(int, string) {})
	if err != nil {
		t.Fatalf("ExecuteRollback: %v", err)
	}
	want := [][]string{
		{"umount", "/srv/data"},
		{"mount", "-t", "btrfs", "-o", dataMountOpts, "UUID=" + rootUUID, "/srv/data"},
	}
	if !reflect.DeepEqual(s.cmds, want) {
		t.Fatalf("commands = %v, want %v", s.cmds, want)
	}
	if res.RebootRequired || !res.Remounted || res.LeftoverPath == nil || *res.LeftoverPath != dataOld {
		t.Fatalf("result = %+v", res)
	}

	// Unmounted: the renamed subvolume is deleted and nothing is left over.
	s = stubRollback(t, nil, nil)
	plan, _ = PlanRollback("data", dataSnap, nil, 5, 302, snapshotTime)
	res, err = ExecuteRollback(context.Background(), rootUUID, plan, func(int, string) {})
	if err != nil {
		t.Fatalf("ExecuteRollback unmounted: %v", err)
	}
	if last := s.btrfs[len(s.btrfs)-1]; !reflect.DeepEqual(last, []string{"subvolume", "delete", filepath.Join(s.top, dataOld)}) {
		t.Fatalf("last btrfs call = %v", last)
	}
	if res.LeftoverPath != nil || res.Remounted {
		t.Fatalf("result = %+v", res)
	}
}

func TestRollbackPlanExecuteRemountVerifiesMount(t *testing.T) {
	s := stubRollback(t, nil, nil)
	// After the remount findmnt still shows the old subvolume at /srv/data.
	stubMounts(t, []Mount{
		{Mountpoint: s.top, UUID: rootUUID, SubvolID: 5},
		{Mountpoint: "/srv/data", UUID: rootUUID, Subvol: dataOld, SubvolID: 302},
	})
	plan, _ := PlanRollback("data", dataSnap, dataMounts, 5, 302, snapshotTime)
	var msgs []string
	res, err := ExecuteRollback(context.Background(), rootUUID, plan, func(_ int, msg string) { msgs = append(msgs, msg) })
	if err != nil {
		t.Fatalf("ExecuteRollback: %v", err)
	}
	if res.Remounted || res.LeftoverPath == nil || *res.LeftoverPath != dataOld {
		t.Fatalf("result = %+v", res)
	}
	if !strings.Contains(msgs[len(msgs)-1], "does not show the restored subvolume") {
		t.Fatalf("unverified remount not reported: %v", msgs)
	}
}

func TestRollbackPlanBusyMountKeepsOld(t *testing.T) {
	s := stubRollback(t, dataMounts, map[string]string{"umount": "echo 'umount: /srv/data: target is busy.' >&2; exit 32"})
	plan, err := PlanRollback("data", dataSnap, dataMounts, 5, 302, snapshotTime)
	if err != nil {
		t.Fatalf("PlanRollback: %v", err)
	}
	var msgs []string
	res, err := ExecuteRollback(context.Background(), rootUUID, plan, func(_ int, msg string) { msgs = append(msgs, msg) })
	if err != nil {
		t.Fatalf("ExecuteRollback: %v", err)
	}
	if res.Remounted || res.RebootRequired || res.LeftoverPath == nil || *res.LeftoverPath != dataOld {
		t.Fatalf("result = %+v", res)
	}
	for _, c := range s.btrfs {
		if c[1] == "delete" {
			t.Fatalf("old subvolume deleted while mounted: %v", s.btrfs)
		}
	}
	if want := [][]string{{"umount", "/srv/data"}}; !reflect.DeepEqual(s.cmds, want) {
		t.Fatalf("commands = %v, want %v", s.cmds, want)
	}
	if !strings.Contains(strings.Join(msgs, "\n"), "busy") {
		t.Fatalf("busy mount not reported: %v", msgs)
	}
}

func TestRollbackPlanExecuteMountFailureNamesMountpointAndOldPath(t *testing.T) {
	stubRollback(t, dataMounts, map[string]string{"mount": "echo 'mount: /srv/data: wrong fs type.' >&2; exit 32"})
	plan, _ := PlanRollback("data", dataSnap, dataMounts, 5, 302, snapshotTime)
	_, err := ExecuteRollback(context.Background(), rootUUID, plan, func(int, string) {})
	if err == nil || !strings.Contains(err.Error(), "after renaming data to "+dataOld) || !strings.Contains(err.Error(), "mount /srv/data") {
		t.Fatalf("err = %v, want it to name /srv/data and %s", err, dataOld)
	}
}

func TestRollbackPlanExecuteFailuresBeforeRenameAreClean(t *testing.T) {
	plan, _ := PlanRollback("data", dataSnap, nil, 5, 302, snapshotTime)

	// The read-write copy fails: nothing is renamed.
	s := stubRollback(t, nil, nil)
	oldBtrfs := runBtrfs
	runBtrfs = func(ctx context.Context, args ...string) (string, error) {
		if len(args) == 4 && args[1] == "snapshot" {
			return "", exec.ErrNotFound
		}
		return oldBtrfs(ctx, args...)
	}
	_, err := ExecuteRollback(context.Background(), rootUUID, plan, func(int, string) {})
	if err == nil || strings.Contains(err.Error(), "after renaming") || len(s.renames) != 0 {
		t.Fatalf("copy failure: err = %v, renames = %v", err, s.renames)
	}
	runBtrfs = oldBtrfs

	// Renaming the source fails: the restore copy is deleted again.
	s = stubRollback(t, nil, nil)
	oldBtrfs = runBtrfs
	runBtrfs = func(ctx context.Context, args ...string) (string, error) {
		if len(args) == 4 && args[1] == "snapshot" {
			if mkErr := os.Mkdir(args[3], 0o755); mkErr != nil {
				return "", mkErr
			}
		}
		return oldBtrfs(ctx, args...)
	}
	s.renameErr = map[int]error{0: os.ErrPermission}
	_, err = ExecuteRollback(context.Background(), rootUUID, plan, func(int, string) {})
	if err == nil || strings.Contains(err.Error(), "after renaming") {
		t.Fatalf("rename failure err = %v", err)
	}
	if last := s.btrfs[len(s.btrfs)-1]; !reflect.DeepEqual(last, []string{"subvolume", "delete", filepath.Join(s.top, dataRestore)}) {
		t.Fatalf("restore copy not deleted: %v", s.btrfs)
	}
}

func TestRollbackPlanExecuteRenamesBackWhenRestoreRenameFails(t *testing.T) {
	plan, _ := PlanRollback("data", dataSnap, nil, 5, 302, snapshotTime)
	s := stubRollback(t, nil, nil)
	s.renameErr = map[int]error{1: os.ErrPermission}
	_, err := ExecuteRollback(context.Background(), rootUUID, plan, func(int, string) {})
	if err == nil || !strings.Contains(err.Error(), "after renaming data to "+dataOld) || !strings.Contains(err.Error(), "renamed it back") {
		t.Fatalf("err = %v", err)
	}
	top := s.top
	want := [][]string{
		{filepath.Join(top, "data"), filepath.Join(top, dataOld)},
		{filepath.Join(top, dataRestore), filepath.Join(top, "data")},
		{filepath.Join(top, dataOld), filepath.Join(top, "data")},
	}
	if !reflect.DeepEqual(s.renames, want) {
		t.Fatalf("renames = %v, want %v", s.renames, want)
	}
	for _, c := range s.btrfs {
		if c[1] == "delete" {
			t.Fatalf("deleted after failed rollback: %v", s.btrfs)
		}
	}

	// The rename back fails too: the error says so.
	s = stubRollback(t, nil, nil)
	s.renameErr = map[int]error{1: os.ErrPermission, 2: os.ErrPermission}
	if _, err = ExecuteRollback(context.Background(), rootUUID, plan, func(int, string) {}); err == nil || !strings.Contains(err.Error(), "renaming it back also failed") {
		t.Fatalf("err = %v", err)
	}
}

func TestRollbackPlanSkipsBindMountsInsideSource(t *testing.T) {
	mounts := append(slices.Clone(dataMounts), Mount{Mountpoint: "/var/www", UUID: rootUUID, Subvol: "data", SubvolID: 302, SubPath: "data/www",
		Options: []string{"rw", "subvolid=302", "subvol=/data"}})
	mounts[0].SubPath = "data"
	plan, err := PlanRollback("data", dataSnap, mounts, 5, 302, snapshotTime)
	if err != nil {
		t.Fatalf("PlanRollback: %v", err)
	}
	want := RollbackPlan{
		Source:        "data",
		Steps:         append(dataSteps(), RollbackStep{Op: "remount", From: "/srv/data", To: dataMountOpts}),
		KeepOld:       true,
		OldPath:       dataOld,
		SkippedMounts: []string{"/var/www"},
	}
	if !reflect.DeepEqual(plan, want) {
		t.Fatalf("plan = %+v\nwant  %+v", plan, want)
	}

	// Only a bind mount: nothing is remounted and the old subvolume stays.
	plan, _ = PlanRollback("data", dataSnap, mounts[1:], 5, 302, snapshotTime)
	stubRollback(t, nil, nil)
	res, err := ExecuteRollback(context.Background(), rootUUID, plan, func(int, string) {})
	if err != nil {
		t.Fatalf("ExecuteRollback: %v", err)
	}
	if !reflect.DeepEqual(res.SkippedMounts, []string{"/var/www"}) || res.Remounted || res.LeftoverPath == nil || *res.LeftoverPath != dataOld {
		t.Fatalf("result = %+v", res)
	}
}

func TestRollbackExecuteRefusesExistingPaths(t *testing.T) {
	plan, _ := PlanRollback("data", dataSnap, nil, 5, 302, snapshotTime)
	for _, existing := range []string{dataOld, dataRestore} {
		s := stubRollback(t, nil, nil)
		if err := os.Mkdir(filepath.Join(s.top, existing), 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := ExecuteRollback(context.Background(), rootUUID, plan, func(int, string) {})
		if err == nil || err.Error() != "rollback aborted: "+existing+" already exists" {
			t.Fatalf("%s: err = %v", existing, err)
		}
		if len(s.btrfs) != 0 || len(s.renames) != 0 {
			t.Fatalf("%s: btrfs %v, renames %v; want none", existing, s.btrfs, s.renames)
		}
	}
}

func TestRollbackExecuteSetDefaultFailureKeepsRestoredState(t *testing.T) {
	// "data" is the default and unmounted: the old subvolume would be deleted
	// after set-default, so a set-default failure must report it as leftover.
	plan, err := PlanRollback("data", dataSnap, nil, 302, 302, snapshotTime)
	if err != nil {
		t.Fatal(err)
	}
	s := stubRollback(t, nil, nil)
	oldBtrfs := runBtrfs
	runBtrfs = func(ctx context.Context, args ...string) (string, error) {
		if len(args) > 1 && args[1] == "set-default" {
			return "", os.ErrPermission
		}
		return oldBtrfs(ctx, args...)
	}
	res, err := ExecuteRollback(context.Background(), rootUUID, plan, func(int, string) {})
	if err == nil || !strings.Contains(err.Error(), "rollback restored data but could not set it as the default subvolume") {
		t.Fatalf("err = %v", err)
	}
	if res.LeftoverPath == nil || *res.LeftoverPath != dataOld || !strings.Contains(err.Error(), "kept at "+dataOld) {
		t.Fatalf("LeftoverPath = %v, want %s", res.LeftoverPath, dataOld)
	}
	if len(s.renames) != 2 {
		t.Fatalf("renames = %v, want the two rollback renames and no rename back", s.renames)
	}
	for _, c := range s.btrfs {
		if c[1] == "delete" {
			t.Fatalf("old subvolume deleted after failed set-default: %v", s.btrfs)
		}
	}
}
