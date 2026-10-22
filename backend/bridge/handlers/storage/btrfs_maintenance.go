package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// btrfsmaintenance (https://github.com/kdave/btrfsmaintenance) runs periodic
// scrub/balance/trim/defrag through systemd timers driven by one shell-style
// config file. LinuxIO edits only the period and path variables each task
// owns and then starts the package's oneshot refresh service, which
// enables or disables the matching timer and writes its OnCalendar drop-in.

const (
	btrfsMaintenanceScript      = "/usr/share/btrfsmaintenance/btrfs-scrub.sh"
	btrfsMaintenanceRefreshUnit = "btrfsmaintenance-refresh.service"
	btrfsMaintenancePeriodNone  = "none"
)

// btrfsMaintenanceConfigPaths lists the config location per distro family:
// Debian/Ubuntu first, then Fedora/openSUSE. The first existing file wins.
var btrfsMaintenanceConfigPaths = []string{"/etc/default/btrfsmaintenance", "/etc/sysconfig/btrfsmaintenance"}

type btrfsMaintenanceTask struct {
	Name      string
	PeriodVar string
	PathsVar  string
	Periods   []string
}

// btrfsMaintenanceTasks mirrors the variables and allowed periods documented
// in the package's default config file.
var btrfsMaintenanceTasks = []btrfsMaintenanceTask{
	{Name: "scrub", PeriodVar: "BTRFS_SCRUB_PERIOD", PathsVar: "BTRFS_SCRUB_MOUNTPOINTS", Periods: []string{"none", "weekly", "monthly"}},
	{Name: "balance", PeriodVar: "BTRFS_BALANCE_PERIOD", PathsVar: "BTRFS_BALANCE_MOUNTPOINTS", Periods: []string{"none", "daily", "weekly", "monthly"}},
	{Name: "trim", PeriodVar: "BTRFS_TRIM_PERIOD", PathsVar: "BTRFS_TRIM_MOUNTPOINTS", Periods: []string{"none", "daily", "weekly", "monthly"}},
	{Name: "defrag", PeriodVar: "BTRFS_DEFRAG_PERIOD", PathsVar: "BTRFS_DEFRAG_PATHS", Periods: []string{"none", "daily", "weekly", "monthly"}},
}

// CheckBtrfsMaintenanceAvailability reports whether the btrfsmaintenance
// scripts are installed.
func CheckBtrfsMaintenanceAvailability() (bool, error) {
	if _, err := os.Stat(btrfsMaintenanceScript); err != nil {
		return false, fmt.Errorf("btrfsmaintenance is not installed: %w", err)
	}
	return true, nil
}

func btrfsMaintenanceConfigPath() string {
	for _, path := range btrfsMaintenanceConfigPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return btrfsMaintenanceConfigPaths[0]
}

func btrfsMountpoints() ([]string, error) {
	partitions, err := disk.Partitions(true)
	if err != nil {
		return nil, err
	}
	var mounts []string
	for _, partition := range partitions {
		if partition.Fstype == "btrfs" && !slices.Contains(mounts, partition.Mountpoint) {
			mounts = append(mounts, partition.Mountpoint)
		}
	}
	slices.Sort(mounts)
	return mounts, nil
}

// parseShellVars reads KEY=value and KEY="value" assignments from a sourced
// shell config, ignoring comments. Later assignments win, as in the shell.
func parseShellVars(content string) map[string]string {
	vars := map[string]string{}
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		vars[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return vars
}

// setShellVar replaces every `KEY=` assignment line in place, preserving the
// surrounding comments, or appends one when the key is absent.
func setShellVar(content, key, value string) string {
	assignment := fmt.Sprintf("%s=%q", key, value)
	lines := strings.Split(content, "\n")
	replaced := false
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), key+"=") {
			lines[i] = assignment
			replaced = true
		}
	}
	if replaced {
		return strings.Join(lines, "\n")
	}
	content = strings.TrimRight(content, "\n")
	if content != "" {
		content += "\n"
	}
	return content + assignment + "\n"
}

// taskPaths returns the paths a task currently targets. A task whose period
// is "none" targets nothing, whatever its path list says, so the Debian
// default of "/" does not leak into a schedule the user turns on later.
func taskPaths(vars map[string]string, task btrfsMaintenanceTask, btrfsMounts []string) (string, []string) {
	period := vars[task.PeriodVar]
	if period == "" {
		period = btrfsMaintenancePeriodNone
	}
	if period == btrfsMaintenancePeriodNone {
		return period, nil
	}
	var paths []string
	for path := range strings.SplitSeq(vars[task.PathsVar], ":") {
		switch path = strings.TrimSpace(path); {
		case path == "":
		case path == "auto":
			paths = append(paths, btrfsMounts...)
		case !slices.Contains(paths, path):
			paths = append(paths, path)
		}
	}
	return period, paths
}

