package docker

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	dockerUpdateRunnerPath    = version.BinDir + "/linuxio-docker-update"
	DockerUpdateConfigPath    = "/etc/linuxio/docker-update.json"
	dockerUpdateUnitPath      = "/etc/systemd/system/" + dockerUpdateUnitName
	dockerUpdateTimerPath     = "/etc/systemd/system/" + dockerUpdateTimerName
	defaultDockerUpdateTime   = "04:00"
)

var dailyDockerUpdateTimePattern = regexp.MustCompile(`^([01][0-9]|2[0-3]):([0-5][0-9])$`)

type containerAutoUpdateStore struct {
	configPath string
	timerPath  string
	unitPath   string
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
	IncludeStopped bool                                    `json:"include_stopped"`
	UpdateStopped  bool                                    `json:"update_stopped"`
	ReviveStopped  bool                                    `json:"revive_stopped"`
}

var (
	defaultContainerAutoUpdateStore = containerAutoUpdateStore{
		configPath: DockerUpdateConfigPath,
		timerPath:  dockerUpdateTimerPath,
		unitPath:   dockerUpdateUnitPath,
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
			IncludeStopped: document.IncludeStopped,
			UpdateStopped:  document.UpdateStopped,
			ReviveStopped:  document.ReviveStopped,
		})
	}
	if !errors.Is(err, os.ErrNotExist) {
		return defaultContainerAutoUpdateOptions(), fmt.Errorf("read %s: %w", s.configPath, err)
	}
	return defaultContainerAutoUpdateOptions(), nil
}

func (s containerAutoUpdateStore) writeOptions(opts apischema.DockerContainerAutoUpdateOptions) error {
	document := dockerUpdateScheduleDocument{
		Version:        dockerUpdateConfigVersion,
		Mode:           opts.Mode,
		Time:           opts.Time,
		Cleanup:        opts.Cleanup,
		ContainerNames: opts.ContainerNames,
		IncludeStopped: opts.IncludeStopped,
		UpdateStopped:  opts.UpdateStopped,
		ReviveStopped:  opts.ReviveStopped,
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
	if opts.ReviveStopped && !opts.UpdateStopped {
		return apischema.DockerContainerAutoUpdateOptions{}, errors.New("starting stopped containers after update requires stopped-container updates")
	}
	return apischema.DockerContainerAutoUpdateOptions{
		Cleanup:        opts.Cleanup,
		ContainerNames: normalizeContainerNames(opts.ContainerNames),
		Enabled:        opts.Enabled,
		IncludeStopped: opts.IncludeStopped,
		Mode:           apischema.DockerContainerAutoUpdateMode(mode),
		ReviveStopped:  opts.ReviveStopped,
		Time:           timeOfDay,
		UpdateStopped:  opts.UpdateStopped,
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
ExecStart=%s run --config %s
TimeoutStartSec=infinity
NoNewPrivileges=true
PrivateTmp=true
ProtectHome=read-only
ProtectSystem=strict
ReadWritePaths=%s /run
`, dockerUpdateRunnerPath, DockerUpdateConfigPath, version.DataDir)
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

func CheckDockerUpdateRunnerInstalled() (bool, error) {
	info, err := os.Stat(dockerUpdateRunnerPath)
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", dockerUpdateRunnerPath, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return false, fmt.Errorf("%s is not an executable regular file", dockerUpdateRunnerPath)
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
	replicas := composeReplicaCounts(containers)
	targets := make([]apischema.DockerContainerAutoUpdateTarget, 0, len(containers))
	for _, ctr := range containers {
		name := primaryContainerName(ctr)
		if name == "" {
			continue
		}
		_, isSelected := selected[name]
		mutationAllowed, mutationReason := containerMutationEligibility(ctr, replicas)
		targets = append(targets, apischema.DockerContainerAutoUpdateTarget{
			ID:              ctr.ID,
			Image:           ctr.Image,
			Name:            name,
			Selected:        isSelected,
			State:           string(ctr.State),
			MutationAllowed: mutationAllowed,
			MutationReason:  mutationReason,
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

type composeReplicaKey struct {
	project string
	service string
}

func composeReplicaCounts(containers []container.Summary) map[composeReplicaKey]int {
	counts := make(map[composeReplicaKey]int)
	for _, ctr := range containers {
		project := strings.TrimSpace(ctr.Labels["com.docker.compose.project"])
		service := strings.TrimSpace(ctr.Labels["com.docker.compose.service"])
		if project == "" || service == "" {
			continue
		}
		counts[composeReplicaKey{project: project, service: service}]++
	}
	return counts
}

func containerMutationEligibility(
	ctr container.Summary,
	replicas map[composeReplicaKey]int,
) (bool, *string) {
	state := strings.ToLower(strings.TrimSpace(string(ctr.State)))
	if state != "running" && state != "exited" {
		reason := fmt.Sprintf("Container state %q cannot be updated automatically.", state)
		return false, &reason
	}

	project := strings.TrimSpace(ctr.Labels["com.docker.compose.project"])
	service := strings.TrimSpace(ctr.Labels["com.docker.compose.service"])
	if state == "exited" && project != "" {
		reason := "Stopped Compose services cannot be updated safely without changing their lifecycle state."
		return false, &reason
	}
	if project != "" && service != "" {
		replicaCount := replicas[composeReplicaKey{project: project, service: service}]
		if replicaCount > 1 {
			reason := fmt.Sprintf(
				`Compose service %q has %d replicas; automatic updates require a single replica.`,
				project+"/"+service,
				replicaCount,
			)
			return false, &reason
		}
	}
	return true, nil
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
