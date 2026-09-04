package container

import (
	"encoding/json"
	"testing"
)

func TestTelemetryJSONPreservesKnownZeroAndUnknownGPUMetrics(t *testing.T) {
	zeroMemory := uint64(0)
	zeroUsage := 0.0
	payload, err := json.Marshal([]Telemetry{{
		ID: "c1",
		GPUs: map[string]GPUUsage{
			"known":   {MemoryUsedBytes: &zeroMemory, UsagePercent: &zeroUsage},
			"unknown": {},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}

	var decoded []Telemetry
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	known := decoded[0].GPUs["known"]
	if known.MemoryUsedBytes == nil || *known.MemoryUsedBytes != 0 || known.UsagePercent == nil || *known.UsagePercent != 0 {
		t.Fatalf("known zero metrics were lost: %#v", known)
	}
	unknown := decoded[0].GPUs["unknown"]
	if unknown.MemoryUsedBytes != nil || unknown.UsagePercent != nil {
		t.Fatalf("unknown metrics became known zero: %#v", unknown)
	}
}
