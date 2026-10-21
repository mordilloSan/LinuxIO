package btrfs

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// Rollback step operations, in the order a plan runs them.
const (
	opSnapshotRO = "snapshot-ro"
	opRename     = "rename"
	opSnapshotRW = "snapshot-rw"
	opSetDefault = "set-default"
	opDelete     = "delete"
	opRemount    = "remount"
)

// rebootMountpoints hold the running system; a source mounted at one of them
// cannot be remounted in place.
var rebootMountpoints = []string{"/", "/usr", "/var", "/etc"}

// renamePath is swapped by tests.
var renamePath = os.Rename

// RollbackStep is one step of a rollback. From and To are top-level-relative
// subvolume paths, except remount, whose From is a mountpoint and To the
// options it is mounted again with.
type RollbackStep struct {
	Op       string
	From, To string
}

// RollbackPlan is the ordered rollback of Source to one of its snapshots.
// OldPath is where the current subvolume is renamed to; KeepOld means it is
// mounted and cannot be deleted by the rollback. SkippedMounts are bind
// mounts of a path inside Source, which are not remounted.
type RollbackPlan struct {
	Source         string
	Steps          []RollbackStep
	RebootRequired bool
	KeepOld        bool
	OldPath        string
	SkippedMounts  []string
}

// BtrfsRollbackResult reports how a rollback ended. LeftoverPath is the
// renamed former subvolume when it still exists. SkippedMounts are bind
// mounts inside the subvolume that keep showing the old one; the rollback
// takes effect there after a reboot.
type BtrfsRollbackResult struct {
	RebootRequired bool     `json:"rebootRequired"`
	LeftoverPath   *string  `json:"leftoverPath,omitempty"`
	Remounted      bool     `json:"remounted"`
	SkippedMounts  []string `json:"skippedMounts,omitempty"`
}

// PlanRollback plans rolling source back to snapshotPath. mounts are the
// mounts of source's filesystem; defaultID and sourceID are the default
// subvolume id and source's id. The snapshot is copied to a sibling of source
// first, so the only steps that move source are two back-to-back renames.
func PlanRollback(source, snapshotPath string, mounts []Mount, defaultID, sourceID int, now time.Time) (RollbackPlan, error) {
	if err := ValidateSubvolumePath(source); err != nil {
		return RollbackPlan{}, err
	}
	if snapSource, _, _, ok := ParseSnapshotPath(snapshotPath); !ok || snapSource != source {
		return RollbackPlan{}, fmt.Errorf("snapshot does not belong to subvolume %q: %q", source, snapshotPath)
	}
	stamp := now.UTC().Format(snapshotTimeLayout)
	restore := source + ".linuxio-restore-" + stamp
	plan := RollbackPlan{Source: source, OldPath: source + ".linuxio-rollback-" + stamp}
	plan.Steps = []RollbackStep{
		{Op: opSnapshotRO, From: source, To: SnapshotPath(source, now, KindPreRollback)},
		{Op: opSnapshotRW, From: snapshotPath, To: restore},
		{Op: opRename, From: source, To: plan.OldPath},
		{Op: opRename, From: restore, To: source},
	}
	if defaultID == sourceID {
		plan.Steps = append(plan.Steps, RollbackStep{Op: opSetDefault, To: source})
	}
	var remounts []RollbackStep
	for _, m := range mounts {
		if m.Subvol != source {
			continue
		}
		if m.SubPath != "" && m.SubPath != source {
			// A bind mount of a path inside source: remounting it with
			// subvol= would expose the whole subvolume there.
			plan.SkippedMounts = append(plan.SkippedMounts, m.Mountpoint)
			continue
		}
		plan.RebootRequired = plan.RebootRequired || slices.Contains(rebootMountpoints, m.Mountpoint)
		// Mount the new subvolume by path explicitly: fstab may name it by
		// subvolid, which still points at the renamed old subvolume.
		opts := slices.DeleteFunc(slices.Clone(m.Options), func(o string) bool {
			return strings.HasPrefix(o, "subvol=") || strings.HasPrefix(o, "subvolid=")
		})
		remounts = append(remounts, RollbackStep{Op: opRemount, From: m.Mountpoint, To: strings.Join(append(opts, "subvol=/"+source), ",")})
	}
	plan.KeepOld = len(remounts) > 0 || len(plan.SkippedMounts) > 0
	switch {
	case !plan.KeepOld:
		plan.Steps = append(plan.Steps, RollbackStep{Op: opDelete, From: plan.OldPath})
	case !plan.RebootRequired:
		plan.Steps = append(plan.Steps, remounts...)
	}
	return plan, nil
}

