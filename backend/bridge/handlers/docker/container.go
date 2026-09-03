package docker

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/monitoring"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

const containerMetricsTimeout = 2 * time.Second

// List all containers with metrics.
func ListContainers(ctx context.Context) ([]apischema.ContainerInfo, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	updateStatus := readUpdateStatusSnapshot()
	metricsSnapshot := monitoring.ContainerMetricsSnapshot{}
	var metricsErr error
	if len(containers.Items) > 0 {
		metricsCtx, cancel := context.WithTimeout(ctx, containerMetricsTimeout)
		metricsSnapshot, metricsErr = monitoring.FetchContainerMetricsSnapshot(metricsCtx)
		cancel()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if metricsErr != nil {
			slog.Debug("container metrics unavailable", "component", "docker", "error", metricsErr)
		}
	}
	now := time.Now()
	enriched := make([]apischema.ContainerInfo, 0, len(containers.Items))

	for _, ctr := range containers.Items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		metrics := containerMetricsFromSnapshot(ctr, metricsSnapshot, metricsErr, now)
		iconIdentifier, resolvedURL, proxyPort := resolveContainerPresentation(ctr)

		info := containerInfoFromSummary(ctr, metrics, iconIdentifier, resolvedURL, proxyPort)
		applyContainerUpdateStatus(&info, updateStatus)
		enriched = append(enriched, info)
	}

	// Docker's API does not guarantee a stable order across calls. Sort by
	// creation time (newest first) so the UI doesn't reshuffle on each refetch,
	// tie-breaking on ID for full determinism.
	slices.SortFunc(enriched, func(a, b apischema.ContainerInfo) int {
		if d := cmp.Compare(b.Created, a.Created); d != 0 {
			return d
		}
		return strings.Compare(a.ID, b.ID)
	})

	return enriched, nil
}

func containerInfoFromSummary(
	ctr container.Summary,
	metrics *apischema.ContainerMetrics,
	iconIdentifier string,
	resolvedURL string,
	proxyPort string,
) apischema.ContainerInfo {
	info := apischema.ContainerInfo{
		Created:         ctr.Created,
		HostConfig:      containerHostConfigFromSummary(ctr),
		Icon:            utils.OptionalString(iconIdentifier),
		ID:              ctr.ID,
		Image:           ctr.Image,
		Labels:          ctr.Labels,
		Metrics:         metrics,
		Mounts:          containerMountsFromSummary(ctr.Mounts),
		Names:           ctr.Names,
		NetworkSettings: containerNetworkSettingsFromSummary(ctr.NetworkSettings),
		Ports:           containerPortsFromSummary(ctr.Ports),
		ProxyPort:       utils.OptionalString(proxyPort),
		State:           string(ctr.State),
		Status:          ctr.Status,
		URL:             utils.OptionalString(resolvedURL),
	}
	return info
}

func containerHostConfigFromSummary(ctr container.Summary) *apischema.ContainerHostConfig {
	networkMode := utils.OptionalString(ctr.HostConfig.NetworkMode)
	if networkMode == nil {
		return nil
	}
	return &apischema.ContainerHostConfig{NetworkMode: networkMode}
}

func containerNetworkSettingsFromSummary(settings *container.NetworkSettingsSummary) *apischema.ContainerNetworkSettings {
	if settings == nil || len(settings.Networks) == 0 {
		return nil
	}

	networks := containerEndpointsFromSDK(settings.Networks)
	if len(networks) == 0 {
		return nil
	}
	return &apischema.ContainerNetworkSettings{Networks: networks}
}

func containerEndpointsFromSDK(endpoints map[string]*network.EndpointSettings) map[string]apischema.ContainerEndpoint {
	result := make(map[string]apischema.ContainerEndpoint, len(endpoints))
	for name, endpoint := range endpoints {
		if endpoint == nil {
			continue
		}
		result[name] = apischema.ContainerEndpoint{
			Aliases:           append([]string(nil), endpoint.Aliases...),
			Gateway:           addrString(endpoint.Gateway),
			GlobalIPv6Address: optionalAddrString(endpoint.GlobalIPv6Address),
			IPAddress:         addrString(endpoint.IPAddress),
			MACAddress:        utils.OptionalString(endpoint.MacAddress.String()),
		}
	}
	return result
}

