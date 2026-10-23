package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
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
	healthTimerOnce sync.Once
)

// ensureBtrfsHealthTimerOnce enables the packaged health timer the first time
// a root bridge lists filesystems, covering pools created after install.
func ensureBtrfsHealthTimerOnce(ctx context.Context) {
	if os.Geteuid() != 0 {
		return
	}
	healthTimerOnce.Do(func() {
		if err := ensureBtrfsHealthTimer(ctx, defaultBtrfsTimerOps); err != nil {
			slog.Warn("failed to enable btrfs health timer", "error", err)
		}
	})
}

// ensureBtrfsHealthTimer keeps the health timer enabled and running.
func ensureBtrfsHealthTimer(ctx context.Context, ops btrfsTimerOps) error {
	if err := ops.enableUnit(ctx, btrfs.HealthTimerName); err != nil {
		return fmt.Errorf("enable %s: %w", btrfs.HealthTimerName, err)
	}
	if err := ops.startUnit(ctx, btrfs.HealthTimerName); err != nil {
		return fmt.Errorf("start %s: %w", btrfs.HealthTimerName, err)
	}
	return nil
}

// syncBtrfsTimer writes the snapshot units and runs the timer only while
// some rule or replication job is enabled: the timer's run does both.
func syncBtrfsTimer(ctx context.Context, ops btrfsTimerOps, paths btrfsTimerPaths, rules []btrfs.BtrfsSnapshotRule, jobs []btrfs.BtrfsReplicationJob) error {
	if err := atomicfile.Write(paths.unitPath, btrfs.RenderUnit(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", paths.unitPath, err)
	}
	if err := atomicfile.Write(paths.timerPath, btrfs.RenderTimer(), 0o644); err != nil {
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
	var dbusErr godbus.Error
	if errors.As(err, &dbusErr) && dbusErr.Name == "org.freedesktop.DBus.Error.FileNotFound" {
		return false, nil // the timer unit is written on the first rule or job
	}
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

// LoadBtrfsSnapshotRules returns the stored snapshot rules with their timer state.
func LoadBtrfsSnapshotRules(ctx context.Context) (btrfs.BtrfsSnapshotRules, error) {
	rules, err := btrfs.LoadRules()
	if err != nil {
		return btrfs.BtrfsSnapshotRules{}, err
	}
	return btrfsSnapshotRules(ctx, rules), nil
}

// SaveBtrfsSnapshotRule stores a snapshot rule and returns the updated rules.
func SaveBtrfsSnapshotRule(ctx context.Context, rule btrfs.BtrfsSnapshotRule) (btrfs.BtrfsSnapshotRules, error) {
	slog.Info("setting btrfs snapshot rule", "uuid", rule.UUID, "path", rule.Path, "enabled", rule.Enabled)
	rules, err := setBtrfsSnapshotRule(ctx, rule)
	if err != nil {
		slog.Error("failed to set btrfs snapshot rule", "uuid", rule.UUID, "path", rule.Path, "error", err)
		return btrfs.BtrfsSnapshotRules{}, err
	}
	return btrfsSnapshotRules(ctx, rules), nil
}

// DeleteBtrfsSnapshotRule removes a snapshot rule and returns the updated rules.
func DeleteBtrfsSnapshotRule(ctx context.Context, uuid, path string) (btrfs.BtrfsSnapshotRules, error) {
	slog.Info("deleting btrfs snapshot rule", "uuid", uuid, "path", path)
	rules, err := updateBtrfsSnapshotRules(ctx, func(rules *[]btrfs.BtrfsSnapshotRule) error {
		*rules = btrfs.DeleteRule(*rules, uuid, path)
		return nil
	})
	if err != nil {
		slog.Error("failed to delete btrfs snapshot rule", "uuid", uuid, "path", path, "error", err)
		return btrfs.BtrfsSnapshotRules{}, err
	}
	return btrfsSnapshotRules(ctx, rules), nil
}

// RunBtrfsSnapshotRules runs every stored snapshot rule now.
func RunBtrfsSnapshotRules(ctx context.Context) (btrfs.RunReport, error) {
	rules, err := btrfs.LoadRules()
	if err != nil {
		return btrfs.RunReport{}, err
	}
	slog.Info("running btrfs snapshot rules", "rules", len(rules))
	return btrfs.Run(ctx, rules, time.Now(), btrfs.KindTimeline)
}
