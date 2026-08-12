package docker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	dockerUpdateConfigVersion = 1
	dockerUpdateUnitName      = "linuxio-docker-update.service"
	dockerUpdateTimerName     = "linuxio-docker-update.timer"
	DockerUpdateConfigPath    = "/etc/linuxio/docker-update.json"
	dockerUpdateUnitPath      = "/etc/systemd/system/" + dockerUpdateUnitName
	dockerUpdateTimerPath     = "/etc/systemd/system/" + dockerUpdateTimerName
	legacyWatchtowerEnvPath   = "/etc/linuxio/watchtower.env"
	legacyWatchtowerTimerPath = "/etc/systemd/system/linuxio-watchtower.timer"
	defaultDockerUpdateTime   = "04:00"
)

var dailyDockerUpdateTimePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

type containerAutoUpdateStore struct {
	configPath      string
	legacyEnvPath   string
	legacyTimerPath string
	timerPath       string
	unitPath        string
}

type containerUpdateSystemdOps struct {
	daemonReload     func(context.Context) error
	disableUnit      func(context.Context, string) error
	enableUnit       func(context.Context, string) error
	getActiveState   func(context.Context, string) (string, error)
	getUnitFileState func(context.Context, string) (string, error)
	startUnit        func(context.Context, string) error
	stopUnit         func(context.Context, string) error
}

type dockerUpdateScheduleDocument struct {
	Version        int                                     `json:"version"`
	Mode           apischema.DockerContainerAutoUpdateMode `json:"mode"`
	Time           string                                  `json:"time"`
	Cleanup        bool                                    `json:"cleanup"`
	ContainerNames []string                                `json:"container_names"`
}

var (
	defaultContainerAutoUpdateStore = containerAutoUpdateStore{
		configPath:      DockerUpdateConfigPath,
		legacyEnvPath:   legacyWatchtowerEnvPath,
		legacyTimerPath: legacyWatchtowerTimerPath,
		timerPath:       dockerUpdateTimerPath,
		unitPath:        dockerUpdateUnitPath,
	}
	defaultContainerUpdateSystemdOps = containerUpdateSystemdOps{
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
	return getContainerAutoUpdate(ctx, defaultContainerAutoUpdateStore, defaultContainerUpdateSystemdOps)
}

func SetContainerAutoUpdate(ctx context.Context, opts apischema.DockerContainerAutoUpdateOptions) (apischema.DockerContainerAutoUpdateState, error) {
	if err := applyContainerAutoUpdate(ctx, defaultContainerAutoUpdateStore, defaultContainerUpdateSystemdOps, opts); err != nil {
		return apischema.DockerContainerAutoUpdateState{}, err
	}
	return getContainerAutoUpdate(ctx, defaultContainerAutoUpdateStore, defaultContainerUpdateSystemdOps)
}

func getContainerAutoUpdate(ctx context.Context, store containerAutoUpdateStore, ops containerUpdateSystemdOps) (apischema.DockerContainerAutoUpdateState, error) {
	opts, readErr := store.readOptions()
	timerEnabled, timerErr := containerUpdateTimerEnabled(ctx, ops)
	timerActive, activeErr := containerUpdateTimerActive(ctx, ops)
	opts.Enabled = timerEnabled

	available, installErr := CheckDockerUpdateRunnerInstalled()
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

func applyContainerAutoUpdate(ctx context.Context, store containerAutoUpdateStore, ops containerUpdateSystemdOps, opts apischema.DockerContainerAutoUpdateOptions) error {
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
		if err := ops.enableUnit(ctx, dockerUpdateTimerName); err != nil {
			return fmt.Errorf("enable %s: %w", dockerUpdateTimerName, err)
		}
		if err := ops.startUnit(ctx, dockerUpdateTimerName); err != nil {
			return fmt.Errorf("start %s: %w", dockerUpdateTimerName, err)
		}
		return nil
	}
	if err := ops.stopUnit(ctx, dockerUpdateTimerName); err != nil {
		return fmt.Errorf("stop %s: %w", dockerUpdateTimerName, err)
	}
	if err := ops.disableUnit(ctx, dockerUpdateTimerName); err != nil {
		return fmt.Errorf("disable %s: %w", dockerUpdateTimerName, err)
	}
	return nil
}

func (s containerAutoUpdateStore) readOptions() (apischema.DockerContainerAutoUpdateOptions, error) {
	data, err := os.ReadFile(s.configPath)
	if err == nil {
		var document dockerUpdateScheduleDocument
		if decodeErr := json.Unmarshal(data, &document); decodeErr != nil {
			return defaultContainerAutoUpdateOptions(), fmt.Errorf("decode %s: %w", s.configPath, decodeErr)
		}
		if document.Version != dockerUpdateConfigVersion {
			return defaultContainerAutoUpdateOptions(), fmt.Errorf("unsupported Docker update config version %d", document.Version)
		}
		return normalizeContainerAutoUpdateOptions(apischema.DockerContainerAutoUpdateOptions{
			Mode:           document.Mode,
			Time:           document.Time,
			Cleanup:        document.Cleanup,
			ContainerNames: document.ContainerNames,
		})
	}
	if !errors.Is(err, os.ErrNotExist) {
		return defaultContainerAutoUpdateOptions(), fmt.Errorf("read %s: %w", s.configPath, err)
	}
	return s.readLegacyOptions()
}

func (s containerAutoUpdateStore) readLegacyOptions() (apischema.DockerContainerAutoUpdateOptions, error) {
	opts := defaultContainerAutoUpdateOptions()
	data, err := os.ReadFile(s.legacyEnvPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return opts, nil
		}
		return opts, fmt.Errorf("read %s: %w", s.legacyEnvPath, err)
	}
	assignments := parseLegacyEnvironment(data)
	if legacyTruthy(assignments["WATCHTOWER_MONITOR_ONLY"]) {
		opts.Mode = "check_only"
	}
	opts.Cleanup = legacyTruthy(assignments["WATCHTOWER_CLEANUP"])
	opts.ContainerNames = parseLegacyContainerNames(assignments["LINUXIO_WATCHTOWER_CONTAINERS"])
	if timerData, timerErr := os.ReadFile(s.legacyTimerPath); timerErr == nil {
		opts.Time = parseDockerUpdateTimer(timerData)
	} else if !errors.Is(timerErr, os.ErrNotExist) {
		return opts, fmt.Errorf("read %s: %w", s.legacyTimerPath, timerErr)
	}
	return normalizeContainerAutoUpdateOptions(opts)
}