func containerPortsFromSummary(ports []container.PortSummary) []apischema.ContainerPort {
	if len(ports) == 0 {
		return nil
	}

	result := make([]apischema.ContainerPort, 0, len(ports))
	for _, port := range ports {
		result = append(result, apischema.ContainerPort{
			IP:          optionalAddrString(port.IP),
			PrivatePort: int(port.PrivatePort),
			PublicPort:  utils.OptionalInt(int(port.PublicPort)),
			Type:        port.Type,
		})
	}

	// Numeric sort by container-side port, tie-broken by protocol, so the UI
	// shows ports in a stable order across refetches.
	slices.SortFunc(result, func(a, b apischema.ContainerPort) int {
		if d := cmp.Compare(a.PrivatePort, b.PrivatePort); d != 0 {
			return d
		}
		return strings.Compare(a.Type, b.Type)
	})

	return result
}

func containerMountsFromSummary(mounts []container.MountPoint) []apischema.ContainerMount {
	if len(mounts) == 0 {
		return nil
	}

	result := make([]apischema.ContainerMount, 0, len(mounts))
	for _, mount := range mounts {
		result = append(result, apischema.ContainerMount{
			Destination: mount.Destination,
			Driver:      mount.Driver,
			Mode:        mount.Mode,
			Name:        mount.Name,
			Propagation: string(mount.Propagation),
			RW:          mount.RW,
			Source:      mount.Source,
			Type:        string(mount.Type),
		})
	}

	// Alphabetical sort by in-container destination path, tie-broken by source,
	// so the UI shows volumes in a stable order across refetches.
	slices.SortFunc(result, func(a, b apischema.ContainerMount) int {
		if d := strings.Compare(a.Destination, b.Destination); d != 0 {
			return d
		}
		return strings.Compare(a.Source, b.Source)
	})

	return result
}

func containerMetricsFromSnapshot(
	ctr container.Summary,
	snapshot monitoring.ContainerMetricsSnapshot,
	fetchErr error,
	now time.Time,
) *apischema.ContainerMetrics {
	if ctr.State != container.StateRunning && ctr.State != container.StatePaused {
		return &apischema.ContainerMetrics{Status: apischema.ContainerMetricsStatusNotRunning}
	}
	if fetchErr != nil || snapshot.CapturedAtMs <= 0 {
		return &apischema.ContainerMetrics{Status: apischema.ContainerMetricsStatusUnavailable}
	}

	sample, ok := snapshot.Samples[ctr.ID]
	if !ok && len(ctr.ID) > 12 {
		sample, ok = snapshot.Samples[ctr.ID[:12]]
	}
	if !ok {
		return &apischema.ContainerMetrics{Status: apischema.ContainerMetricsStatusUnavailable}
	}

	capturedAtMs := snapshot.CapturedAtMs
	cpuPercent := sample.CPUPercent
	memoryUsageBytes := sample.MemoryUsageBytes
	networkReceiveBytesPerSecond := sample.NetworkReceiveBytesPerSecond
	networkSendBytesPerSecond := sample.NetworkSendBytesPerSecond
	status := apischema.ContainerMetricsStatusAvailable
	freshFor := max(3*snapshot.CollectorInterval, time.Minute)
	if now.Sub(time.UnixMilli(capturedAtMs)) > freshFor {
		status = apischema.ContainerMetricsStatusStale
	}

	return &apischema.ContainerMetrics{
		BlockReadBytesPerSecond:      sample.BlockReadBytesPerSecond,
		BlockWriteBytesPerSecond:     sample.BlockWriteBytesPerSecond,
		CapturedAtMs:                 &capturedAtMs,
		CPUPercent:                   &cpuPercent,
		MemoryUsageBytes:             &memoryUsageBytes,
		NetworkReceiveBytesPerSecond: &networkReceiveBytesPerSecond,
		NetworkSendBytesPerSecond:    &networkSendBytesPerSecond,
		Status:                       status,
	}
}

