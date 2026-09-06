package app

import (
	"testing"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

func TestMergeCollectorGPUUsesPCIAddressOverCollectorID(t *testing.T) {
	temperature := 72.5
	result := map[string]monitoringapi.LiveGPU{
		"0000:01:00.0": {},
	}
	mergeCollectorGPU(result, "0", system.GPUData{Address: "0000:01:00.0", Temperature: temperature})

	if len(result) != 1 {
		t.Fatalf("merged GPU map = %#v", result)
	}
	if result["0000:01:00.0"].TemperatureC == nil || *result["0000:01:00.0"].TemperatureC != temperature {
		t.Fatalf("PCI keyed GPU = %#v", result)
	}
}

func TestGetCurrentDataPreservesPCIAddressForLiveJoin(t *testing.T) {
	temperature := 72.5
	manager := &GPUManager{GpuDataMap: map[string]*system.GPUData{
		"0": {Name: "GPU", Address: "0000:01:00.0", Temperature: temperature, Count: 1},
	}}
	data := manager.GetCurrentData(77)
	result := map[string]monitoringapi.LiveGPU{"0000:01:00.0": {}}
	mergeCollectorGPU(result, "0", data["0"])

	if len(result) != 1 || result["0000:01:00.0"].TemperatureC == nil || *result["0000:01:00.0"].TemperatureC != temperature {
		t.Fatalf("live GPU join = %#v", result)
	}
}
