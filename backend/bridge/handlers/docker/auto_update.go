package docker

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/watchtower"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

type watchtowerAutoUpdateStore struct {
	envPath   string
	timerPath string
	unitPath  string
}

type watchtowerSystemdOps struct {
	daemonReload     func(context.Context) error
	disableUnit      func(context.Context, string) error
	enableUnit       func(context.Context, string) error
	getActiveState   func(context.Context, string) (string, error)
	getUnitFileState func(context.Context, string) (string, error)
	startUnit        func(context.Context, string) error
	stopUnit         func(context.Context, string) error
}

var (
	defaultWatchtowerAutoUpdateStore = watchtowerAutoUpdateStore{
		envPath:   watchtower.EnvPath,
		timerPath: watchtower.TimerPath,
		unitPath:  watchtower.UnitPath,
	}
	defaultWatchtowerSystemdOps = watchtowerSystemdOps{
		daemonReload:     systemd.DaemonReload,
		disableUnit:      systemd.DisableUnit,
		enableUnit:       systemd.EnableUnit,
		getActiveState:   systemd.GetActiveState,
		getUnitFileState: systemd.GetUnitFileState,
		startUnit:        systemd.StartUnit,
		stopUnit:         systemd.StopUnit,
	}
)

func GetContainerAutoUpdate(ctx context.Context) (apischema.DockerContainerAutoUpdateState, error) {
	return getContainerAutoUpdate(ctx, defaultWatchtowerAutoUpdateStore, defaultWatchtowerSystemdOps)
}

func SetContainerAutoUpdate(ctx context.Context, opts apischema.DockerContainerAutoUpdateOptions) (apischema.DockerContainerAutoUpdateState, error) {
	if err := applyContainerAutoUpdate(ctx, defaultWatchtowerAutoUpdateStore, defaultWatchtowerSystemdOps, opts); err != nil {
		return apischema.DockerContainerAutoUpdateState{}, err
	}
	return getContainerAutoUpdate(ctx, defaultWatchtowerAutoUpdateStore, defaultWatchtowerSystemdOps)
}

func getContainerAutoUpdate(ctx context.Context, store watchtowerAutoUpdateStore, ops watchtowerSystemdOps) (apischema.DockerContainerAutoUpdateState, error) {
	opts, readErr := store.readOptions()
	timerEnabled, timerErr := watchtowerTimerEnabled(ctx, ops)
	timerActive, activeErr := watchtowerTimerActive(ctx, ops)
	opts.Enabled = timerEnabled

	available, installErr := watchtower.CheckInstalled()
	targets, missing, listErr := containerAutoUpdateTargets(ctx, opts.ContainerNames)
	if listErr != nil {
		return apischema.DockerContainerAutoUpdateState{}, listErr
	}

	return apischema.DockerContainerAutoUpdateState{
		Available:             available,
		Containers:            targets,
		Error:                 utils.OptionalString(joinErrorMessages(installErr, readErr, timerErr, activeErr)),
		MissingContainerNames: missing,
		Options:               opts,
		TimerActive:           timerActive,
		TimerEnabled:          timerEnabled,
	}, nil
}

func applyContainerAutoUpdate(ctx context.Context, store watchtowerAutoUpdateStore, ops watchtowerSystemdOps, opts apischema.DockerContainerAutoUpdateOptions) error {
	normalized, err := normalizeContainerAutoUpdateOptions(opts)
	if err != nil {
		return err
	}
	if err := store.writeOptions(normalized); err != nil {
		return err
	}
	if err := ops.daemonReload(ctx); err != nil {
		return fmt.Errorf("reload systemd: %w", err)
	}
	if normalized.Enabled {
		if err := ops.enableUnit(ctx, watchtower.TimerName); err != nil {
			return fmt.Errorf("enable %s: %w", watchtower.TimerName, err)
		}
		if err := ops.startUnit(ctx, watchtower.TimerName); err != nil {
			return fmt.Errorf("start %s: %w", watchtower.TimerName, err)
		}
		return nil
	}
	if err := ops.stopUnit(ctx, watchtower.TimerName); err != nil {
		return fmt.Errorf("stop %s: %w", watchtower.TimerName, err)
	}
	if err := ops.disableUnit(ctx, watchtower.TimerName); err != nil {
		return fmt.Errorf("disable %s: %w", watchtower.TimerName, err)
	}
	return nil
}

func (s watchtowerAutoUpdateStore) readOptions() (apischema.DockerContainerAutoUpdateOptions, error) {
	cfg := watchtower.DefaultScheduleConfig()
	var readErr error
	if envBytes, err := os.ReadFile(s.envPath); err == nil {
		envCfg := watchtower.ParseEnv(envBytes)
		cfg.Mode = envCfg.Mode
		cfg.Cleanup = envCfg.Cleanup
		cfg.ContainerNames = envCfg.ContainerNames
	} else if !errors.Is(err, os.ErrNotExist) {
		readErr = fmt.Errorf("read %s: %w", s.envPath, err)
	}

	if timerBytes, err := os.ReadFile(s.timerPath); err == nil {
		cfg.Time = watchtower.ParseTimer(timerBytes)
	} else if !errors.Is(err, os.ErrNotExist) && readErr == nil {
		readErr = fmt.Errorf("read %s: %w", s.timerPath, err)
	}

	return optionsFromWatchtowerConfig(cfg, false), readErr
}