// ExecuteRollback runs plan on the filesystem uuid, calling report before each
// step. A failure before source is renamed returns a plain error; a later
// failure returns an error naming where the old subvolume was renamed to. A
// failed delete, a busy mount or a remount that does not show the new
// subvolume does not fail the rollback: the old subvolume is reported as
// LeftoverPath and Remounted stays false.
func ExecuteRollback(ctx context.Context, uuid string, plan RollbackPlan, report func(step int, msg string)) (BtrfsRollbackResult, error) {
	top, err := TopLevel(ctx, uuid)
	if err != nil {
		return BtrfsRollbackResult{}, err
	}
	if err := checkRollbackTargetsFree(top, plan); err != nil {
		return BtrfsRollbackResult{}, err
	}
	res := BtrfsRollbackResult{RebootRequired: plan.RebootRequired, SkippedMounts: plan.SkippedMounts}
	leftover := func() { res.LeftoverPath = &plan.OldPath }
	if plan.KeepOld {
		leftover()
	}
	remounted, remountFailed, restore, renamed := 0, false, "", false
	for i, step := range plan.Steps {
		report(i, stepMessage(step))
		err := runRollbackStep(ctx, uuid, top, step)
		switch {
		case err == nil && step.Op == opSnapshotRW:
			restore = step.To
		case err == nil && step.Op == opRename && !renamed:
			renamed = true
			// Past the point of no return: finish even if the caller goes away,
			// so source is not left missing.
			ctx = context.WithoutCancel(ctx)
		case err == nil && step.Op == opRemount:
			if mountShows(ctx, step.From, plan.Source) {
				remounted++
			} else {
				remountFailed = true
				leftover()
				report(i, fmt.Sprintf("%s does not show the restored subvolume; reboot to apply the rollback there", step.From))
			}
		case err == nil:
		case step.Op == opSnapshotRW:
			removeRestore(top, step.To)
			return res, err
		case !renamed:
			removeRestore(top, restore)
			return res, err
		case step.Op == opRename && step.To == plan.Source:
			// The new subvolume could not take source's name: put the old one back.
			if backErr := renamePath(filepath.Join(top, plan.OldPath), filepath.Join(top, plan.Source)); backErr != nil {
				return res, fmt.Errorf("rollback failed after renaming %s to %s: %w; renaming it back also failed: %v", plan.Source, plan.OldPath, err, backErr)
			}
			return res, fmt.Errorf("rollback failed after renaming %s to %s: %w; renamed it back, %s is unchanged and %s is left over", plan.Source, plan.OldPath, err, plan.Source, step.From)
		case step.Op == opSetDefault:
			leftover()
			return res, fmt.Errorf("rollback restored %s but could not set it as the default subvolume: %w; the old subvolume is kept at %s", plan.Source, err, plan.OldPath)
		case step.Op == opDelete:
			leftover()
			report(i, fmt.Sprintf("Could not delete %s, delete it by hand: %v", plan.OldPath, err))
		case errors.Is(err, errBusy):
			remountFailed = true
			leftover()
			report(i, fmt.Sprintf("%s is busy; the rollback takes effect there after a reboot", step.From))
		default:
			return res, fmt.Errorf("rollback failed after renaming %s to %s: %w", plan.Source, plan.OldPath, err)
		}
	}
	res.Remounted = remounted > 0 && !remountFailed
	return res, nil
}

// checkRollbackTargetsFree refuses a plan whose restore copy or old-subvolume
// name already exists, say from an earlier run: a step would fail half-way.
func checkRollbackTargetsFree(top string, plan RollbackPlan) error {
	paths := []string{plan.OldPath}
	for _, step := range plan.Steps {
		if step.Op == opSnapshotRW {
			paths = append(paths, step.To)
		}
	}
	for _, p := range paths {
		_, err := os.Lstat(filepath.Join(top, p))
		if err == nil {
			return fmt.Errorf("rollback aborted: %s already exists", p)
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("rollback aborted: check %s: %w", p, err)
		}
	}
	return nil
}

