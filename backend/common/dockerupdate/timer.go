package dockerupdate

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

const (
	dockerUpdateUnitName  = "linuxio-docker-update.service"
	dockerUpdateTimerName = "linuxio-docker-update.timer"
	dockerUpdateUnitPath  = "/etc/systemd/system/" + dockerUpdateUnitName
	dockerUpdateTimerPath = "/etc/systemd/system/" + dockerUpdateTimerName
)

type containerUpdateSystemdOps struct {
	daemonReload     func(context.Context) error
	disableUnit      func(context.Context, string) error
	enableUnit       func(context.Context, string) error
	getActiveState   func(context.Context, string) (string, error)
	getUnitFileState func(context.Context, string) (string, error)
	startUnit        func(context.Context, string) error
	stopUnit         func(context.Context, string) error
}

var defaultContainerUpdateSystemdOps = containerUpdateSystemdOps{
	daemonReload:     systemd.DaemonReload,
	disableUnit:      systemd.DisableUnit,
	enableUnit:       systemd.EnableUnit,
	getActiveState:   systemd.GetActiveState,
	getUnitFileState: systemd.GetUnitFileState,
	startUnit:        systemd.StartUnit,
	stopUnit:         systemd.StopUnit,
}

// GetAutoUpdate reports the scheduled-update configuration, the timer state and
// the containers the schedule can target.
func GetAutoUpdate(ctx context.Context, cli *client.Client) (DockerContainerAutoUpdateState, error) {
	return getContainerAutoUpdate(ctx, cli, defaultContainerAutoUpdateStore, defaultContainerUpdateSystemdOps)
}

// SetAutoUpdate persists the schedule, renders the service and timer units,
// enables or disables the timer and returns the resulting state.
func SetAutoUpdate(ctx context.Context, cli *client.Client, opts DockerContainerAutoUpdateOptions) (DockerContainerAutoUpdateState, error) {
	if err := applyContainerAutoUpdate(ctx, defaultContainerAutoUpdateStore, defaultContainerUpdateSystemdOps, opts); err != nil {
		return DockerContainerAutoUpdateState{}, err
	}
	return getContainerAutoUpdate(ctx, cli, defaultContainerAutoUpdateStore, defaultContainerUpdateSystemdOps)
}

func getContainerAutoUpdate(ctx context.Context, cli *client.Client, store containerAutoUpdateStore, ops containerUpdateSystemdOps) (DockerContainerAutoUpdateState, error) {
	opts, readErr := store.readOptions()
	timerEnabled, timerErr := containerUpdateTimerEnabled(ctx, ops)
	timerActive, activeErr := containerUpdateTimerActive(ctx, ops)
	opts.Enabled = timerEnabled

	available, installErr := CheckRunnerInstalled()
	targets, missing, listErr := containerAutoUpdateTargets(ctx, cli, opts.ContainerNames)
	if listErr != nil {
		return DockerContainerAutoUpdateState{}, listErr
	}

	return DockerContainerAutoUpdateState{
		Available:             available,
		Containers:            targets,
		Error:                 optional(joinErrorMessages(installErr, readErr, timerErr, activeErr)),
		MissingContainerNames: missing,
		Options:               opts,
		TimerActive:           timerActive,
		TimerEnabled:          timerEnabled,
	}, nil
}

func applyContainerAutoUpdate(ctx context.Context, store containerAutoUpdateStore, ops containerUpdateSystemdOps, opts DockerContainerAutoUpdateOptions) error {
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

func renderDockerUpdateUnit() []byte {
	unit := WorkerUnit
	execStart := strings.Join(append([]string{RunnerPath}, scheduledArgs()...), " ")
	return fmt.Appendf(nil, `[Unit]
Description=Run LinuxIO native Docker updates
After=%s
Requires=%s
Wants=%s

[Service]
Type=oneshot
ExecStart=%s
TimeoutStartSec=infinity
NoNewPrivileges=%t
PrivateTmp=%t
ProtectHome=%s
ProtectSystem=%s
ReadWritePaths=%s
`,
		strings.Join(unit.After, " "),
		strings.Join(unit.Requires, " "),
		strings.Join(unit.Wants, " "),
		execStart,
		unit.NoNewPrivileges,
		unit.PrivateTmp,
		unit.ProtectHome,
		unit.ProtectSystem,
		strings.Join(unit.ReadWritePaths, " "),
	)
}

func renderDockerUpdateTimer(timeOfDay string) ([]byte, error) {
	normalized, err := normalizeContainerAutoUpdateOptions(DockerContainerAutoUpdateOptions{Mode: "update", Time: timeOfDay})
	if err != nil {
		return nil, err
	}
	return fmt.Appendf(nil, `[Unit]
Description=Schedule LinuxIO native Docker updates

[Timer]
OnCalendar=*-*-* %s:00
RandomizedDelaySec=1m
Persistent=true
Unit=%s

[Install]
WantedBy=timers.target
`, normalized.Time, dockerUpdateUnitName), nil
}

// CheckRunnerInstalled reports whether the update worker binary is installed
// as an executable regular file.
func CheckRunnerInstalled() (bool, error) {
	info, err := os.Stat(RunnerPath)
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", RunnerPath, err)
	}
	if !info.Mode().IsRegular() || info.Mode()&0o111 == 0 {
		return false, fmt.Errorf("%s is not an executable regular file", RunnerPath)
	}
	return true, nil
}

func containerAutoUpdateTargets(ctx context.Context, cli *client.Client, selectedNames []string) ([]DockerContainerAutoUpdateTarget, []string, error) {
	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list containers: %w", err)
	}
	return buildContainerAutoUpdateTargets(containers.Items, selectedNames), missingSelectedContainerNames(containers.Items, selectedNames), nil
}

func buildContainerAutoUpdateTargets(containers []container.Summary, selectedNames []string) []DockerContainerAutoUpdateTarget {
	selected := selectedNameSet(selectedNames)
	replicas := composeReplicaCounts(containers)
	targets := make([]DockerContainerAutoUpdateTarget, 0, len(containers))
	for _, ctr := range containers {
		name := PrimaryContainerName(ctr)
		if name == "" {
			continue
		}
		_, isSelected := selected[name]
		mutationAllowed, mutationReason := containerMutationEligibility(ctr, replicas)
		targets = append(targets, DockerContainerAutoUpdateTarget{
			ID:              ctr.ID,
			Image:           ctr.Image,
			Name:            name,
			Selected:        isSelected,
			State:           string(ctr.State),
			MutationAllowed: mutationAllowed,
			MutationReason:  mutationReason,
		})
	}
	slices.SortFunc(targets, func(a, b DockerContainerAutoUpdateTarget) int {
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
		if name := PrimaryContainerName(ctr); name != "" {
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
