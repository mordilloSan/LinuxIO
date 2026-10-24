package plugins

import (
	"encoding/json/v2"
	"errors"
	"math"
	"sort"

	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/units"
)

// Average merges stored JSON payloads of one plugin into the payload of a
// coarser history bucket.
//
//nolint:gocognit // Complexity comes from the number of plugin cases, not logical nesting.
func Average(plugin string, items []string) (string, error) {
	if len(items) == 0 {
		return "", errors.New("no history items to aggregate")
	}

	switch plugin {
	case CPU:
		return averagePluginViaSystemStats(items, func(raw []byte, stats *systemmodel.Stats) error {
			var item CPUData
			if err := json.Unmarshal(raw, &item); err != nil {
				return err
			}
			stats.Cpu = item.Cpu
			stats.MaxCpu = item.MaxCpu
			stats.CpuBreakdown = item.CpuBreakdown
			stats.CpuCoresUsage = item.CpuCoresUsage
			stats.CpuFrequencies = item.CpuFrequencies
			return nil
		}, func(stats systemmodel.Stats) any {
			return Snapshot(&systemmodel.CombinedData{Stats: stats})[CPU]
		})
	case Mem:
		return averagePluginViaSystemStats(items, func(raw []byte, stats *systemmodel.Stats) error {
			var item MemData
			if err := json.Unmarshal(raw, &item); err != nil {
				return err
			}
			stats.Mem = item.Mem
			stats.MaxMem = item.MaxMem
			stats.MemUsed = item.MemUsed
			stats.MemPct = item.MemPct
			stats.MemBuffCache = item.MemBuffCache
			stats.MemZfsArc = item.MemZfsArc
			stats.Swap = item.MemSwapTotal
			stats.SwapUsed = item.MemSwapUsed
			stats.SwapPct = item.MemSwapPct
			stats.MemAvailable = item.MemAvailable
			stats.MemCached = item.MemCached
			stats.MemBuffers = item.MemBuffers
			return nil
		}, func(stats systemmodel.Stats) any {
			return Snapshot(&systemmodel.CombinedData{Stats: stats})[Mem]
		})
	case Swap:
		return averagePluginViaSystemStats(items, func(raw []byte, stats *systemmodel.Stats) error {
			var item SwapData
			if err := json.Unmarshal(raw, &item); err != nil {
				return err
			}
			stats.Swap = item.Swap
			stats.SwapUsed = item.SwapUsed
			return nil
		}, func(stats systemmodel.Stats) any {
			return Snapshot(&systemmodel.CombinedData{Stats: stats})[Swap]
		})
	case Load:
		return averagePluginViaSystemStats(items, func(raw []byte, stats *systemmodel.Stats) error {
			var item LoadData
			if err := json.Unmarshal(raw, &item); err != nil {
				return err
			}
			stats.LoadAvg = item.LoadAvg
			stats.Battery = item.Battery
			return nil
		}, func(stats systemmodel.Stats) any {
			return Snapshot(&systemmodel.CombinedData{Stats: stats})[Load]
		})
	case DiskIO:
		return averagePluginViaSystemStats(items, func(raw []byte, stats *systemmodel.Stats) error {
			var item DiskIOData
			if err := json.Unmarshal(raw, &item); err != nil {
				return err
			}
			stats.DiskTotal = item.DiskTotal
			stats.DiskUsed = item.DiskUsed
			stats.DiskPct = item.DiskPct
			stats.DiskReadPs = item.DiskReadPs
			stats.DiskWritePs = item.DiskWritePs
			stats.MaxDiskReadPs = item.MaxDiskReadPs
			stats.MaxDiskWritePs = item.MaxDiskWritePs
			stats.DiskIO = item.DiskIO
			stats.MaxDiskIO = item.MaxDiskIO
			stats.DiskIoStats = item.DiskIoStats
			stats.MaxDiskIoStats = item.MaxDiskIoStats
			return nil
		}, func(stats systemmodel.Stats) any {
			return Snapshot(&systemmodel.CombinedData{Stats: stats})[DiskIO]
		})
	case FS:
		return averagePluginViaSystemStats(items, func(raw []byte, stats *systemmodel.Stats) error {
			var item FSData
			if err := json.Unmarshal(raw, &item); err != nil {
				return err
			}
			stats.ExtraFs = item.ExtraFs
			return nil
		}, func(stats systemmodel.Stats) any {
			return Snapshot(&systemmodel.CombinedData{Stats: stats})[FS]
		})
	case Network:
		return averagePluginViaSystemStats(items, func(raw []byte, stats *systemmodel.Stats) error {
			var item NetworkData
			if err := json.Unmarshal(raw, &item); err != nil {
				return err
			}
			stats.Bandwidth = item.Bandwidth
			stats.MaxBandwidth = item.MaxBandwidth
			stats.NetworkInterfaces = item.NetworkInterfaces
			return nil
		}, func(stats systemmodel.Stats) any {
			return Snapshot(&systemmodel.CombinedData{Stats: stats})[Network]
		})
	case GPU:
		return averagePluginViaSystemStats(items, func(raw []byte, stats *systemmodel.Stats) error {
			var item map[string]systemmodel.GPUData
			if err := json.Unmarshal(raw, &item); err != nil {
				return err
			}
			stats.GPUData = item
			return nil
		}, func(stats systemmodel.Stats) any {
			return stats.GPUData
		})
	case Sensors:
		return averagePluginViaSystemStats(items, func(raw []byte, stats *systemmodel.Stats) error {
			var item SensorsData
			if err := json.Unmarshal(raw, &item); err != nil {
				return err
			}
			stats.Temperatures = item.Temperatures
			return nil
		}, func(stats systemmodel.Stats) any {
			return Snapshot(&systemmodel.CombinedData{Stats: stats})[Sensors]
		})
	case Containers:
		averaged, err := averageContainerStatsJSON(items)
		if err != nil {
			return "", err
		}
		return marshalJSON(averaged)
	case ContainerTelemetry:
		averaged, err := averageContainerTelemetryJSON(items)
		if err != nil {
			return "", err
		}
		return marshalJSON(averaged)
	default:
		return items[len(items)-1], nil
	}
}