// removeRestore deletes the temporary restore copy, if it was created. It is
// best effort: the caller is already returning the error that matters. The
// delete is one ioctl, so a cancelled task does not stop it.
func removeRestore(top, restore string) {
	if restore == "" {
		return
	}
	if _, err := os.Lstat(filepath.Join(top, restore)); err != nil {
		return
	}
	_ = subvolOps.destroy(filepath.Join(top, restore))
}

// mountShows reports whether mountpoint now mounts the subvolume at source.
func mountShows(ctx context.Context, mountpoint, source string) bool {
	mounts, err := listMounts(ctx)
	if err != nil {
		return false
	}
	return slices.ContainsFunc(mounts, func(m Mount) bool { return m.Mountpoint == mountpoint && m.Subvol == source })
}

// errBusy marks an umount refused because the mount is in use.
var errBusy = errors.New("mount is busy")

func runRollbackStep(ctx context.Context, uuid, top string, step RollbackStep) error {
	switch step.Op {
	case opSnapshotRO:
		if err := os.MkdirAll(filepath.Join(top, filepath.Dir(step.To)), 0o755); err != nil {
			return fmt.Errorf("create snapshot directory for %q: %w", step.From, err)
		}
		if err := subvolOps.snapshot(filepath.Join(top, step.From), filepath.Join(top, step.To), true); err != nil {
			return fmt.Errorf("snapshot subvolume %q: %w", step.From, err)
		}
	case opSnapshotRW:
		if err := subvolOps.snapshot(filepath.Join(top, step.From), filepath.Join(top, step.To), false); err != nil {
			return fmt.Errorf("copy %q to %q: %w", step.From, step.To, err)
		}
	case opRename:
		if err := renamePath(filepath.Join(top, step.From), filepath.Join(top, step.To)); err != nil {
			return fmt.Errorf("rename subvolume %q to %q: %w", step.From, step.To, err)
		}
	case opSetDefault:
		sv, err := requireSubvolume(ctx, uuid, step.To)
		if err != nil {
			return err
		}
		if err := subvolOps.setDefault(top, sv.ID); err != nil {
			return fmt.Errorf("set default subvolume %q: %w", step.To, err)
		}
	case opDelete:
		return deleteUnmounted(ctx, uuid, step.From)
	case opRemount:
		return remountStep(ctx, uuid, step)
	default:
		return fmt.Errorf("unknown rollback step %q", step.Op)
	}
	return nil
}

// remountStep unmounts step.From and mounts the filesystem there again with
// the options in step.To, which now select the restored subvolume.
func remountStep(ctx context.Context, uuid string, step RollbackStep) error {
	mounts, err := listMounts(ctx)
	if err != nil {
		return fmt.Errorf("list btrfs mounts: %w", err)
	}
	device, err := deviceOf(mounts, uuid)
	if err != nil {
		return err
	}
	if err := unmountFS(step.From, 0); err != nil {
		if errors.Is(err, unix.EBUSY) {
			return fmt.Errorf("%w: unmount %s: %w", errBusy, step.From, err)
		}
		return fmt.Errorf("unmount %s: %w", step.From, err)
	}
	flags, data := splitMountOptions(strings.Split(step.To, ","))
	if err := mountFS(device, step.From, "btrfs", flags, data); err != nil {
		return fmt.Errorf("mount %s: %w", step.From, err)
	}
	return nil
}

func stepMessage(step RollbackStep) string {
	switch step.Op {
	case opSnapshotRO:
		return fmt.Sprintf("Snapshotting %s to %s", step.From, step.To)
	case opRename:
		return fmt.Sprintf("Renaming %s to %s", step.From, step.To)
	case opSnapshotRW:
		return fmt.Sprintf("Copying %s to %s", step.From, step.To)
	case opSetDefault:
		return fmt.Sprintf("Setting %s as the default subvolume", step.To)
	case opDelete:
		return fmt.Sprintf("Deleting %s", step.From)
	case opRemount:
		return fmt.Sprintf("Remounting %s", step.From)
	}
	return step.Op
}