func btrfsMaintenanceSchedule(content string, btrfsMounts []string) apischema.BtrfsMaintenanceSchedule {
	vars := parseShellVars(content)
	schedule := apischema.BtrfsMaintenanceSchedule{Tasks: make([]apischema.BtrfsMaintenanceTask, 0, len(btrfsMaintenanceTasks))}
	for _, task := range btrfsMaintenanceTasks {
		period, paths := taskPaths(vars, task, btrfsMounts)
		if paths == nil {
			paths = []string{}
		}
		schedule.Tasks = append(schedule.Tasks, apischema.BtrfsMaintenanceTask{
			Task: task.Name, Period: period, Periods: task.Periods, Mountpoints: paths,
		})
	}
	return schedule
}

// applyBtrfsMaintenance sets one task's period for one mountpoint. The period
// is shared by every path the task targets, so enabling a period adds the
// mountpoint and retargets the whole list; "none" removes just this
// mountpoint and turns the task off when nothing is left.
func applyBtrfsMaintenance(content string, task btrfsMaintenanceTask, mountpoint, period string, btrfsMounts []string) (string, error) {
	if !slices.Contains(task.Periods, period) {
		return "", fmt.Errorf("invalid %s period %q", task.Name, period)
	}
	_, paths := taskPaths(parseShellVars(content), task, btrfsMounts)
	if period == btrfsMaintenancePeriodNone {
		paths = slices.DeleteFunc(paths, func(p string) bool { return p == mountpoint })
		if len(paths) > 0 {
			return setShellVar(content, task.PathsVar, strings.Join(paths, ":")), nil
		}
	} else if !slices.Contains(paths, mountpoint) {
		paths = append(paths, mountpoint)
	}
	content = setShellVar(content, task.PeriodVar, period)
	return setShellVar(content, task.PathsVar, strings.Join(paths, ":")), nil
}

func GetBtrfsMaintenance(_ context.Context) (apischema.BtrfsMaintenanceSchedule, error) {
	content, err := os.ReadFile(btrfsMaintenanceConfigPath())
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return apischema.BtrfsMaintenanceSchedule{}, fmt.Errorf("read btrfsmaintenance config: %w", err)
	}
	mounts, err := btrfsMountpoints()
	if err != nil {
		return apischema.BtrfsMaintenanceSchedule{}, fmt.Errorf("list btrfs mounts: %w", err)
	}
	return btrfsMaintenanceSchedule(string(content), mounts), nil
}

func SetBtrfsMaintenance(ctx context.Context, req apischema.BtrfsMaintenanceRequest) (apischema.BtrfsMaintenanceSchedule, error) {
	if !validPath.MatchString(req.Mountpoint) {
		return apischema.BtrfsMaintenanceSchedule{}, fmt.Errorf("invalid mountpoint")
	}
	idx := slices.IndexFunc(btrfsMaintenanceTasks, func(t btrfsMaintenanceTask) bool { return t.Name == req.Task })
	if idx < 0 {
		return apischema.BtrfsMaintenanceSchedule{}, fmt.Errorf("unknown btrfs maintenance task %q", req.Task)
	}
	task := btrfsMaintenanceTasks[idx]
	partition, err := getPartitionByMountpoint(req.Mountpoint)
	if err != nil {
		return apischema.BtrfsMaintenanceSchedule{}, err
	}
	if partition.Fstype != "btrfs" {
		return apischema.BtrfsMaintenanceSchedule{}, fmt.Errorf("%s is not a btrfs filesystem", req.Mountpoint)
	}
	if _, availErr := CheckBtrfsMaintenanceAvailability(); availErr != nil {
		return apischema.BtrfsMaintenanceSchedule{}, availErr
	}
	mounts, err := btrfsMountpoints()
	if err != nil {
		return apischema.BtrfsMaintenanceSchedule{}, fmt.Errorf("list btrfs mounts: %w", err)
	}

	path := btrfsMaintenanceConfigPath()
	mode := os.FileMode(0o644)
	content, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return apischema.BtrfsMaintenanceSchedule{}, fmt.Errorf("read btrfsmaintenance config: %w", err)
	}
	if info, statErr := os.Stat(path); statErr == nil {
		mode = info.Mode().Perm()
	}
	updated, err := applyBtrfsMaintenance(string(content), task, req.Mountpoint, req.Period, mounts)
	if err != nil {
		return apischema.BtrfsMaintenanceSchedule{}, err
	}
	if err := os.WriteFile(path, []byte(updated), mode); err != nil {
		return apischema.BtrfsMaintenanceSchedule{}, fmt.Errorf("write btrfsmaintenance config: %w", err)
	}
	if err := systemd.StartUnit(ctx, btrfsMaintenanceRefreshUnit); err != nil {
		return apischema.BtrfsMaintenanceSchedule{}, fmt.Errorf("config saved but refreshing btrfs maintenance timers failed: %w", err)
	}
	return btrfsMaintenanceSchedule(updated, mounts), nil
}
