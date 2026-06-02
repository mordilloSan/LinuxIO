package system

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/load"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// ---------- Helpers ----------

func getCurrentFrequencies(ctx context.Context) ([]float64, error) {
	var freqs []float64
	const basePath = "/sys/devices/system/cpu"

	entries, err := os.ReadDir(basePath)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), "cpu") {
			continue
		}

		cpuPath := filepath.Join(basePath, entry.Name(), "cpufreq", "scaling_cur_freq")
		data, err := os.ReadFile(cpuPath)
		if err != nil {
			continue // skip offline or inaccessible cores
		}

		kHzStr := strings.TrimSpace(string(data))
		kHz, err := strconv.ParseFloat(kHzStr, 64)
		if err != nil {
			continue
		}

		freqs = append(freqs, kHz/1000.0) // MHz
	}

	return freqs, nil
}

// NOTE: Assuming you already have getTemperatureMap() elsewhere in this package.
// If it returns nil or errors, we'll just send an empty map.
func safeTemperatureMap(ctx context.Context) map[string]float64 {
	m := getTemperatureMap(ctx)
	if m == nil {
		return map[string]float64{}
	}
	// Filter to CPU-related temps only
	cpuTemps := make(map[string]float64, len(m))
	for k, v := range m {
		if strings.HasPrefix(k, "core") || k == "package" {
			cpuTemps[k] = v
		}
	}
	return cpuTemps
}

func FetchPreferredCPUTemperature(ctx context.Context) (float64, bool) {
	temps := safeTemperatureMap(ctx)
	if len(temps) == 0 {
		return 0, false
	}
	if packageTemp, ok := temps["package"]; ok {
		return packageTemp, true
	}

	keys := make([]string, 0, len(temps))
	for key := range temps {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return temps[keys[0]], true
}

// ---------- Fetchers ----------

func FetchCPUInfo(ctx context.Context) (*apischema.CPUInfoResponse, error) {
	info, err := cpu.InfoWithContext(ctx)
	if err != nil || len(info) == 0 {
		return nil, err
	}

	percent, _ := cpu.PercentWithContext(ctx, 0, true) // per-core usage snapshot (%)
	counts, _ := cpu.CountsWithContext(ctx, true)      // logical cores
	loadAvg, _ := load.AvgWithContext(ctx)
	currentFreqs, _ := getCurrentFrequencies(ctx)

	cpuData := info[0]

	return &apischema.CPUInfoResponse{
		VendorID:           cpuData.VendorID,
		ModelName:          cpuData.ModelName,
		Family:             cpuData.Family,
		Model:              cpuData.Model,
		MHz:                cpuData.Mhz,
		CurrentFrequencies: currentFreqs,
		Cores:              counts,
		LoadAverage:        cpuLoadAverage(loadAvg),
		PerCoreUsage:       percent,
		Temperature:        safeTemperatureMap(ctx),
	}, nil
}

func FetchLoadInfo(ctx context.Context) (*apischema.CPULoadAverage, error) {
	loadAvg, err := load.AvgWithContext(ctx)
	if err != nil {
		return nil, err
	}
	return cpuLoadAverage(loadAvg), nil
}

func cpuLoadAverage(loadAvg *load.AvgStat) *apischema.CPULoadAverage {
	if loadAvg == nil {
		return nil
	}
	return &apischema.CPULoadAverage{
		Load1:  loadAvg.Load1,
		Load5:  loadAvg.Load5,
		Load15: loadAvg.Load15,
	}
}
