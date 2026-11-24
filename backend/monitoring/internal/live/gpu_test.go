package live

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector/gpu"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

func TestMergeCollectorGPUUsesPCIAddressOverCollectorID(t *testing.T) {
	temperature := 72.5
	result := map[string]monitoringapi.LiveGPU{
		"0000:01:00.0": {},
	}
	mergeCollectorGPU(result, "0", systemmodel.GPUData{Address: "0000:01:00.0", Temperature: temperature})

	if len(result) != 1 {
		t.Fatalf("merged GPU map = %#v", result)
	}
	if result["0000:01:00.0"].TemperatureC == nil || *result["0000:01:00.0"].TemperatureC != temperature {
		t.Fatalf("PCI keyed GPU = %#v", result)
	}
}

func TestGetCurrentDataPreservesPCIAddressForLiveJoin(t *testing.T) {
	temperature := 72.5
	manager := &gpu.Manager{GpuDataMap: map[string]*systemmodel.GPUData{
		"0": {Name: "GPU", Address: "0000:01:00.0", Temperature: temperature, Count: 1},
	}}
	data := manager.GetCurrentData(77)
	result := map[string]monitoringapi.LiveGPU{"0000:01:00.0": {}}
	mergeCollectorGPU(result, "0", data["0"])

	if len(result) != 1 || result["0000:01:00.0"].TemperatureC == nil || *result["0000:01:00.0"].TemperatureC != temperature {
		t.Fatalf("live GPU join = %#v", result)
	}
}

// The driver version, memory size, power limit and top clock are asked for
// once per nvidiaStaticTTL; the changing figures on every call.
func TestNvidiaSMIStatsAsksForTheStaticFiguresOnce(t *testing.T) {
	queries := map[string]int{}
	oldRun := nvidiaSMIRun
	nvidiaSMIRun = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		fields := strings.TrimPrefix(args[0], "--query-gpu=")
		queries[fields]++
		if strings.Contains(fields, "driver_version") {
			return []byte("00000000:01:00.0, 550.54, 8192, 200.00, 2100\n"), nil
		}
		return []byte("00000000:01:00.0, 61, 30, 75, 1024, 120.50, 1800\n"), nil
	}
	nvidiaStatic.Lock()
	nvidiaStatic.stats, nvidiaStatic.at = nil, time.Time{}
	nvidiaStatic.Unlock()
	t.Cleanup(func() {
		nvidiaSMIRun = oldRun
		nvidiaStatic.Lock()
		nvidiaStatic.stats, nvidiaStatic.at = nil, time.Time{}
		nvidiaStatic.Unlock()
	})

	for range 3 {
		stats := nvidiaSMIStats(t.Context(), "nvidia-smi")
		require.Len(t, stats, 1)
		for _, gpu := range stats {
			assert.Equal(t, nvidiaGPUStats{
				DriverVersion: "550.54", TemperatureC: 61, FanPercent: 30, UtilizationPercent: 75,
				MemoryTotalBytes: 8192 << 20, MemoryUsedBytes: 1024 << 20, PowerDrawWatts: 120.5,
				PowerLimitWatts: 200, CurrentGraphicsMHz: 1800, MaxGraphicsClockMHz: 2100,
			}, gpu)
		}
	}
	assert.Equal(t, map[string]int{
		"pci.bus_id,driver_version,memory.total,power.limit,clocks.max.graphics":                              1,
		"pci.bus_id,temperature.gpu,fan.speed,utilization.gpu,memory.used,power.draw,clocks.current.graphics": 3,
	}, queries)
}