func (s watchtowerAutoUpdateStore) writeOptions(opts apischema.DockerContainerAutoUpdateOptions) error {
	cfg := watchtower.ScheduleConfig{
		Mode:           string(opts.Mode),
		Time:           opts.Time,
		Cleanup:        opts.Cleanup,
		ContainerNames: opts.ContainerNames,
	}
	envBytes, err := watchtower.RenderEnv(cfg)
	if err != nil {
		return err
	}
	timerBytes, err := watchtower.RenderTimer(opts.Time)
	if err != nil {
		return err
	}
	unitBytes, err := watchtower.UnitFile()
	if err != nil {
		return err
	}

	if err := utils.WriteFileAtomic(s.envPath, envBytes, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", s.envPath, err)
	}
	if err := utils.WriteFileAtomic(s.unitPath, unitBytes, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", s.unitPath, err)
	}
	if err := utils.WriteFileAtomic(s.timerPath, timerBytes, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", s.timerPath, err)
	}
	return nil
}

func normalizeContainerAutoUpdateOptions(opts apischema.DockerContainerAutoUpdateOptions) (apischema.DockerContainerAutoUpdateOptions, error) {
	cfg, err := watchtower.NormalizeScheduleConfig(watchtower.ScheduleConfig{
		Mode:           string(opts.Mode),
		Time:           opts.Time,
		Cleanup:        opts.Cleanup,
		ContainerNames: opts.ContainerNames,
	})
	if err != nil {
		return apischema.DockerContainerAutoUpdateOptions{}, err
	}
	return optionsFromWatchtowerConfig(cfg, opts.Enabled), nil
}

func optionsFromWatchtowerConfig(cfg watchtower.ScheduleConfig, enabled bool) apischema.DockerContainerAutoUpdateOptions {
	normalized, err := watchtower.NormalizeScheduleConfig(cfg)
	if err != nil {
		normalized = watchtower.DefaultScheduleConfig()
	}
	return apischema.DockerContainerAutoUpdateOptions{
		Cleanup:        normalized.Cleanup,
		ContainerNames: normalized.ContainerNames,
		Enabled:        enabled,
		Mode:           apischema.DockerContainerAutoUpdateMode(normalized.Mode),
		Time:           normalized.Time,
	}
}

func containerAutoUpdateTargets(ctx context.Context, selectedNames []string) ([]apischema.DockerContainerAutoUpdateTarget, []string, error) {
	cli, err := getClient()
	if err != nil {
		return nil, nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list containers: %w", err)
	}
	return buildContainerAutoUpdateTargets(containers.Items, selectedNames), missingSelectedContainerNames(containers.Items, selectedNames), nil
}

func buildContainerAutoUpdateTargets(containers []container.Summary, selectedNames []string) []apischema.DockerContainerAutoUpdateTarget {
	selected := selectedNameSet(selectedNames)
	targets := make([]apischema.DockerContainerAutoUpdateTarget, 0, len(containers))
	for _, ctr := range containers {
		name := primaryContainerName(ctr)
		if name == "" {
			continue
		}
		_, isSelected := selected[name]
		targets = append(targets, apischema.DockerContainerAutoUpdateTarget{
			ID:       ctr.ID,
			Image:    ctr.Image,
			Name:     name,
			Selected: isSelected,
			State:    string(ctr.State),
		})
	}
	slices.SortFunc(targets, func(a, b apischema.DockerContainerAutoUpdateTarget) int {
		if d := strings.Compare(a.Name, b.Name); d != 0 {
			return d
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return targets
}

func missingSelectedContainerNames(containers []container.Summary, selectedNames []string) []string {
	current := map[string]struct{}{}
	for _, ctr := range containers {
		if name := primaryContainerName(ctr); name != "" {
			current[name] = struct{}{}
		}
	}

	var missing []string
	for _, name := range watchtower.NormalizeContainerNames(selectedNames) {
		if _, ok := current[name]; !ok {
			missing = append(missing, name)
		}
	}
	return missing
}

func selectedNameSet(names []string) map[string]struct{} {
	normalized := watchtower.NormalizeContainerNames(names)
	out := make(map[string]struct{}, len(normalized))
	for _, name := range normalized {
		out[name] = struct{}{}
	}
	return out
}

func watchtowerTimerEnabled(ctx context.Context, ops watchtowerSystemdOps) (bool, error) {
	state, err := ops.getUnitFileState(ctx, watchtower.TimerName)
	if err != nil {
		return false, fmt.Errorf("read %s unit state: %w", watchtower.TimerName, err)
	}
	switch state {
	case "enabled", "enabled-runtime":
		return true, nil
	default:
		return false, nil
	}
}

func watchtowerTimerActive(ctx context.Context, ops watchtowerSystemdOps) (bool, error) {
	state, err := ops.getActiveState(ctx, watchtower.TimerName)
	if err != nil {
		return false, fmt.Errorf("read %s active state: %w", watchtower.TimerName, err)
	}
	return state == "active", nil
}

func joinErrorMessages(errs ...error) string {
	var messages []string
	for _, err := range errs {
		if err != nil {
			messages = append(messages, err.Error())
		}
	}
	return strings.Join(messages, "; ")
}
