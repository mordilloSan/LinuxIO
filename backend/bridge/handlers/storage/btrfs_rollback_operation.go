package storage

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// BtrfsRollbackProgress reports the rollback step about to run.
type BtrfsRollbackProgress struct {
	Step    int    `json:"step"`
	Total   int    `json:"total"`
	Message string `json:"message"`
}

func (p BtrfsRollbackProgress) ProgressEnvelope() bridgeipc.TaskProgress {
	pct := 0
	if p.Total > 0 {
		pct = p.Step * 100 / p.Total
	}
	return bridgeipc.TaskProgress{Percentage: &pct, Phase: "rollback", Message: p.Message, Detail: p}
}

var btrfsRollbackRoutes = btrfsRollbackBindings().Routes()

func btrfsRollbackBindings() apischema.BindingSet {
	return apischema.Bindings(
		apischema.TaskRunner[apischema.BtrfsSnapshotRequest, btrfs.BtrfsRollbackResult]("storage.btrfs_snapshot_rollback", apischema.Privileged(), apischema.SessionTask(), apischema.WithTaskProgress[BtrfsRollbackProgress](), apischema.WithTaskMetadata(func(req apischema.BtrfsSnapshotRequest) bridgeipc.TaskMetadata {
			return bridgeipc.TaskMetadata{Identity: []string{req.UUID, req.SnapshotPath}, Label: "Rolling back btrfs subvolume"}
		})).Run(runBtrfsRollbackTask, bridgeipc.TaskSingletonSystem),
	)
}

func runBtrfsRollbackTask(ctx context.Context, task *bridgeipc.Task, req apischema.BtrfsSnapshotRequest) (btrfs.BtrfsRollbackResult, error) {
	source, _, _, ok := btrfs.ParseSnapshotPath(req.SnapshotPath)
	if !ok {
		return btrfs.BtrfsRollbackResult{}, fmt.Errorf("not a linuxio snapshot path: %q", req.SnapshotPath)
	}
	allMounts, err := btrfs.ListMounts(ctx)
	if err != nil {
		return btrfs.BtrfsRollbackResult{}, fmt.Errorf("list btrfs mounts: %w", err)
	}
	var mounts []btrfs.Mount
	for _, m := range allMounts {
		if m.UUID == req.UUID {
			mounts = append(mounts, m)
		}
	}
	subvols, err := btrfs.ListSubvolumes(ctx, req.UUID)
	if err != nil {
		return btrfs.BtrfsRollbackResult{}, err
	}
	// The top level (id 5) is not listed, so defaultID stays 0 when it is the
	// default and never matches the source.
	defaultID, sourceID, snapFound := 0, 0, false
	for _, sv := range subvols {
		if sv.IsDefault {
			defaultID = sv.ID
		}
		switch sv.Path {
		case source:
			sourceID = sv.ID
		case req.SnapshotPath:
			if !sv.ReadOnly {
				return btrfs.BtrfsRollbackResult{}, fmt.Errorf("snapshot %q is not read-only", req.SnapshotPath)
			}
			snapFound = true
		}
	}
	if sourceID == 0 {
		return btrfs.BtrfsRollbackResult{}, fmt.Errorf("subvolume %q not found", source)
	}
	if !snapFound {
		return btrfs.BtrfsRollbackResult{}, fmt.Errorf("snapshot %q not found", req.SnapshotPath)
	}
	if err = checkRollbackPinned(source, mounts); err != nil {
		return btrfs.BtrfsRollbackResult{}, err
	}
	plan, err := btrfs.PlanRollback(source, req.SnapshotPath, mounts, defaultID, sourceID, time.Now())
	if err != nil {
		return btrfs.BtrfsRollbackResult{}, err
	}

	slog.Info("rolling back btrfs subvolume", "uuid", req.UUID, "source", source, "snapshot", req.SnapshotPath, "steps", len(plan.Steps))
	res, err := btrfs.ExecuteRollback(ctx, req.UUID, plan, func(step int, msg string) {
		slog.Info("btrfs rollback step", "uuid", req.UUID, "source", source, "step", step+1, "total", len(plan.Steps), "message", msg)
		task.ReportProgress(BtrfsRollbackProgress{Step: step, Total: len(plan.Steps), Message: msg})
	})
	if err != nil {
		return btrfs.BtrfsRollbackResult{}, fmt.Errorf("roll back btrfs subvolume %s of %s to snapshot %s: %w", source, req.UUID, req.SnapshotPath, err)
	}
	slog.Info("rolled back btrfs subvolume", "uuid", req.UUID, "source", source, "rebootRequired", res.RebootRequired, "remounted", res.Remounted)
	return res, nil
}

// procCmdlinePath is the kernel command line; a var so tests can use a temp file.
var procCmdlinePath = "/proc/cmdline"

// checkRollbackPinned refuses a rollback when a mount of source is pinned by
// subvolid= in /etc/fstab or, for /, in the kernel rootflags: after the rename
// that id names the old subvolume, and the mount fails at boot when subvolid
// and subvol disagree.
func checkRollbackPinned(source string, mounts []btrfs.Mount) error {
	fstab := getFstabEntries()
	for _, m := range mounts {
		if m.Subvol != source {
			continue
		}
		if entry, ok := fstab[m.Mountpoint]; ok && hasSubvolIDOption(entry.options) {
			return bridgeipc.NewError(fmt.Sprintf("%s is mounted with subvolid= in /etc/fstab; replace it with subvol=/%s before rolling back", m.Mountpoint, source), 400)
		}
		if m.Mountpoint != "/" {
			continue
		}
		cmdline, err := os.ReadFile(procCmdlinePath)
		if err != nil {
			return fmt.Errorf("read kernel command line: %w", err)
		}
		for field := range strings.FieldsSeq(string(cmdline)) {
			if flags, ok := strings.CutPrefix(field, "rootflags="); ok && hasSubvolIDOption(flags) {
				return bridgeipc.NewError(fmt.Sprintf("/ is mounted with subvolid= in the kernel rootflags; replace it with subvol=/%s before rolling back", source), 400)
			}
		}
	}
	return nil
}

// hasSubvolIDOption reports whether a comma-separated mount option list
// contains a subvolid= option.
func hasSubvolIDOption(options string) bool {
	for opt := range strings.SplitSeq(options, ",") {
		if strings.HasPrefix(opt, "subvolid=") {
			return true
		}
	}
	return false
}