func averagePluginViaSystemStats(
	items []string,
	fill func([]byte, *systemmodel.Stats) error,
	extract func(systemmodel.Stats) any,
) (string, error) {
	systemStatsJSON := make([]string, 0, len(items))
	for _, raw := range items {
		stats := systemmodel.Stats{}
		if err := fill([]byte(raw), &stats); err != nil {
			return "", err
		}
		statsRaw, err := marshalJSON(stats)
		if err != nil {
			return "", err
		}
		systemStatsJSON = append(systemStatsJSON, statsRaw)
	}
	averaged, err := averageSystemStatsJSON(systemStatsJSON)
	if err != nil {
		return "", err
	}
	return marshalJSON(extract(*averaged))
}

//nolint:gocognit // System stats aggregation spans optional nested metric instances and their distinct rules.
func averageSystemStatsJSON(items []string) (*systemmodel.Stats, error) {
	sum := &systemmodel.Stats{}
	temp := &systemmodel.Stats{}
	count := float64(len(items))
	batterySum := 0
	var cpuCoresSums []uint64
	var cpuBreakdownSums []float64
	var cpuFreqSums []float64

	for _, raw := range items {
		*temp = systemmodel.Stats{}
		if err := json.Unmarshal([]byte(raw), temp); err != nil {
			return nil, err
		}

		sum.Cpu += temp.Cpu
		sum.Mem += temp.Mem
		sum.MemUsed += temp.MemUsed
		sum.MemPct += temp.MemPct
		sum.MemBuffCache += temp.MemBuffCache
		sum.MemZfsArc += temp.MemZfsArc
		sum.Swap += temp.Swap
		sum.SwapUsed += temp.SwapUsed
		sum.SwapPct += temp.SwapPct
		sum.MemAvailable += temp.MemAvailable
		sum.MemCached += temp.MemCached
		sum.MemBuffers += temp.MemBuffers
		sum.DiskTotal += temp.DiskTotal
		sum.DiskUsed += temp.DiskUsed
		sum.DiskPct += temp.DiskPct
		sum.DiskReadPs += temp.DiskReadPs
		sum.DiskWritePs += temp.DiskWritePs
		for i := range temp.LoadAvg {
			sum.LoadAvg[i] += temp.LoadAvg[i]
		}
		for i := range temp.Bandwidth {
			sum.Bandwidth[i] += temp.Bandwidth[i]
			sum.DiskIO[i] += temp.DiskIO[i]
			sum.MaxBandwidth[i] = max(sum.MaxBandwidth[i], temp.MaxBandwidth[i], temp.Bandwidth[i])
			sum.MaxDiskIO[i] = max(sum.MaxDiskIO[i], temp.MaxDiskIO[i], temp.DiskIO[i])
		}
		for i := range temp.DiskIoStats {
			sum.DiskIoStats[i] += temp.DiskIoStats[i]
			sum.MaxDiskIoStats[i] = max(sum.MaxDiskIoStats[i], temp.MaxDiskIoStats[i], temp.DiskIoStats[i])
		}
		batterySum += int(temp.Battery[0])
		sum.Battery[1] = temp.Battery[1]

		if len(temp.CpuBreakdown) > len(cpuBreakdownSums) {
			cpuBreakdownSums = append(cpuBreakdownSums, make([]float64, len(temp.CpuBreakdown)-len(cpuBreakdownSums))...)
		}
		for i, value := range temp.CpuBreakdown {
			cpuBreakdownSums[i] += value
		}
		if len(temp.CpuCoresUsage) > len(cpuCoresSums) {
			cpuCoresSums = append(cpuCoresSums, make([]uint64, len(temp.CpuCoresUsage)-len(cpuCoresSums))...)
		}
		for i, value := range temp.CpuCoresUsage {
			cpuCoresSums[i] += uint64(value)
		}
		if len(temp.CpuFrequencies) > len(cpuFreqSums) {
			cpuFreqSums = append(cpuFreqSums, make([]float64, len(temp.CpuFrequencies)-len(cpuFreqSums))...)
		}
		for i, value := range temp.CpuFrequencies {
			cpuFreqSums[i] += value
		}

		sum.MaxCpu = max(sum.MaxCpu, temp.MaxCpu, temp.Cpu)
		sum.MaxMem = max(sum.MaxMem, temp.MaxMem, temp.MemUsed)
		sum.MaxDiskReadPs = max(sum.MaxDiskReadPs, temp.MaxDiskReadPs, temp.DiskReadPs)
		sum.MaxDiskWritePs = max(sum.MaxDiskWritePs, temp.MaxDiskWritePs, temp.DiskWritePs)
		accumulateNetworkInterfaces(sum, temp)
		accumulateTemperatures(sum, temp)
		accumulateFilesystems(sum, temp)
		accumulateGPUs(sum, temp)
	}

	if count == 0 {
		return sum, nil
	}
	averageScalarStats(sum, count)
	sum.Battery[0] = uint8(batterySum / int(count))
	averageNetworkInterfaces(sum, uint64(count))
	averageTemperatures(sum, count)
	averageFilesystems(sum, count)
	averageGPUs(sum, count)
	if len(cpuCoresSums) > 0 {
		sum.CpuCoresUsage = make(systemmodel.Uint8Slice, len(cpuCoresSums))
		for i, value := range cpuCoresSums {
			sum.CpuCoresUsage[i] = uint8(math.Round(float64(value) / count))
		}
	}
	if len(cpuBreakdownSums) > 0 {
		sum.CpuBreakdown = make([]float64, len(cpuBreakdownSums))
		for i, value := range cpuBreakdownSums {
			sum.CpuBreakdown[i] = units.TwoDecimals(value / count)
		}
	}
	if len(cpuFreqSums) > 0 {
		sum.CpuFrequencies = make([]float64, len(cpuFreqSums))
		for i, value := range cpuFreqSums {
			sum.CpuFrequencies[i] = units.TwoDecimals(value / count)
		}
	}
	return sum, nil
}

