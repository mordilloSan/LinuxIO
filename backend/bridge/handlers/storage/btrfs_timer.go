package storage

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

type btrfsTimerOps struct {
	daemonReload     func(context.Context) error
	disableUnit      func(context.Context, string) error
	enableUnit       func(context.Context, string) error
	getUnitFileState func(context.Context, string) (string, error)
	startUnit        func(context.Context, string) error
	stopUnit         func(context.Context, string) error
}

type btrfsTimerPaths struct {
	unitPath  string
	timerPath string
}

var (
	defaultBtrfsTimerOps = btrfsTimerOps{
		daemonReload:     systemd.DaemonReload,
		disableUnit:      systemd.DisableUnit,
		enableUnit:       systemd.EnableUnit,
		getUnitFileState: systemd.GetUnitFileState,
		startUnit:        systemd.StartUnit,
		stopUnit:         systemd.StopUnit,
	}
	defaultBtrfsTimerPaths = btrfsTimerPaths{
		unitPath:  filepath.Join(btrfs.SnapshotUnitDir, btrfs.SnapshotServiceName),
		timerPath: filepath.Join(btrfs.SnapshotUnitDir, btrfs.SnapshotTimerName),
	}
)

// syncBtrfsTimer writes the snapshot units and runs the timer only while
// some rule or replication job is enabled: the timer's run does both.
func syncBtrfsTimer(ctx context.Context, ops btrfsTimerOps, paths btrfsTimerPaths, rules []btrfs.BtrfsSnapshotRule, jobs []btrfs.BtrfsReplicationJob) error {
	if err := utils.WriteFileAtomic(paths.unitPath, btrfs.RenderUnit(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", paths.unitPath, err)
	}
	if err := utils.WriteFileAtomic(paths.timerPath, btrfs.RenderTimer(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", paths.timerPath, err)
	}
	if err := ops.daemonReload(ctx); err != nil {
		return fmt.Errorf("reload systemd: %w", err)
	}
	timer := btrfs.SnapshotTimerName
	if slices.ContainsFunc(rules, func(r btrfs.BtrfsSnapshotRule) bool { return r.Enabled }) ||
		slices.ContainsFunc(jobs, func(j btrfs.BtrfsReplicationJob) bool { return j.Enabled }) {
		if err := ops.enableUnit(ctx, timer); err != nil {
			return fmt.Errorf("enable %s: %w", timer, err)
		}
		if err := ops.startUnit(ctx, timer); err != nil {
			return fmt.Errorf("start %s: %w", timer, err)
		}
		return nil
	}
	if err := ops.stopUnit(ctx, timer); err != nil {
		return fmt.Errorf("stop %s: %w", timer, err)
	}
	if err := ops.disableUnit(ctx, timer); err != nil {
		return fmt.Errorf("disable %s: %w", timer, err)
	}
	return nil
}

func btrfsTimerEnabled(ctx context.Context, ops btrfsTimerOps) (bool, error) {
	state, err := ops.getUnitFileState(ctx, btrfs.SnapshotTimerName)
	if err != nil {
		return false, fmt.Errorf("read %s unit state: %w", btrfs.SnapshotTimerName, err)
	}
	return state == "enabled" || state == "enabled-runtime", nil
}

// btrfsSnapshotRules is the API view: the rules plus the timer state.
func btrfsSnapshotRules(ctx context.Context, rules []btrfs.BtrfsSnapshotRule) btrfs.BtrfsSnapshotRules {
	enabled, err := btrfsTimerEnabled(ctx, defaultBtrfsTimerOps)
	if err != nil {
		slog.Warn("failed to read btrfs snapshot timer state", "error", err)
	}
	return btrfs.BtrfsSnapshotRules{Rules: rules, TimerEnabled: enabled}
}

// setBtrfsSnapshotRule validates and upserts a rule for an existing subvolume.
func setBtrfsSnapshotRule(ctx context.Context, rule btrfs.BtrfsSnapshotRule) ([]btrfs.BtrfsSnapshotRule, error) {
	if err := btrfs.ValidateRule(rule); err != nil {
		return nil, err
	}
	mounts, err := btrfs.ListMounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list btrfs mounts: %w", err)
	}
	if !slices.ContainsFunc(mounts, func(m btrfs.Mount) bool { return m.UUID == rule.UUID }) {
		return nil, fmt.Errorf("btrfs filesystem %s is not mounted", rule.UUID)
	}
	subvols, err := btrfs.ListSubvolumes(ctx, rule.UUID)
	if err != nil {
		return nil, err
	}
	if !slices.ContainsFunc(subvols, func(sv btrfs.BtrfsSubvolume) bool { return sv.Path == rule.Path }) {
		return nil, fmt.Errorf("subvolume %q not found", rule.Path)
	}
	return updateBtrfsSnapshotRules(ctx, func(rules *[]btrfs.BtrfsSnapshotRule) error {
		*rules = btrfs.UpsertRule(*rules, rule)
		return nil
	})
}

// updateBtrfsSnapshotRules changes the rule file then syncs the timer.
func updateBtrfsSnapshotRules(ctx context.Context, fn func(*[]btrfs.BtrfsSnapshotRule) error) ([]btrfs.BtrfsSnapshotRule, error) {
	rules, err := btrfs.UpdateRules(ctx, fn)
	if err != nil {
		return nil, err
	}
	jobs, err := btrfs.LoadJobs()
	if err != nil {
		return nil, err
	}
	if err := syncBtrfsTimer(ctx, defaultBtrfsTimerOps, defaultBtrfsTimerPaths, rules, jobs); err != nil {
		return nil, err
	}
	return rules, nil
}

// updateBtrfsReplicationJobs changes the job file then syncs the timer.
func updateBtrfsReplicationJobs(ctx context.Context, fn func(*[]btrfs.BtrfsReplicationJob) error) ([]btrfs.BtrfsReplicationJob, error) {
	jobs, err := btrfs.UpdateJobs(ctx, fn)
	if err != nil {
		return nil, err
	}
	rules, err := btrfs.LoadRules()
	if err != nil {
		return nil, err
	}
	if err := syncBtrfsTimer(ctx, defaultBtrfsTimerOps, defaultBtrfsTimerPaths, rules, jobs); err != nil {
		return nil, err
	}
	return jobs, nil
}