func resolveContainerPresentation(ctr container.Summary) (string, string, string) {
	containerIcon := ctr.Labels["io.linuxio.container.icon"]
	containerURL := ctr.Labels["io.linuxio.container.url"]
	proxyPort := ctr.Labels[ProxyPortLabel]
	iconName := containerIconName(ctr)
	resolvedURL := resolveContainerURL(containerURL, proxyPort, iconName)
	return ResolveIconIdentifier(containerIcon, iconName), resolvedURL, proxyPort
}

func containerIconName(ctr container.Summary) string {
	if len(ctr.Names) == 0 {
		return ""
	}
	containerName := strings.TrimPrefix(ctr.Names[0], "/")
	serviceName := ctr.Labels["com.docker.compose.service"]
	projectName := ctr.Labels["com.docker.compose.project"]
	if serviceName == "" || projectName == "" {
		return containerName
	}
	expectedPrefix := projectName + "-" + serviceName + "-"
	if strings.HasPrefix(containerName, expectedPrefix) {
		return serviceName
	}
	return containerName
}

func resolveContainerURL(containerURL, proxyPort, iconName string) string {
	if containerURL != "" || proxyPort == "" || iconName == "" {
		return containerURL
	}
	return "/proxy/" + iconName + "/"
}

func addrString(value netip.Addr) string {
	if !value.IsValid() {
		return ""
	}
	return value.String()
}

func optionalAddrString(value netip.Addr) *string {
	return utils.OptionalString(addrString(value))
}

type containerInspector interface {
	ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
}

type containerStarter interface {
	ContainerStart(context.Context, string, client.ContainerStartOptions) (client.ContainerStartResult, error)
}

type containerStopper interface {
	ContainerStop(context.Context, string, client.ContainerStopOptions) (client.ContainerStopResult, error)
}

type containerRestarter interface {
	ContainerRestart(context.Context, string, client.ContainerRestartOptions) (client.ContainerRestartResult, error)
}

type containerPauser interface {
	ContainerPause(context.Context, string, client.ContainerPauseOptions) (client.ContainerPauseResult, error)
}

type containerUnpauser interface {
	ContainerUnpause(context.Context, string, client.ContainerUnpauseOptions) (client.ContainerUnpauseResult, error)
}

type containerKiller interface {
	ContainerKill(context.Context, string, client.ContainerKillOptions) (client.ContainerKillResult, error)
}

type containerRemover interface {
	ContainerRemove(context.Context, string, client.ContainerRemoveOptions) (client.ContainerRemoveResult, error)
}

func InspectContainer(ctx context.Context, id string) (apischema.ContainerInspectInfo, error) {
	cli, err := getClient()
	if err != nil {
		return apischema.ContainerInspectInfo{}, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	return inspectContainer(ctx, cli, id)
}

func inspectContainer(ctx context.Context, cli containerInspector, id string) (apischema.ContainerInspectInfo, error) {
	result, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		return apischema.ContainerInspectInfo{}, fmt.Errorf("failed to inspect container: %w", err)
	}
	return containerInspectInfoFromSDK(result.Container), nil
}