func averageScalarStats(sum *systemmodel.Stats, count float64) {
	sum.Cpu = units.TwoDecimals(sum.Cpu / count)
	sum.Mem = units.TwoDecimals(sum.Mem / count)
	sum.MemUsed = units.TwoDecimals(sum.MemUsed / count)
	sum.MemPct = units.TwoDecimals(sum.MemPct / count)
	sum.MemBuffCache = units.TwoDecimals(sum.MemBuffCache / count)
	sum.MemZfsArc = units.TwoDecimals(sum.MemZfsArc / count)
	sum.Swap = units.TwoDecimals(sum.Swap / count)
	sum.SwapUsed = units.TwoDecimals(sum.SwapUsed / count)
	sum.SwapPct = units.TwoDecimals(sum.SwapPct / count)
	sum.MemAvailable = units.TwoDecimals(sum.MemAvailable / count)
	sum.MemCached = units.TwoDecimals(sum.MemCached / count)
	sum.MemBuffers = units.TwoDecimals(sum.MemBuffers / count)
	sum.DiskTotal = units.TwoDecimals(sum.DiskTotal / count)
	sum.DiskUsed = units.TwoDecimals(sum.DiskUsed / count)
	sum.DiskPct = units.TwoDecimals(sum.DiskPct / count)
	sum.DiskReadPs = units.TwoDecimals(sum.DiskReadPs / count)
	sum.DiskWritePs = units.TwoDecimals(sum.DiskWritePs / count)
	for i := range sum.LoadAvg {
		sum.LoadAvg[i] = units.TwoDecimals(sum.LoadAvg[i] / count)
	}
	for i := range sum.Bandwidth {
		sum.Bandwidth[i] /= uint64(count)
		sum.DiskIO[i] /= uint64(count)
	}
	for i := range sum.DiskIoStats {
		sum.DiskIoStats[i] = units.TwoDecimals(sum.DiskIoStats[i] / count)
	}
}

