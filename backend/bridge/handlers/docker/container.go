package docker

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

type dockerStatsPayload struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage  uint64   `json:"total_usage"`
			PercpuUsage []uint64 `json:"percpu_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"cpu_stats"`
	MemoryStats struct {
		Usage uint64            `json:"usage"`
		Limit uint64            `json:"limit"`
		Stats map[string]uint64 `json:"stats"`
	} `json:"memory_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	BlkioStats struct {
		IoServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
}

// List all containers with metrics.
func ListContainers(ctx context.Context) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	enriched := make([]apischema.ContainerInfo, 0, len(containers.Items))

	for _, ctr := range containers.Items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		metrics := collectContainerMetrics(ctx, cli, ctr.ID)
		iconIdentifier, resolvedURL, proxyPort := resolveContainerPresentation(ctr)

		enriched = append(enriched, containerInfoFromSummary(ctr, metrics, iconIdentifier, resolvedURL, proxyPort))
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
	return apischema.ContainerInfo{
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

	networks := make(map[string]apischema.ContainerEndpoint, len(settings.Networks))
	for name, endpoint := range settings.Networks {
		if endpoint == nil {
			continue
		}
		networks[name] = apischema.ContainerEndpoint{
			Gateway:           addrString(endpoint.Gateway),
			GlobalIPv6Address: optionalAddrString(endpoint.GlobalIPv6Address),
			IPAddress:         addrString(endpoint.IPAddress),
			MACAddress:        utils.OptionalString(endpoint.MacAddress.String()),
		}
	}
	if len(networks) == 0 {
		return nil
	}
	return &apischema.ContainerNetworkSettings{Networks: networks}
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
			Mode:        mount.Mode,
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

func collectContainerMetrics(ctx context.Context, cli *client.Client, containerID string) *apischema.ContainerMetrics {
	metrics := &apischema.ContainerMetrics{}
	statsResp, err := cli.ContainerStats(ctx, containerID, client.ContainerStatsOptions{})
	if err != nil {
		return metrics
	}
	defer func() {
		if cerr := statsResp.Body.Close(); cerr != nil {
			slog.Warn("failed to close container stats body", "component", "docker", "container", containerID, "error", cerr)
		}
	}()

	var stats dockerStatsPayload

	if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
		return metrics
	}
	populateContainerCPUMetrics(metrics, stats)
	populateContainerMemoryMetrics(metrics, stats)
	populateContainerIOMetrics(metrics, stats)
	return metrics
}

func populateContainerCPUMetrics(metrics *apischema.ContainerMetrics, stats dockerStatsPayload) {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemCPUUsage)
	if systemDelta > 0 && len(stats.CPUStats.CPUUsage.PercpuUsage) > 0 {
		metrics.CPUPercent = (cpuDelta / systemDelta) * float64(len(stats.CPUStats.CPUUsage.PercpuUsage)) * 100.0
	}
}

func populateContainerMemoryMetrics(metrics *apischema.ContainerMetrics, stats dockerStatsPayload) {
	memUsage := stats.MemoryStats.Usage
	if inactiveFile, ok := stats.MemoryStats.Stats["inactive_file"]; ok && inactiveFile < memUsage {
		memUsage -= inactiveFile
	}
	metrics.MemUsage = memUsage
	metrics.MemLimit = stats.MemoryStats.Limit
}

func populateContainerIOMetrics(metrics *apischema.ContainerMetrics, stats dockerStatsPayload) {
	for _, netStats := range stats.Networks {
		metrics.NetInput += netStats.RxBytes
		metrics.NetOutput += netStats.TxBytes
	}
	for _, entry := range stats.BlkioStats.IoServiceBytesRecursive {
		switch entry.Op {
		case "Read":
			metrics.BlockRead += entry.Value
		case "Write":
			metrics.BlockWrite += entry.Value
		}
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

// Start a container by ID
func StartContainer(ctx context.Context, id string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	if _, err := cli.ContainerStart(ctx, id, client.ContainerStartOptions{}); err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	return "started", nil
}

// Stop a container by ID
func StopContainer(ctx context.Context, id string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	if _, err := cli.ContainerStop(ctx, id, client.ContainerStopOptions{}); err != nil {
		return nil, fmt.Errorf("failed to stop container: %w", err)
	}

	return "stopped", nil
}

// Remove a container by ID
func RemoveContainer(ctx context.Context, id string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	if _, err := cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true}); err != nil {
		return nil, fmt.Errorf("failed to remove container: %w", err)
	}

	return "removed", nil
}

// Restart a container by ID
func RestartContainer(ctx context.Context, id string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	if _, err := cli.ContainerRestart(ctx, id, client.ContainerRestartOptions{}); err != nil {
		return nil, fmt.Errorf("failed to restart container: %w", err)
	}

	return "restarted", nil
}

// StartAllStopped starts all exited/dead containers and returns counts.
func StartAllStopped(ctx context.Context) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	started, failed := 0, 0
	for _, c := range containers.Items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if c.State == "exited" || c.State == "dead" {
			if _, err := cli.ContainerStart(ctx, c.ID, client.ContainerStartOptions{}); err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				slog.Warn("failed to start container", "component", "docker", "container", c.ID[:12], "error", err)
				failed++
			} else {
				started++
			}
		}
	}

	return map[string]any{"started": started, "failed": failed}, nil
}

// StopAllRunning stops all running containers and returns counts.
func StopAllRunning(ctx context.Context) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	stopped, failed := 0, 0
	for _, c := range containers.Items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if c.State == "running" {
			if _, err := cli.ContainerStop(ctx, c.ID, client.ContainerStopOptions{}); err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				slog.Warn("failed to stop container", "component", "docker", "container", c.ID[:12], "error", err)
				failed++
			} else {
				stopped++
			}
		}
	}

	return map[string]any{"stopped": stopped, "failed": failed}, nil
}
