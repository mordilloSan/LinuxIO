package system

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var cpuInfoCache hwSnapshotCache[*apischema.CPUInfoResponse]

func FetchCPUInfo(ctx context.Context) (*apischema.CPUInfoResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return cpuInfoCache.get(func() (*apischema.CPUInfoResponse, error) {
		info, err := cpu.InfoWithContext(ctx)
		if err != nil {
			return nil, err
		}
		if len(info) == 0 {
			return nil, fmt.Errorf("cpu information unavailable")
		}
		counts, err := cpu.CountsWithContext(ctx, true)
		if err != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if counts <= 0 {
			counts = len(info)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &apischema.CPUInfoResponse{
			VendorID:  info[0].VendorID,
			ModelName: info[0].ModelName,
			Family:    info[0].Family,
			Model:     info[0].Model,
			MHz:       readBaseFrequencyMHz(),
			Cores:     counts,
		}, nil
	})
}

func readBaseFrequencyMHz() float64 {
	for _, name := range []string{"cpuinfo_base_freq", "base_frequency", "cpuinfo_max_freq"} {
		data, err := os.ReadFile(filepath.Join("/sys/devices/system/cpu/cpu0/cpufreq", name))
		if err != nil {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
		if err == nil && value > 0 {
			return value / 1000
		}
	}
	return 0
}
