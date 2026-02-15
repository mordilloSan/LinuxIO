package system

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/shirou/gopsutil/v4/mem"
)

// ---------- Public types & handler ----------

type MemoryResponse struct {
	System *mem.VirtualMemoryStat `json:"system"`
	Docker struct {
		Used uint64 `json:"used"`
	} `json:"docker"`
	ZFS struct {
		ARC uint64 `json:"arc"`
	} `json:"zfs"`
}

// FetchMemoryInfo returns the system memory, docker usage, and ZFS ARC cache info.
func FetchMemoryInfo() (*MemoryResponse, error) {
	vm, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	var resp MemoryResponse
	resp.System = vm
	resp.ZFS.ARC = readZFSArc()

	if used, err := getDockerMemoryUsage(); err == nil {
		resp.Docker.Used = used
		// (Intentionally ignore Docker errors to keep endpoint useful without Docker.)
	}

	return &resp, nil
}

// ---------- ZFS ARC ----------

func readZFSArc() uint64 {
	// /proc/spl/kstat/zfs/arcstats exists on ZFS systems
	data, err := os.ReadFile("/proc/spl/kstat/zfs/arcstats")
	if err != nil {
		return 0
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		// format line looks like: "size    4    1234567890"
		if !strings.HasPrefix(line, "size") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 3 {
			if val, err := strconv.ParseUint(fields[2], 10, 64); err == nil {
				return val
			}
		}
	}
	return 0
}

// ---------- Docker stats (efficient, cgroup-aware) ----------

var (
	dockerOnce sync.Once
	dockerCli  *client.Client
	dockerErr  error
)

func dockerClient() (*client.Client, error) {
	dockerOnce.Do(func() {
		dockerCli, dockerErr = client.NewClientWithOpts(
			client.FromEnv,
			client.WithAPIVersionNegotiation(),
		)
	})
	return dockerCli, dockerErr
}

// getDockerMemoryUsage returns the sum of "current" memory usage across containers.
// We approximate current as usage - inactive_file when available (cgroup v2 style).
func getDockerMemoryUsage() (uint64, error) {
	cli, err := dockerClient()
	if err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return 0, err
	}
	if len(containers) == 0 {
		return 0, nil
	}

	type agg struct{ n uint64 }
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, 8) // bound concurrency
		ch  = make(chan agg, len(containers))
	)

	for _, ctr := range containers {
		id := ctr.ID

		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			statsCtx, cancelStats := context.WithTimeout(ctx, 1500*time.Millisecond)
			defer cancelStats()

			stats, err := cli.ContainerStatsOneShot(statsCtx, id)
			if err != nil {
				return
			}
			defer stats.Body.Close()

			var s struct {
				MemoryStats struct {
					Usage uint64            `json:"usage"`
					Stats map[string]uint64 `json:"stats"`
				} `json:"memory_stats"`
			}
			if err := json.NewDecoder(stats.Body).Decode(&s); err != nil {
				return
			}

			// cgroup v2 “current” heuristic: usage - inactive_file (when present)
			usage := s.MemoryStats.Usage
			if inact, ok := s.MemoryStats.Stats["inactive_file"]; ok && inact < usage {
				usage -= inact
			}

			ch <- agg{n: usage}
		})
	}

	wg.Wait()
	close(ch)

	var total uint64
	for v := range ch {
		total += v.n
	}
	return total, nil
}