func containerInspectInfoFromSDK(inspect container.InspectResponse) apischema.ContainerInspectInfo {
	info := apischema.ContainerInspectInfo{
		Created:      inspect.Created,
		ID:           inspect.ID,
		ImageID:      inspect.Image,
		Mounts:       containerMountsFromSummary(inspect.Mounts),
		Name:         strings.TrimPrefix(inspect.Name, "/"),
		RestartCount: inspect.RestartCount,
	}
	if inspect.Config != nil {
		info.Command = inspect.Config.Cmd
		info.Entrypoint = inspect.Config.Entrypoint
		info.Environment = containerEnvironmentFromSDK(inspect.Config.Env)
		info.Image = inspect.Config.Image
		info.Labels = inspect.Config.Labels
		info.User = inspect.Config.User
		info.WorkingDirectory = inspect.Config.WorkingDir
	}
	if inspect.HostConfig != nil {
		info.RestartPolicy = apischema.ContainerRestartPolicy{
			MaximumRetryCount: inspect.HostConfig.RestartPolicy.MaximumRetryCount,
			Name:              string(inspect.HostConfig.RestartPolicy.Name),
		}
	}
	if inspect.State != nil {
		info.State = apischema.ContainerInspectState{
			Dead:       inspect.State.Dead,
			Error:      inspect.State.Error,
			ExitCode:   inspect.State.ExitCode,
			FinishedAt: inspect.State.FinishedAt,
			OOMKilled:  inspect.State.OOMKilled,
			Paused:     inspect.State.Paused,
			Restarting: inspect.State.Restarting,
			Running:    inspect.State.Running,
			StartedAt:  inspect.State.StartedAt,
			Status:     string(inspect.State.Status),
		}
		if inspect.State.Health != nil {
			info.Health = &apischema.ContainerInspectHealth{
				FailingStreak: inspect.State.Health.FailingStreak,
				Status:        string(inspect.State.Health.Status),
			}
		}
	}
	if inspect.NetworkSettings != nil {
		info.Networks = containerEndpointsFromSDK(inspect.NetworkSettings.Networks)
		info.Ports = containerPortBindingsFromSDK(inspect.Config, inspect.NetworkSettings.Ports)
	} else {
		info.Ports = containerPortBindingsFromSDK(inspect.Config, nil)
	}
	return info
}

func containerEnvironmentFromSDK(environment []string) []apischema.ContainerEnvironmentVariable {
	result := make([]apischema.ContainerEnvironmentVariable, 0, len(environment))
	for _, value := range environment {
		name, variableValue, _ := strings.Cut(value, "=")
		result = append(result, apischema.ContainerEnvironmentVariable{Name: name, Value: variableValue})
	}
	slices.SortFunc(result, func(a, b apischema.ContainerEnvironmentVariable) int {
		return strings.Compare(a.Name, b.Name)
	})
	return result
}

func containerPortBindingsFromSDK(config *container.Config, ports network.PortMap) []apischema.ContainerPortBinding {
	allPorts := make(network.PortMap, len(ports))
	if config != nil {
		for port := range config.ExposedPorts {
			allPorts[port] = nil
		}
	}
	maps.Copy(allPorts, ports)

	result := make([]apischema.ContainerPortBinding, 0, len(allPorts))
	for port, bindings := range allPorts {
		if len(bindings) == 0 {
			result = append(result, apischema.ContainerPortBinding{
				ContainerPort: int(port.Num()),
				Protocol:      string(port.Proto()),
			})
			continue
		}
		for _, binding := range bindings {
			result = append(result, apischema.ContainerPortBinding{
				ContainerPort: int(port.Num()),
				HostIP:        addrString(binding.HostIP),
				HostPort:      binding.HostPort,
				Protocol:      string(port.Proto()),
			})
		}
	}
	slices.SortFunc(result, func(a, b apischema.ContainerPortBinding) int {
		if difference := cmp.Compare(a.ContainerPort, b.ContainerPort); difference != 0 {
			return difference
		}
		if difference := strings.Compare(a.Protocol, b.Protocol); difference != 0 {
			return difference
		}
		if difference := strings.Compare(a.HostIP, b.HostIP); difference != 0 {
			return difference
		}
		return strings.Compare(a.HostPort, b.HostPort)
	})
	return result
}

// Start a container by ID
func StartContainer(ctx context.Context, id string) error {
	cli, err := getClient()
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	return startContainer(ctx, cli, id)
}

func startContainer(ctx context.Context, cli containerStarter, id string) error {
	if _, err := cli.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}
	return nil
}

// Stop a container by ID
func StopContainer(ctx context.Context, id string) error {
	cli, err := getClient()
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	return stopContainer(ctx, cli, id)
}

func stopContainer(ctx context.Context, cli containerStopper, id string) error {
	if _, err := cli.ContainerStop(ctx, id, client.ContainerStopOptions{}); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}
	return nil
}

// Restart a container by ID
func RestartContainer(ctx context.Context, id string) error {
	cli, err := getClient()
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	return restartContainer(ctx, cli, id)
}

