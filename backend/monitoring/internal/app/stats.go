package app

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector/host"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/units"
)

// getSystemStats samples every host collector under the given delta key.
func (a *App) getSystemStats(ctx context.Context, cacheTimeMs uint16) (systemmodel.Stats, error) {
	var systemStats systemmodel.Stats
	if err := a.host.UpdateCore(ctx, cacheTimeMs, &systemStats); err != nil {
		return systemStats, err
	}
	// Hosts without cpufreq simply have no frequencies.
	systemStats.CpuFrequencies, _ = host.CurrentFrequencies(ctx)
	if err := a.fsManager.UpdateUsage(ctx, &systemStats); err != nil {
		return systemStats, err
	}
	if err := a.fsManager.UpdateIO(ctx, cacheTimeMs, &systemStats); err != nil {
		return systemStats, err
	}
	if err := a.fsManager.UpdateDevices(ctx, cacheTimeMs, &systemStats); err != nil {
		return systemStats, err
	}
	if err := a.networkManager.UpdateStats(ctx, cacheTimeMs, &systemStats); err != nil {
		return systemStats, err
	}
	if err := a.sensorConfig.Update(ctx, &systemStats, &a.host.Info); err != nil {
		return systemStats, err
	}
	a.updateGPUStats(cacheTimeMs, &systemStats)
	if err := a.host.UpdateInfo(ctx, &systemStats); err != nil {
		return systemStats, err
	}
	return systemStats, nil
}

func (a *App) updateGPUStats(cacheTimeMs uint16, systemStats *systemmodel.Stats) {
	info := &a.host.Info
	if a.gpuManager == nil {
		return
	}
	info.GpuPct = 0
	gpuData := a.gpuManager.GetCurrentData(cacheTimeMs)
	if len(gpuData) == 0 {
		return
	}
	systemStats.GPUData = gpuData
	if systemStats.Temperatures == nil {
		systemStats.Temperatures = make(map[string]float64, len(gpuData))
	}
	highestTemp := 0.0
	for _, gpu := range gpuData {
		if gpu.Temperature > 0 {
			systemStats.Temperatures[gpu.Name] = gpu.Temperature
			if a.sensorConfig.PrimarySensor() == gpu.Name {
				info.DashboardTemp = gpu.Temperature
			}
			highestTemp = max(highestTemp, gpu.Temperature)
		}
		info.GpuPct = max(info.GpuPct, gpu.Usage)
	}
	if info.DashboardTemp == 0 {
		info.DashboardTemp = highestTemp
	}
}

// attachFilesystemStats publishes the tracked non-root filesystems.
func (a *App) attachFilesystemStats(data *systemmodel.CombinedData) {
	data.Stats.ExtraFs = make(map[string]*systemmodel.FsStats)
	data.Info.ExtraFsPct = make(map[string]float64)
	for name, stats := range a.fsManager.Tracked() {
		if !stats.Root && stats.DiskTotal > 0 {
			key := name
			if stats.Name != "" {
				key = stats.Name
			}
			data.Stats.ExtraFs[key] = stats
			data.Info.ExtraFsPct[key] = units.TwoDecimals((stats.DiskUsed / stats.DiskTotal) * 100)
		}
	}
}
