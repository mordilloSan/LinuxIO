package docker

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"regexp"

	"io"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/mordilloSan/LinuxIO/backend/common/logger"
	"golang.org/x/sys/unix"
)

type Metrics struct {
	CPUPercent     float64 `json:"cpu_percent"`
	MemUsage       uint64  `json:"mem_usage"`
	MemLimit       uint64  `json:"mem_limit"`
	SystemMemTotal uint64  `json:"system_mem_total"`
	NetInput       uint64  `json:"net_input"`
	NetOutput      uint64  `json:"net_output"`
	BlockRead      uint64  `json:"block_read"`
	BlockWrite     uint64  `json:"block_write"`
}

type ContainerWithMetrics struct {
	types.Container
	Metrics *Metrics `json:"metrics,omitempty"`
}

// Helper to get a docker client
func getClient() (*client.Client, error) {
	return client.NewClientWithOpts(client.FromEnv)
}

// Helper to get full system mm
func getSystemMemoryTotal() (uint64, error) {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return 0, err
	}
	return uint64(info.Totalram) * uint64(info.Unit), nil
}

// Helper to demultiplex Docker log stream into clean text
func demuxDockerLogs(reader io.Reader) (string, error) {
	var logs strings.Builder
	header := make([]byte, 8)
	for {
		// Each frame starts with 8 bytes: [STREAM][0,0,0][SIZE(4 bytes)]
		_, err := io.ReadFull(reader, header)
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		size := int(binary.BigEndian.Uint32(header[4:]))
		if size == 0 {
			continue
		}
		frame := make([]byte, size)
		_, err = io.ReadFull(reader, frame)
		if err != nil {
			break
		}
		logs.Write(frame)
	}
	return logs.String(), nil
}

// List all containers with metrics
func ListContainers() (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers: %w", err)
	}

	var enriched []ContainerWithMetrics

	sysMemTotal, sysErr := getSystemMemoryTotal() // call once before the loop

	for _, ctr := range containers {
		metrics := &Metrics{}
		statsResp, err := cli.ContainerStatsOneShot(context.Background(), ctr.ID)
		if err == nil {
			var stats struct {
				CPUStats struct {
					CPUUsage struct {
						TotalUsage  uint64   `json:"total_usage"`
						PercpuUsage []uint64 `json:"percpu_usage"`
					} `json:"cpu_usage"`
					SystemCPUUsage uint64 `json:"system_cpu_usage"`
				} `json:"cpu_stats"`
				MemoryStats struct {
					Usage uint64 `json:"usage"`
					Limit uint64 `json:"limit"`
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

			if err := json.NewDecoder(statsResp.Body).Decode(&stats); err == nil {
				// CPU as before
				cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage)
				systemDelta := float64(stats.CPUStats.SystemCPUUsage)
				if systemDelta > 0 && len(stats.CPUStats.CPUUsage.PercpuUsage) > 0 {
					metrics.CPUPercent = (cpuDelta / systemDelta) * float64(len(stats.CPUStats.CPUUsage.PercpuUsage)) * 100.0
				}

				metrics.MemUsage = stats.MemoryStats.Usage
				metrics.MemLimit = stats.MemoryStats.Limit
				if sysErr == nil {
					metrics.SystemMemTotal = sysMemTotal
				}

				// net & block as before...
				for _, net := range stats.Networks {
					metrics.NetInput += net.RxBytes
					metrics.NetOutput += net.TxBytes
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
			if cerr := statsResp.Body.Close(); cerr != nil {
				logger.Warnf("failed to close container stats body: %v", cerr)
			}
		}

		enriched = append(enriched, ContainerWithMetrics{
			Container: ctr,
			Metrics:   metrics,
		})
	}

	return enriched, nil
}

// Start a container by ID
func StartContainer(id string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	if err := cli.ContainerStart(context.Background(), id, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("failed to start container: %w", err)
	}

	return "started", nil
}

// Stop a container by ID
func StopContainer(id string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	if err := cli.ContainerStop(context.Background(), id, container.StopOptions{}); err != nil {
		return nil, fmt.Errorf("failed to stop container: %w", err)
	}

	return "stopped", nil
}

// Remove a container by ID
func RemoveContainer(id string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	if err := cli.ContainerRemove(context.Background(), id, container.RemoveOptions{Force: true}); err != nil {
		return nil, fmt.Errorf("failed to remove container: %w", err)
	}

	return "removed", nil
}

// Restart a container by ID
func RestartContainer(id string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	if err := cli.ContainerRestart(context.Background(), id, container.StopOptions{}); err != nil {
		return nil, fmt.Errorf("failed to restart container: %w", err)
	}

	return "restarted", nil
}

// GetContainerLogs fetches logs (stdout + stderr) for a container by ID.
func LogContainer(id string) (string, error) {
	cli, err := getClient()
	if err != nil {
		return "", fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: false,
		Follow:     false,
		Tail:       "100",
	}

	reader, err := cli.ContainerLogs(context.Background(), id, options)
	if err != nil {
		return "", fmt.Errorf("failed to get logs: %w", err)
	}

	plainLogs, err := demuxDockerLogs(reader)
	closeErr := reader.Close() // Close explicitly, handle error

	if closeErr != nil {
		// Log the close error but do not override the main error
		logger.Warnf("failed to close container logs reader: %v", closeErr)
	}

	if err != nil {
		return "", fmt.Errorf("failed to decode logs: %w", err)
	}
	cleanLogs := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`).ReplaceAllString(plainLogs, "")
	return cleanLogs, nil
}
