package app

import (
	"sort"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	procmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/process"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/utils"
)

type containerGPUAccumulator struct {
	value          container.GPUUsage
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

// aggregateContainerTelemetry turns the live process list and transient GPU
// process samples into the only high-cardinality boundary that is persisted:
// one bounded record per running container.
//
//nolint:gocognit // Aggregation intentionally handles each optional process/GPU metric in one pass.
func aggregateContainerTelemetry(
	identities []container.Identity,
	processes []procmodel.Process,
	gpuSamples []GPUProcessSample,
) []container.Telemetry {
	byID := make(map[string]*container.Telemetry, len(identities))
	for _, identity := range identities {
		id := identity.ID
		if id == "" {
			id = identity.FullID
		}
		if id == "" {
			continue
		}
		byID[id] = &container.Telemetry{ID: id, Name: identity.Name}
	}

	processContainer := make(map[int32]string, len(processes))
	for _, process := range processes {
		if process.ContainerID == "" {
			continue
		}
		item := byID[process.ContainerID]
		if item == nil {
			item = &container.Telemetry{ID: process.ContainerID, Name: process.ContainerName}
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
	for _, sample := range uniqueGPUProcessSamples(gpuSamples) {
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
		gpu := devices[deviceID]
		if gpu == nil {
			gpu = &containerGPUAccumulator{
				value:          container.GPUUsage{Name: sample.DeviceName, Source: sample.Source},
				memoryComplete: true,
			}
			devices[deviceID] = gpu
		}
		gpu.value.ProcessCount++
		if sample.MemoryBytes != nil {
			gpu.memory += *sample.MemoryBytes
			gpu.memoryKnown = true
		} else {
			gpu.memoryComplete = false
		}
		if sample.GPUUtilization != nil {
			gpu.usage += *sample.GPUUtilization
			gpu.usageKnown = true
		}
		if sample.EncodeUtilization != nil {
			gpu.encode += *sample.EncodeUtilization
			gpu.encodeKnown = true
		}
		if sample.DecodeUtilization != nil {
			gpu.decode += *sample.DecodeUtilization
			gpu.decodeKnown = true
		}
		gpu.value.Source = mergeGPUProcessSource(gpu.value.Source, sample.Source)
	}

	result := make([]container.Telemetry, 0, len(byID))
	for id, item := range byID {
		item.CPUPercent = utils.TwoDecimals(item.CPUPercent)
		if devices := gpuAccumulators[id]; len(devices) > 0 {
			item.GPUs = make(map[string]container.GPUUsage, len(devices))
			for deviceID, acc := range devices {
				if acc.memoryKnown && acc.memoryComplete {
					memory := acc.memory
					acc.value.MemoryUsedBytes = &memory
				}
				if acc.usageKnown {
					usage := utils.TwoDecimals(acc.usage)
					acc.value.UsagePercent = &usage
				}
				if acc.encodeKnown {
					encode := utils.TwoDecimals(acc.encode)
					acc.value.EncodePercent = &encode
				}
				if acc.decodeKnown {
					decode := utils.TwoDecimals(acc.decode)
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

type gpuProcessIdentity struct {
	deviceID string
	pid      int32
}

func uniqueGPUProcessSamples(samples []GPUProcessSample) []GPUProcessSample {
	unique := make(map[gpuProcessIdentity]GPUProcessSample, len(samples))
	for _, sample := range samples {
		key := gpuProcessIdentity{deviceID: sample.DeviceID, pid: sample.PID}
		if key.deviceID == "" {
			key.deviceID = sample.DeviceName
		}
		if key.deviceID == "" || key.pid <= 0 {
			continue
		}
		current, exists := unique[key]
		if !exists {
			unique[key] = sample
			continue
		}
		if gpuProcessSourceRank(sample.Source) > gpuProcessSourceRank(current.Source) {
			sample = fillMissingGPUProcessMetrics(sample, current)
			unique[key] = sample
		} else {
			unique[key] = fillMissingGPUProcessMetrics(current, sample)
		}
	}
	result := make([]GPUProcessSample, 0, len(unique))
	for _, sample := range unique {
		result = append(result, sample)
	}
	return result
}

func fillMissingGPUProcessMetrics(preferred, fallback GPUProcessSample) GPUProcessSample {
	usedFallback := false
	if preferred.DeviceName == "" {
		preferred.DeviceName = fallback.DeviceName
		usedFallback = fallback.DeviceName != ""
	}
	if preferred.GPUUtilization == nil {
		preferred.GPUUtilization = fallback.GPUUtilization
		usedFallback = usedFallback || fallback.GPUUtilization != nil
	}
	if preferred.EncodeUtilization == nil {
		preferred.EncodeUtilization = fallback.EncodeUtilization
		usedFallback = usedFallback || fallback.EncodeUtilization != nil
	}
	if preferred.DecodeUtilization == nil {
		preferred.DecodeUtilization = fallback.DecodeUtilization
		usedFallback = usedFallback || fallback.DecodeUtilization != nil
	}
	if preferred.MemoryBytes == nil {
		preferred.MemoryBytes = fallback.MemoryBytes
		usedFallback = usedFallback || fallback.MemoryBytes != nil
	}
	if usedFallback {
		preferred.Source = mergeGPUProcessSource(preferred.Source, fallback.Source)
	}
	return preferred
}

func gpuProcessSourceRank(source string) int {
	switch source {
	case "mixed":
		return 3
	case nvtopCmd:
		return 2
	case string(collectorSourceNVML):
		return 1
	default:
		return 0
	}
}

func mergeGPUProcessSource(current, next string) string {
	if current == "" {
		return next
	}
	if next == "" || current == next {
		return current
	}
	return "mixed"
}