func accumulateNetworkInterfaces(sum, sample *systemmodel.Stats) {
	if sample.NetworkInterfaces == nil {
		return
	}
	if sum.NetworkInterfaces == nil {
		sum.NetworkInterfaces = make(map[string][4]uint64, len(sample.NetworkInterfaces))
	}
	for key, value := range sample.NetworkInterfaces {
		current := sum.NetworkInterfaces[key]
		sum.NetworkInterfaces[key] = [4]uint64{
			current[0] + value[0],
			current[1] + value[1],
			max(current[2], value[2]),
			max(current[3], value[3]),
		}
	}
}

func averageNetworkInterfaces(sum *systemmodel.Stats, count uint64) {
	for key, value := range sum.NetworkInterfaces {
		sum.NetworkInterfaces[key] = [4]uint64{value[0] / count, value[1] / count, value[2], value[3]}
	}
}

func accumulateTemperatures(sum, sample *systemmodel.Stats) {
	if sample.Temperatures == nil {
		return
	}
	if sum.Temperatures == nil {
		sum.Temperatures = make(map[string]float64, len(sample.Temperatures))
	}
	for key, value := range sample.Temperatures {
		sum.Temperatures[key] += value
	}
}

func averageTemperatures(sum *systemmodel.Stats, count float64) {
	for key, value := range sum.Temperatures {
		sum.Temperatures[key] = units.TwoDecimals(value / count)
	}
}

