package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/mordilloSan/go-logger/logger"
)

type Metrics struct {
	CPUPercent float64 `json:"cpu_percent"`
	MemUsage   uint64  `json:"mem_usage"`
	MemLimit   uint64  `json:"mem_limit"`
	NetInput   uint64  `json:"net_input"`
	NetOutput  uint64  `json:"net_output"`
	BlockRead  uint64  `json:"block_read"`
	BlockWrite uint64  `json:"block_write"`
}

type ContainerWithMetrics struct {
	container.Summary
	Metrics   *Metrics `json:"metrics,omitempty"`
	Icon      string   `json:"icon,omitempty"`
	URL       string   `json:"url,omitempty"`
	ProxyPort string   `json:"proxyPort,omitempty"`
}

// List all containers with metrics.
func ListContainers(ctx context.Context) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var enriched []ContainerWithMetrics

	for _, ctr := range containers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		metrics := collectContainerMetrics(ctx, cli, ctr.ID)
		iconIdentifier, resolvedURL, proxyPort := resolveContainerPresentation(ctx, ctr)

		enriched = append(enriched, ContainerWithMetrics{
			Summary:   ctr,
			Metrics:   metrics,
			Icon:      iconIdentifier,
			URL:       resolvedURL,
			ProxyPort: proxyPort,
		})
	}

	return enriched, nil
}

func collectContainerMetrics(ctx context.Context, cli *client.Client, containerID string) *Metrics {
	metrics := &Metrics{}
	statsResp, err := cli.ContainerStatsOneShot(ctx, containerID)
	if err != nil {
		return metrics
	}
	defer func() {
		if cerr := statsResp.Body.Close(); cerr != nil {
			logger.Warnf("failed to close container stats body: %v", cerr)
		}
	}()

	var stats struct {
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

	if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
		return metrics
	}
	populateContainerCPUMetrics(metrics, stats)
	populateContainerMemoryMetrics(metrics, stats)
	populateContainerIOMetrics(metrics, stats)
	return metrics
}

func populateContainerCPUMetrics(metrics *Metrics, stats struct {
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
}) {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemCPUUsage)
	if systemDelta > 0 && len(stats.CPUStats.CPUUsage.PercpuUsage) > 0 {
		metrics.CPUPercent = (cpuDelta / systemDelta) * float64(len(stats.CPUStats.CPUUsage.PercpuUsage)) * 100.0
	}
}

func populateContainerMemoryMetrics(metrics *Metrics, stats struct {
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
}) {
	memUsage := stats.MemoryStats.Usage
	if inactiveFile, ok := stats.MemoryStats.Stats["inactive_file"]; ok && inactiveFile < memUsage {
		memUsage -= inactiveFile
	}
	metrics.MemUsage = memUsage
	metrics.MemLimit = stats.MemoryStats.Limit
}

func populateContainerIOMetrics(metrics *Metrics, stats struct {
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
}) {
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

func resolveContainerPresentation(ctx context.Context, ctr container.Summary) (string, string, string) {
	containerIcon := ctr.Labels["io.linuxio.container.icon"]
	containerURL := ctr.Labels["io.linuxio.container.url"]
	proxyPort := ctr.Labels[ProxyPortLabel]
	iconName := containerIconName(ctr)
	resolvedURL := resolveContainerURL(ctx, ctr, containerURL, proxyPort, iconName)
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

func resolveContainerURL(ctx context.Context, ctr container.Summary, containerURL, proxyPort, iconName string) string {
	if containerURL != "" || proxyPort == "" || iconName == "" {
		return containerURL
	}
	if ctr.State == "running" {
		connectToProxyNetwork(ctx, ctr.ID)
	}
	return "/proxy/" + iconName + "/"
}

// Start a container by ID
func StartContainer(ctx context.Context, id string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	if err := cli.ContainerStart(ctx, id, container.StartOptions{}); err != nil {
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
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	if err := cli.ContainerStop(ctx, id, container.StopOptions{}); err != nil {
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
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	if err := cli.ContainerRemove(ctx, id, container.RemoveOptions{Force: true}); err != nil {
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
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	if err := cli.ContainerRestart(ctx, id, container.StopOptions{}); err != nil {
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
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	started, failed := 0, 0
	for _, c := range containers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if c.State == "exited" || c.State == "dead" {
			if err := cli.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				logger.Warnf("failed to start container %s: %v", c.ID[:12], err)
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
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	stopped, failed := 0, 0
	for _, c := range containers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if c.State == "running" {
			if err := cli.ContainerStop(ctx, c.ID, container.StopOptions{}); err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				logger.Warnf("failed to stop container %s: %v", c.ID[:12], err)
				failed++
			} else {
				stopped++
			}
		}
	}

	return map[string]any{"stopped": stopped, "failed": failed}, nil
}
