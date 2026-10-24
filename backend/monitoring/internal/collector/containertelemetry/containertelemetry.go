// Package containertelemetry folds the live process list and GPU process
// samples into the one bounded telemetry record kept per running container.
package containertelemetry

import (
	"sort"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector/gpu"
	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	processmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/process"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/units"
)

type containerGPUAccumulator struct {
	value          containermodel.GPUUsage
	memory         uint64
	memoryKnown    bool
	memoryComplete bool
	usage          float64
	usageKnown     bool
	encode         float64
	encodeKnown    bool
	decode         float64
	decodeKnown    bool
}

// Aggregate turns the live process list and transient GPU
// process samples into the only high-cardinality boundary that is persisted:
// one bounded record per running container.
//
//nolint:gocognit // Aggregation intentionally handles each optional process/GPU metric in one pass.
func Aggregate(
	identities []containermodel.Identity,
	processes []processmodel.Process,
	gpuSamples []gpu.ProcessSample,
) []containermodel.Telemetry {
	byID := make(map[string]*containermodel.Telemetry, len(identities))
	for _, identity := range identities {
		id := identity.ID
		if id == "" {
			id = identity.FullID
		}
		if id == "" {
			continue
		}
		byID[id] = &containermodel.Telemetry{ID: id, Name: identity.Name}
	}

	processContainer := make(map[int32]string, len(processes))
	for _, process := range processes {
		if process.ContainerID == "" {
			continue
		}
		item := byID[process.ContainerID]
		if item == nil {
			item = &containermodel.Telemetry{ID: process.ContainerID, Name: process.ContainerName}
			byID[process.ContainerID] = item
		}
		if process.ContainerName != "" {
			item.Name = process.ContainerName
		}
		item.ProcessCount++
		item.CPUPercent += process.CPUPercent
		item.DiskReadBytesPerSecond += process.IOCounters.DiskReadBytesPerSecond
		item.DiskWriteBytesPerSecond += process.IOCounters.DiskWriteBytesPerSecond
		processContainer[process.PID] = process.ContainerID
	}

	gpuAccumulators := make(map[string]map[string]*containerGPUAccumulator)
	for _, sample := range gpu.UniqueProcessSamples(gpuSamples) {
		containerID := processContainer[sample.PID]
		if containerID == "" {
			continue
		}
		item := byID[containerID]
		if item == nil {
			continue
		}
		deviceID := sample.DeviceID
		if deviceID == "" {
			deviceID = sample.DeviceName
		}
		if deviceID == "" {
			continue
		}
		devices := gpuAccumulators[containerID]
		if devices == nil {
			devices = make(map[string]*containerGPUAccumulator)
			gpuAccumulators[containerID] = devices
		}
		acc := devices[deviceID]
		if acc == nil {
			acc = &containerGPUAccumulator{
				value:          containermodel.GPUUsage{Name: sample.DeviceName, Source: sample.Source},
				memoryComplete: true,
			}
			devices[deviceID] = acc
		}
		acc.value.ProcessCount++
		if sample.MemoryBytes != nil {
			acc.memory += *sample.MemoryBytes
			acc.memoryKnown = true
		} else {
			acc.memoryComplete = false
		}
		if sample.GPUUtilization != nil {
			acc.usage += *sample.GPUUtilization
			acc.usageKnown = true
		}
		if sample.EncodeUtilization != nil {
			acc.encode += *sample.EncodeUtilization
			acc.encodeKnown = true
		}
		if sample.DecodeUtilization != nil {
			acc.decode += *sample.DecodeUtilization
			acc.decodeKnown = true
		}
		acc.value.Source = gpu.MergeProcessSource(acc.value.Source, sample.Source)
	}

	result := make([]containermodel.Telemetry, 0, len(byID))
	for id, item := range byID {
		item.CPUPercent = units.TwoDecimals(item.CPUPercent)
		if devices := gpuAccumulators[id]; len(devices) > 0 {
			item.GPUs = make(map[string]containermodel.GPUUsage, len(devices))
			for deviceID, acc := range devices {
				if acc.memoryKnown && acc.memoryComplete {
					memory := acc.memory
					acc.value.MemoryUsedBytes = &memory
				}
				if acc.usageKnown {
					usage := units.TwoDecimals(acc.usage)
					acc.value.UsagePercent = &usage
				}
				if acc.encodeKnown {
					encode := units.TwoDecimals(acc.encode)
					acc.value.EncodePercent = &encode
				}
				if acc.decodeKnown {
					decode := units.TwoDecimals(acc.decode)
					acc.value.DecodePercent = &decode
				}
				item.GPUs[deviceID] = acc.value
			}
		}
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID != result[j].ID {
			return result[i].ID < result[j].ID
		}
		return result[i].Name < result[j].Name
	})
	return result
}