func accumulateFilesystems(sum, sample *systemmodel.Stats) {
	if sample.ExtraFs == nil {
		return
	}
	if sum.ExtraFs == nil {
		sum.ExtraFs = make(map[string]*systemmodel.FsStats, len(sample.ExtraFs))
	}
	for key, value := range sample.ExtraFs {
		if value == nil {
			continue
		}
		if sum.ExtraFs[key] == nil {
			sum.ExtraFs[key] = &systemmodel.FsStats{}
		}
		fs := sum.ExtraFs[key]
		fs.DiskTotal += value.DiskTotal
		fs.DiskUsed += value.DiskUsed
		fs.DiskWritePs += value.DiskWritePs
		fs.DiskReadPs += value.DiskReadPs
		fs.MaxDiskReadPS = max(fs.MaxDiskReadPS, value.MaxDiskReadPS, value.DiskReadPs)
		fs.MaxDiskWritePS = max(fs.MaxDiskWritePS, value.MaxDiskWritePS, value.DiskWritePs)
		fs.DiskReadBytes += value.DiskReadBytes
		fs.DiskWriteBytes += value.DiskWriteBytes
		fs.MaxDiskReadBytes = max(fs.MaxDiskReadBytes, value.MaxDiskReadBytes, value.DiskReadBytes)
		fs.MaxDiskWriteBytes = max(fs.MaxDiskWriteBytes, value.MaxDiskWriteBytes, value.DiskWriteBytes)
		for i := range value.DiskIoStats {
			fs.DiskIoStats[i] += value.DiskIoStats[i]
			fs.MaxDiskIoStats[i] = max(fs.MaxDiskIoStats[i], value.MaxDiskIoStats[i], value.DiskIoStats[i])
		}
	}
}

func averageFilesystems(sum *systemmodel.Stats, count float64) {
	for _, fs := range sum.ExtraFs {
		fs.DiskTotal = units.TwoDecimals(fs.DiskTotal / count)
		fs.DiskUsed = units.TwoDecimals(fs.DiskUsed / count)
		fs.DiskWritePs = units.TwoDecimals(fs.DiskWritePs / count)
		fs.DiskReadPs = units.TwoDecimals(fs.DiskReadPs / count)
		fs.DiskReadBytes /= uint64(count)
		fs.DiskWriteBytes /= uint64(count)
		for i := range fs.DiskIoStats {
			fs.DiskIoStats[i] = units.TwoDecimals(fs.DiskIoStats[i] / count)
		}
	}
}

func accumulateGPUs(sum, sample *systemmodel.Stats) {
	if sample.GPUData == nil {
		return
	}
	if sum.GPUData == nil {
		sum.GPUData = make(map[string]systemmodel.GPUData, len(sample.GPUData))
	}
	for id, value := range sample.GPUData {
		gpu := sum.GPUData[id]
		if gpu.Name == "" {
			gpu.Name = value.Name
		}
		gpu.Temperature += value.Temperature
		gpu.MemoryUsed += value.MemoryUsed
		gpu.MemoryTotal += value.MemoryTotal
		gpu.Usage += value.Usage
		gpu.Power += value.Power
		gpu.PowerPkg += value.PowerPkg
		gpu.Count += value.Count
		if value.Engines != nil {
			if gpu.Engines == nil {
				gpu.Engines = make(map[string]float64, len(value.Engines))
			}
			for key, engine := range value.Engines {
				gpu.Engines[key] += engine
			}
		}
		sum.GPUData[id] = gpu
	}
}

