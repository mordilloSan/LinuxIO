package system

import (
	"context"
	"encoding/json"
	"go-backend/internal/logger"
	"os"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/mem"
)

// FetchMemoryInfo returns the system memory, docker usage, and ZFS arc cache info
func FetchMemoryInfo() (map[string]any, error) {
	memInfo, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	// ZFS ARC Cache (if available)
	var arc uint64
	if data, err := os.ReadFile("/proc/spl/kstat/zfs/arcstats"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "size") {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					if val, err := strconv.ParseUint(fields[2], 10, 64); err == nil {
						arc = val
					}
				}
				break
			}
		}
	}

	dockerUsed, _ := getDockerMemoryUsage()

	return map[string]any{
		"system": memInfo,
		"docker": map[string]any{"used": dockerUsed},
		"zfs":    map[string]any{"arc": arc},
	}, nil
}

func getMemInfo(c *gin.Context) {
	data, err := FetchMemoryInfo()
	if err != nil {
		c.JSON(500, gin.H{"error": "failed to get memory info", "details": err.Error()})
		return
	}
	c.JSON(200, data)
}

func getDockerMemoryUsage() (uint64, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return 0, err
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{All: true})
	if err != nil {
		return 0, err
	}

	var total uint64
	for _, container := range containers {
		statsResp, err := cli.ContainerStatsOneShot(context.Background(), container.ID)
		if err != nil {
			continue
		}
		func() {
			defer func() {
				if cerr := statsResp.Body.Close(); cerr != nil {
					logger.Warnf("failed to close container stats body: %v", cerr)
				}
			}()
			// ... process statsResp.Body here ...
		}()

		var stats struct {
			MemoryStats struct {
				Usage uint64 `json:"usage"`
			} `json:"memory_stats"`
		}
		if err := json.NewDecoder(statsResp.Body).Decode(&stats); err != nil {
			continue
		}

		total += stats.MemoryStats.Usage
	}

	return total, nil
}