func restartContainer(ctx context.Context, cli containerRestarter, id string) error {
	if _, err := cli.ContainerRestart(ctx, id, client.ContainerRestartOptions{}); err != nil {
		return fmt.Errorf("failed to restart container: %w", err)
	}
	return nil
}

func PauseContainer(ctx context.Context, id string) error {
	cli, err := getClient()
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	return pauseContainer(ctx, cli, id)
}

func pauseContainer(ctx context.Context, cli containerPauser, id string) error {
	if _, err := cli.ContainerPause(ctx, id, client.ContainerPauseOptions{}); err != nil {
		return fmt.Errorf("failed to pause container: %w", err)
	}
	return nil
}

func UnpauseContainer(ctx context.Context, id string) error {
	cli, err := getClient()
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	return unpauseContainer(ctx, cli, id)
}

func unpauseContainer(ctx context.Context, cli containerUnpauser, id string) error {
	if _, err := cli.ContainerUnpause(ctx, id, client.ContainerUnpauseOptions{}); err != nil {
		return fmt.Errorf("failed to unpause container: %w", err)
	}
	return nil
}

func KillContainer(ctx context.Context, id string) error {
	cli, err := getClient()
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	return killContainer(ctx, cli, id)
}

func killContainer(ctx context.Context, cli containerKiller, id string) error {
	if _, err := cli.ContainerKill(ctx, id, client.ContainerKillOptions{Signal: "SIGKILL"}); err != nil {
		return fmt.Errorf("failed to kill container: %w", err)
	}
	return nil
}

func RemoveContainer(ctx context.Context, id string, force bool) error {
	cli, err := getClient()
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	return removeContainer(ctx, cli, id, force)
}

func removeContainer(ctx context.Context, cli containerRemover, id string, force bool) error {
	if _, err := cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: force}); err != nil {
		return fmt.Errorf("failed to remove container: %w", err)
	}
	return nil
}

// StartAllStopped starts all exited/dead containers and returns counts.
func StartAllStopped(ctx context.Context) (apischema.DockerStartedFailedResponse, error) {
	cli, err := getClient()
	if err != nil {
		return apischema.DockerStartedFailedResponse{}, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return apischema.DockerStartedFailedResponse{}, fmt.Errorf("failed to list containers: %w", err)
	}

	started, failed := 0, 0
	for _, c := range containers.Items {
		if err := ctx.Err(); err != nil {
			return apischema.DockerStartedFailedResponse{}, err
		}
		if c.State == "exited" || c.State == "dead" {
			if _, err := cli.ContainerStart(ctx, c.ID, client.ContainerStartOptions{}); err != nil {
				if ctx.Err() != nil {
					return apischema.DockerStartedFailedResponse{}, ctx.Err()
				}
				slog.Warn("failed to start container", "component", "docker", "container", c.ID[:12], "error", err)
				failed++
			} else {
				started++
			}
		}
	}

	return apischema.DockerStartedFailedResponse{Started: started, Failed: failed}, nil
}

// StopAllRunning stops all running containers and returns counts.
func StopAllRunning(ctx context.Context) (apischema.DockerStoppedFailedResponse, error) {
	cli, err := getClient()
	if err != nil {
		return apischema.DockerStoppedFailedResponse{}, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return apischema.DockerStoppedFailedResponse{}, fmt.Errorf("failed to list containers: %w", err)
	}

	stopped, failed := 0, 0
	for _, c := range containers.Items {
		if err := ctx.Err(); err != nil {
			return apischema.DockerStoppedFailedResponse{}, err
		}
		if c.State == "running" {
			if _, err := cli.ContainerStop(ctx, c.ID, client.ContainerStopOptions{}); err != nil {
				if ctx.Err() != nil {
					return apischema.DockerStoppedFailedResponse{}, ctx.Err()
				}
				slog.Warn("failed to stop container", "component", "docker", "container", c.ID[:12], "error", err)
				failed++
			} else {
				stopped++
			}
		}
	}

	return apischema.DockerStoppedFailedResponse{Stopped: stopped, Failed: failed}, nil
}