func averageGPUs(sum *systemmodel.Stats, count float64) {
	for id, gpu := range sum.GPUData {
		gpu.Temperature = units.TwoDecimals(gpu.Temperature / count)
		gpu.MemoryUsed = units.TwoDecimals(gpu.MemoryUsed / count)
		gpu.MemoryTotal = units.TwoDecimals(gpu.MemoryTotal / count)
		gpu.Usage = units.TwoDecimals(gpu.Usage / count)
		gpu.Power = units.TwoDecimals(gpu.Power / count)
		gpu.PowerPkg = units.TwoDecimals(gpu.PowerPkg / count)
		gpu.Count = units.TwoDecimals(gpu.Count / count)
		for key := range gpu.Engines {
			gpu.Engines[key] = units.TwoDecimals(gpu.Engines[key] / count)
		}
		sum.GPUData[id] = gpu
	}
}

type containerStatsAccumulator struct {
	value     containerSnapshotRecord
	samples   uint64
	cpu       float64
	memory    float64
	bandwidth [2]uint64
}

func averageContainerStatsJSON(items []string) ([]containerSnapshotRecord, error) {
	sums := make(map[string]*containerStatsAccumulator)
	for _, raw := range items {
		var records []containerSnapshotRecord
		if err := json.Unmarshal([]byte(raw), &records); err != nil {
			return nil, err
		}
		for _, record := range records {
			key := record.ID
			if key == "" {
				key = record.Name
			}
			if key == "" {
				continue
			}
			acc := sums[key]
			if acc == nil {
				acc = &containerStatsAccumulator{}
				sums[key] = acc
			}
			acc.value = record
			acc.samples++
			acc.cpu += record.Cpu
			acc.memory += record.Mem
			acc.bandwidth[0] += record.Bandwidth[0]
			acc.bandwidth[1] += record.Bandwidth[1]
		}
	}
	result := make([]containerSnapshotRecord, 0, len(sums))
	for _, acc := range sums {
		value := acc.value
		value.Cpu = units.TwoDecimals(acc.cpu / float64(acc.samples))
		value.Mem = units.TwoDecimals(acc.memory / float64(acc.samples))
		value.Bandwidth = [2]uint64{acc.bandwidth[0] / acc.samples, acc.bandwidth[1] / acc.samples}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID != result[j].ID {
			return result[i].ID < result[j].ID
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

type containerTelemetryAccumulator struct {
	value                   containermodel.Telemetry
	samples                 uint64
	processCountSum         uint64
	diskReadBytesPerSecond  uint64
	diskWriteBytesPerSecond uint64
	gpus                    map[string]*containerGPUAccumulator
}

type containerGPUAccumulator struct {
	value                  containermodel.GPUUsage
	samples                uint64
	processCountSum        uint64
	memoryUsedBytesSum     uint64
	memoryUsedBytesSamples uint64
	usageSum               float64
	usageSamples           uint64
	encodeSum              float64
	encodeSamples          uint64
	decodeSum              float64
	decodeSamples          uint64
}

// averageContainerTelemetryJSON aggregates only bounded container data. An
// entity absent from one source sample is absent, rather than a zero reading.
//
//nolint:gocognit // Each optional GPU aggregate needs independent presence tracking.
func averageContainerTelemetryJSON(items []string) ([]containermodel.Telemetry, error) {
	sums := make(map[string]*containerTelemetryAccumulator)
	for _, raw := range items {
		var records []containermodel.Telemetry
		if err := json.Unmarshal([]byte(raw), &records); err != nil {
			return nil, err
		}
		for _, record := range records {
			key := record.ID
			if key == "" {
				key = record.Name
			}
			if key == "" {
				continue
			}
			acc := sums[key]
			if acc == nil {
				acc = &containerTelemetryAccumulator{
					value: containermodel.Telemetry{ID: record.ID, Name: record.Name},
					gpus:  make(map[string]*containerGPUAccumulator),
				}
				sums[key] = acc
			}
			acc.samples++
			acc.processCountSum += uint64(max(record.ProcessCount, 0))
			acc.value.CPUPercent += record.CPUPercent
			acc.diskReadBytesPerSecond += record.DiskReadBytesPerSecond
			acc.diskWriteBytesPerSecond += record.DiskWriteBytesPerSecond
			if record.Name != "" {
				acc.value.Name = record.Name
			}

			for deviceID, gpu := range record.GPUs {
				gpuAcc := acc.gpus[deviceID]
				if gpuAcc == nil {
					gpuAcc = &containerGPUAccumulator{value: containermodel.GPUUsage{Name: gpu.Name, Source: gpu.Source}}
					acc.gpus[deviceID] = gpuAcc
				}
				gpuAcc.samples++
				gpuAcc.processCountSum += uint64(max(gpu.ProcessCount, 0))
				accumulateOptionalUint64(gpu.MemoryUsedBytes, &gpuAcc.memoryUsedBytesSum, &gpuAcc.memoryUsedBytesSamples)
				if gpu.Name != "" {
					gpuAcc.value.Name = gpu.Name
				}
				gpuAcc.value.Source = mergeTelemetrySource(gpuAcc.value.Source, gpu.Source)
				accumulateOptionalMetric(gpu.UsagePercent, &gpuAcc.usageSum, &gpuAcc.usageSamples)
				accumulateOptionalMetric(gpu.EncodePercent, &gpuAcc.encodeSum, &gpuAcc.encodeSamples)
				accumulateOptionalMetric(gpu.DecodePercent, &gpuAcc.decodeSum, &gpuAcc.decodeSamples)
			}
		}
	}

	result := make([]containermodel.Telemetry, 0, len(sums))
	for _, acc := range sums {
		if acc.samples == 0 {
			continue
		}
		value := acc.value
		value.ProcessCount = int(math.Round(float64(acc.processCountSum) / float64(acc.samples)))
		value.CPUPercent = units.TwoDecimals(value.CPUPercent / float64(acc.samples))
		value.DiskReadBytesPerSecond = acc.diskReadBytesPerSecond / acc.samples
		value.DiskWriteBytesPerSecond = acc.diskWriteBytesPerSecond / acc.samples
		if len(acc.gpus) > 0 {
			value.GPUs = make(map[string]containermodel.GPUUsage, len(acc.gpus))
			for deviceID, gpuAcc := range acc.gpus {
				gpu := gpuAcc.value
				gpu.ProcessCount = int(math.Round(float64(gpuAcc.processCountSum) / float64(gpuAcc.samples)))
				gpu.MemoryUsedBytes = averageOptionalUint64(gpuAcc.memoryUsedBytesSum, gpuAcc.memoryUsedBytesSamples)
				gpu.UsagePercent = averageOptionalMetric(gpuAcc.usageSum, gpuAcc.usageSamples)
				gpu.EncodePercent = averageOptionalMetric(gpuAcc.encodeSum, gpuAcc.encodeSamples)
				gpu.DecodePercent = averageOptionalMetric(gpuAcc.decodeSum, gpuAcc.decodeSamples)
				value.GPUs[deviceID] = gpu
			}
		}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].ID != result[j].ID {
			return result[i].ID < result[j].ID
		}
		return result[i].Name < result[j].Name
	})
	return result, nil
}

func accumulateOptionalMetric(value *float64, sum *float64, count *uint64) {
	if value == nil {
		return
	}
	*sum += *value
	(*count)++
}

func averageOptionalMetric(sum float64, count uint64) *float64 {
	if count == 0 {
		return nil
	}
	value := units.TwoDecimals(sum / float64(count))
	return &value
}

func accumulateOptionalUint64(value *uint64, sum, count *uint64) {
	if value == nil {
		return
	}
	*sum += *value
	(*count)++
}

func averageOptionalUint64(sum, count uint64) *uint64 {
	if count == 0 {
		return nil
	}
	value := sum / count
	return &value
}

func mergeTelemetrySource(current, next string) string {
	if current == "" {
		return next
	}
	if next == "" || current == next {
		return current
	}
	return "mixed"
}

func marshalJSON(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
