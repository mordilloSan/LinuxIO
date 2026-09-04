package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	procmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/process"
)

func TestAggregateContainerTelemetryKeepsProcessesLiveOnly(t *testing.T) {
	identities := []container.Identity{
		{ID: "aaa111", FullID: "aaa111-full", Name: "web"},
		{ID: "bbb222", FullID: "bbb222-full", Name: "idle"},
	}
	processes := []procmodel.Process{
		{
			PID: 10, ContainerID: "aaa111", ContainerName: "web", CPUPercent: 12.25,
			IOCounters: procmodel.IOCounters{DiskReadBytesPerSecond: 100, DiskWriteBytesPerSecond: 50},
		},
		{
			PID: 11, ContainerID: "aaa111", ContainerName: "web", CPUPercent: 2.5,
			IOCounters: procmodel.IOCounters{DiskReadBytesPerSecond: 25, DiskWriteBytesPerSecond: 10},
		},
		{PID: 99, CPUPercent: 80},
	}
	gpuSamples := []GPUProcessSample{
		{DeviceID: "n0", DeviceName: "GPU", PID: 10, GPUUtilization: new(float64(30)), MemoryBytes: new(uint64(1024)), Source: "nvtop"},
		{DeviceID: "n0", DeviceName: "GPU", PID: 11, GPUUtilization: new(float64(5)), MemoryBytes: new(uint64(2048)), Source: "nvtop"},
		{DeviceID: "n0", DeviceName: "GPU", PID: 99, GPUUtilization: new(float64(90)), MemoryBytes: new(uint64(4096)), Source: "nvtop"},
	}

	got := aggregateContainerTelemetry(identities, processes, gpuSamples)

	require.Len(t, got, 2)
	assert.Equal(t, "aaa111", got[0].ID)
	assert.Equal(t, 2, got[0].ProcessCount)
	assert.InDelta(t, 14.75, got[0].CPUPercent, 0.001)
	assert.Equal(t, uint64(125), got[0].DiskReadBytesPerSecond)
	assert.Equal(t, uint64(60), got[0].DiskWriteBytesPerSecond)
	require.Contains(t, got[0].GPUs, "n0")
	gpu := got[0].GPUs["n0"]
	assert.Equal(t, 2, gpu.ProcessCount)
	require.NotNil(t, gpu.MemoryUsedBytes)
	assert.Equal(t, uint64(3072), *gpu.MemoryUsedBytes)
	require.NotNil(t, gpu.UsagePercent)
	assert.InDelta(t, 35, *gpu.UsagePercent, 0.001)

	assert.Equal(t, "bbb222", got[1].ID)
	assert.Zero(t, got[1].ProcessCount)
	assert.Empty(t, got[1].GPUs)
}

func TestAggregateContainerTelemetryPreservesUnknownGPUUtilization(t *testing.T) {
	got := aggregateContainerTelemetry(
		[]container.Identity{{ID: "c1", Name: "worker"}},
		[]procmodel.Process{{PID: 10, ContainerID: "c1", ContainerName: "worker"}},
		[]GPUProcessSample{{DeviceID: "gpu0", DeviceName: "GPU", PID: 10, MemoryBytes: new(uint64(2048)), Source: "nvml"}},
	)

	require.Len(t, got, 1)
	gpu := got[0].GPUs["gpu0"]
	assert.Nil(t, gpu.UsagePercent)
	require.NotNil(t, gpu.MemoryUsedBytes)
	assert.Equal(t, uint64(2048), *gpu.MemoryUsedBytes)
}

func TestAggregateContainerTelemetryDoesNotDoubleCountDuplicateGPUSamples(t *testing.T) {
	got := aggregateContainerTelemetry(
		[]container.Identity{{ID: "c1", Name: "worker"}},
		[]procmodel.Process{{PID: 10, ContainerID: "c1", ContainerName: "worker"}},
		[]GPUProcessSample{
			{DeviceID: "0", DeviceName: "GPU", PID: 10, MemoryBytes: new(uint64(2048)), Source: "nvml"},
			{DeviceID: "0", DeviceName: "GPU", PID: 10, GPUUtilization: new(float64(25)), Source: "nvtop"},
		},
	)

	require.Len(t, got, 1)
	gpu := got[0].GPUs["0"]
	assert.Equal(t, 1, gpu.ProcessCount)
	require.NotNil(t, gpu.MemoryUsedBytes)
	assert.Equal(t, uint64(2048), *gpu.MemoryUsedBytes)
	require.NotNil(t, gpu.UsagePercent)
	assert.InDelta(t, 25, *gpu.UsagePercent, 0.001)
	assert.Equal(t, "mixed", gpu.Source)
}

func TestAggregateContainerTelemetryPreservesUnknownGPUMemory(t *testing.T) {
	got := aggregateContainerTelemetry(
		[]container.Identity{{ID: "c1", Name: "worker"}},
		[]procmodel.Process{{PID: 10, ContainerID: "c1", ContainerName: "worker"}},
		[]GPUProcessSample{{DeviceID: "gpu0", DeviceName: "GPU", PID: 10, Source: "nvml"}},
	)

	require.Len(t, got, 1)
	assert.Nil(t, got[0].GPUs["gpu0"].MemoryUsedBytes)
}
