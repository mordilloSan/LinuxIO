package system

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/load"
)

// ---------- Types ----------

type CPUInfoResponse struct {
	VendorID           string             `json:"vendorId"`
	ModelName          string             `json:"modelName"`
	Family             string             `json:"family"`
	Model              string             `json:"model"`
	BaseMHz            float64            `json:"mhz"`
	CurrentFrequencies []float64          `json:"currentFrequencies"` // MHz per logical core
	Cores              int                `json:"cores"`              // logical cores
	LoadAverage        *load.AvgStat      `json:"loadAverage,omitempty"`
	PerCoreUsage       []float64          `json:"perCoreUsage"` // %
	Temperature        map[string]float64 `json:"temperature"`  // e.g. {"core0": 42.5, "package": 55.1}
}

type LoadInfoResponse struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

// ---------- Helpers ----------

func getCurrentFrequencies() ([]float64, error) {
	var freqs []float64
	const basePath = "/sys/devices/system/cpu"

	entries, err := os.ReadDir(basePath)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
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
// If it returns nil or errors, we’ll just send an empty map.
func safeTemperatureMap() map[string]float64 {
	m := getTemperatureMap()
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

// ---------- Fetchers ----------

func FetchCPUInfo() (*CPUInfoResponse, error) {
	info, err := cpu.Info()
	if err != nil || len(info) == 0 {
		return nil, err
	}

	percent, _ := cpu.Percent(0, true) // per-core usage snapshot (%)
	counts, _ := cpu.Counts(true)      // logical cores
	loadAvg, _ := load.Avg()
	currentFreqs, _ := getCurrentFrequencies()

	cpuData := info[0]

	return &CPUInfoResponse{
		VendorID:           cpuData.VendorID,
		ModelName:          cpuData.ModelName,
		Family:             cpuData.Family,
		Model:              cpuData.Model,
		BaseMHz:            cpuData.Mhz,
		CurrentFrequencies: currentFreqs,
		Cores:              counts,
		LoadAverage:        loadAvg,
		PerCoreUsage:       percent,
		Temperature:        safeTemperatureMap(),
	}, nil
}

func FetchLoadInfo() (*LoadInfoResponse, error) {
	loadAvg, err := load.Avg()
	if err != nil {
		return nil, err
	}
	return &LoadInfoResponse{
		Load1:  loadAvg.Load1,
		Load5:  loadAvg.Load5,
		Load15: loadAvg.Load15,
	}, nil
}