func (s containerAutoUpdateStore) writeOptions(opts apischema.DockerContainerAutoUpdateOptions) error {
	document := dockerUpdateScheduleDocument{
		Version:        dockerUpdateConfigVersion,
		Mode:           opts.Mode,
		Time:           opts.Time,
		Cleanup:        opts.Cleanup,
		ContainerNames: opts.ContainerNames,
	}
	configBytes, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Docker update config: %w", err)
	}
	configBytes = append(configBytes, '\n')
	timerBytes, err := renderDockerUpdateTimer(opts.Time)
	if err != nil {
		return err
	}

	if err := utils.WriteFileAtomic(s.configPath, configBytes, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", s.configPath, err)
	}
	if err := utils.WriteFileAtomic(s.unitPath, renderDockerUpdateUnit(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", s.unitPath, err)
	}
	if err := utils.WriteFileAtomic(s.timerPath, timerBytes, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", s.timerPath, err)
	}
	return nil
}

func defaultContainerAutoUpdateOptions() apischema.DockerContainerAutoUpdateOptions {
	return apischema.DockerContainerAutoUpdateOptions{Mode: "update", Time: defaultDockerUpdateTime}
}

func normalizeContainerAutoUpdateOptions(opts apischema.DockerContainerAutoUpdateOptions) (apischema.DockerContainerAutoUpdateOptions, error) {
	mode := strings.TrimSpace(string(opts.Mode))
	if mode == "" {
		mode = "update"
	}
	if mode != "update" && mode != "check_only" {
		return apischema.DockerContainerAutoUpdateOptions{}, fmt.Errorf("invalid Docker update mode %q", opts.Mode)
	}
	timeOfDay := strings.TrimSpace(opts.Time)
	if timeOfDay == "" {
		timeOfDay = defaultDockerUpdateTime
	}
	if !dailyDockerUpdateTimePattern.MatchString(timeOfDay) {
		return apischema.DockerContainerAutoUpdateOptions{}, fmt.Errorf("invalid Docker update time %q", opts.Time)
	}
	return apischema.DockerContainerAutoUpdateOptions{
		Cleanup:        opts.Cleanup,
		ContainerNames: normalizeContainerNames(opts.ContainerNames),
		Enabled:        opts.Enabled,
		Mode:           apischema.DockerContainerAutoUpdateMode(mode),
		Time:           timeOfDay,
	}, nil
}

func normalizeContainerNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimPrefix(strings.TrimSpace(name), "/")
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}

func renderDockerUpdateUnit() []byte {
	return fmt.Appendf(nil, `[Unit]
Description=Run LinuxIO native Docker updates
After=docker.service network-online.target
Requires=docker.service
Wants=network-online.target

[Service]
Type=oneshot
ExecStart=%s/linuxio docker-update-runner --config %s
TimeoutStartSec=infinity
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=read-only
ProtectSystem=strict
ReadWritePaths=%s /run
`, version.BinDir, DockerUpdateConfigPath, version.DataDir)
}

func renderDockerUpdateTimer(timeOfDay string) ([]byte, error) {
	normalized, err := normalizeContainerAutoUpdateOptions(apischema.DockerContainerAutoUpdateOptions{Mode: "update", Time: timeOfDay})
	if err != nil {
		return nil, err
	}
	return fmt.Appendf(nil, `[Unit]
Description=Schedule LinuxIO native Docker updates

[Timer]
OnCalendar=*-*-* %s:00
Persistent=true
Unit=%s

[Install]
WantedBy=timers.target
`, normalized.Time, dockerUpdateUnitName), nil
}

func parseDockerUpdateTimer(data []byte) string {
	for line := range strings.SplitSeq(string(data), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || key != "OnCalendar" {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 2 {
			value := strings.TrimSuffix(fields[1], ":00")
			if dailyDockerUpdateTimePattern.MatchString(value) {
				return value
			}
		}
	}
	return defaultDockerUpdateTime
}

func parseLegacyEnvironment(data []byte) map[string]string {
	out := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if ok {
			out[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return out
}

func legacyTruthy(value string) bool {
	return slices.Contains([]string{"1", "true", "yes", "on"}, strings.ToLower(strings.TrimSpace(value)))
}

func parseLegacyContainerNames(value string) []string {
	var names []string
	for token := range strings.FieldsSeq(value) {
		if token == "" || token == "__linuxio_no_containers_selected__" {
			continue
		}
		token = strings.ReplaceAll(token, `\\`, `\`)
		var name strings.Builder
		escaped := false
		for _, character := range token {
			switch {
			case escaped:
				name.WriteRune(character)
				escaped = false
			case character == '\\':
				escaped = true
			default:
				name.WriteRune(character)
			}
		}
		names = append(names, name.String())
	}
	return normalizeContainerNames(names)
}

func CheckDockerUpdateRunnerInstalled() (bool, error) {
	path := filepath.Join(version.BinDir, "linuxio")
	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return false, fmt.Errorf("%s is not an executable regular file", path)
	}
	return true, nil
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
			ID: ctr.ID, Image: ctr.Image, Name: name, Selected: isSelected, State: string(ctr.State),
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
	for _, name := range normalizeContainerNames(selectedNames) {
		if _, ok := current[name]; !ok {
			missing = append(missing, name)
		}
	}
	return missing
}

func selectedNameSet(names []string) map[string]struct{} {
	normalized := normalizeContainerNames(names)
	out := make(map[string]struct{}, len(normalized))
	for _, name := range normalized {
		out[name] = struct{}{}
	}
	return out
}

func containerUpdateTimerEnabled(ctx context.Context, ops containerUpdateSystemdOps) (bool, error) {
	state, err := ops.getUnitFileState(ctx, dockerUpdateTimerName)
	if err != nil {
		return false, fmt.Errorf("read %s unit state: %w", dockerUpdateTimerName, err)
	}
	return state == "enabled" || state == "enabled-runtime", nil
}

func containerUpdateTimerActive(ctx context.Context, ops containerUpdateSystemdOps) (bool, error) {
	state, err := ops.getActiveState(ctx, dockerUpdateTimerName)
	if err != nil {
		return false, fmt.Errorf("read %s active state: %w", dockerUpdateTimerName, err)
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
