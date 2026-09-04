package app

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"os"
	"runtime"
	"strings"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/load"
	"github.com/shirou/gopsutil/v4/mem"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/integration/docker/dockerapi"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/utils"
)

// systemInfoManager owns dynamic host info, mostly-static host details, the dirty
// flag for incremental detail propagation, and the ZFS capability bit detected at
// startup.
type systemInfoManager struct {
	systemInfo    system.Info    // Host system info (dynamic)
	systemDetails system.Details // Host system details (static, once-per-connection)
	detailsDirty  bool           // Whether system details have changed and need to be resent
	zfs           bool           // true if system has arcstats
}

type containerRuntimeInfo interface {
	IsPodman() bool
	GetHostInfo(context.Context) (dockerapi.HostInfo, error)
}

func newSystemInfoManager() *systemInfoManager {
	return &systemInfoManager{}
}

// Sets initial / non-changing values about the host system
func (m *systemInfoManager) refreshSystemDetails(ctx context.Context, dockerManager containerRuntimeInfo) {
	m.systemInfo.AgentVersion = version.Version

	// get host info from Docker if available
	var hostInfo dockerapi.HostInfo

	if dockerManager != nil {
		m.systemDetails.Podman = dockerManager.IsPodman()
		hostInfo, _ = dockerManager.GetHostInfo(ctx)
	}

	m.systemDetails.Hostname, _ = os.Hostname()
	if arch, err := host.KernelArch(); err == nil {
		m.systemDetails.Arch = arch
	} else {
		m.systemDetails.Arch = runtime.GOARCH
	}

	platform, _, _, _ := host.PlatformInformation()
	m.systemDetails.OsName = hostInfo.OperatingSystem
	if m.systemDetails.OsName == "" {
		if prettyName, err := getOsPrettyName(); err == nil {
			m.systemDetails.OsName = prettyName
		} else {
			m.systemDetails.OsName = platform
		}
	}
	m.systemDetails.Kernel = hostInfo.KernelVersion
	if m.systemDetails.Kernel == "" {
		m.systemDetails.Kernel, _ = host.KernelVersion()
	}

	// cpu model
	if info, err := cpu.Info(); err == nil && len(info) > 0 {
		m.systemDetails.CpuModel = info[0].ModelName
	}
	// cores / threads
	cores, _ := cpu.Counts(false)
	threads := hostInfo.NCPU
	if threads == 0 {
		threads, _ = cpu.Counts(true)
	}
	// in lxc, logical cores reflects container limits, so use that as cores if lower
	if threads > 0 && threads < cores {
		cores = threads
	}
	m.systemDetails.Cores = cores
	m.systemDetails.Threads = threads
	slog.Info("Detected CPU", "model", m.systemDetails.CpuModel, "cores", cores, "threads", threads, "arch", m.systemDetails.Arch)

	// total memory
	m.systemDetails.MemoryTotal = hostInfo.MemTotal
	if m.systemDetails.MemoryTotal == 0 {
		if v, err := mem.VirtualMemory(); err == nil {
			m.systemDetails.MemoryTotal = v.Total
		}
	}

	// zfs
	if _, err := ARCSize(); err != nil {
		slog.Debug("Capability check", "capability", "zfs_arc", "available", false, "err", err)
	} else {
		m.zfs = true
		slog.Debug("Capability check", "capability", "zfs_arc", "available", true)
	}
}

// attachSystemDetails returns details only for fresh default-interval responses.
func (m *systemInfoManager) attachSystemDetails(data *system.CombinedData, cacheTimeMs uint16, includeRequested bool) *system.CombinedData {
	if cacheTimeMs != collectorDataKeyMs || (!includeRequested && !m.detailsDirty) {
		return data
	}

	// copy data to avoid adding details to the original cached struct
	response := *data
	response.Details = &m.systemDetails
	m.detailsDirty = false
	return &response
}

// updateSystemDetails applies a mutation to the static details payload and marks
// it for inclusion on the next fresh default-interval response.
func (m *systemInfoManager) updateSystemDetails(updateFunc func(details *system.Details)) {
	updateFunc(&m.systemDetails)
	m.detailsDirty = true
}

// Returns current info, stats about the host system.
func (a *App) getSystemStats(ctx context.Context, cacheTimeMs uint16) (system.Stats, error) {
	var systemStats system.Stats
	if err := a.updateCoreSystemStats(ctx, cacheTimeMs, &systemStats); err != nil {
		return systemStats, err
	}
	if err := a.fsManager.updateDiskUsage(ctx, &systemStats); err != nil {
		return systemStats, err
	}
	if err := a.fsManager.updateDiskIo(ctx, cacheTimeMs, &systemStats); err != nil {
		return systemStats, err
	}
	if err := a.fsManager.updateBlockDeviceRates(ctx, cacheTimeMs, &systemStats); err != nil {
		return systemStats, err
	}
	if err := a.networkManager.updateNetworkStats(ctx, cacheTimeMs, &systemStats); err != nil {
		return systemStats, err
	}
	if err := a.updateTemperatures(ctx, &systemStats); err != nil {
		return systemStats, err
	}
	a.updateGPUStats(cacheTimeMs, &systemStats)
	if err := a.updateSystemInfoStats(ctx, &systemStats); err != nil {
		return systemStats, err
	}
	return systemStats, nil
}

func (a *App) updateCoreSystemStats(ctx context.Context, cacheTimeMs uint16, systemStats *system.Stats) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	updateBatteryStats(systemStats)
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := updateCPUMetrics(ctx, cacheTimeMs, systemStats); err != nil {
		return err
	}
	if err := updateLoadStats(ctx, systemStats); err != nil {
		return err
	}
	return a.updateMemoryStats(ctx, systemStats)
}

func updateBatteryStats(systemStats *system.Stats) {
	if batteryPercent, batteryState, err := GetBatteryStats(); err == nil {
		systemStats.Battery[0] = batteryPercent
		systemStats.Battery[1] = batteryState
	}
}

func updateCPUMetrics(ctx context.Context, cacheTimeMs uint16, systemStats *system.Stats) error {
	cpuMetrics, err := getCpuMetrics(ctx, cacheTimeMs)
	if err == nil {
		systemStats.Cpu = utils.TwoDecimals(cpuMetrics.Total)
		systemStats.CpuBreakdown = []float64{
			utils.TwoDecimals(cpuMetrics.User),
			utils.TwoDecimals(cpuMetrics.System),
			utils.TwoDecimals(cpuMetrics.Iowait),
			utils.TwoDecimals(cpuMetrics.Steal),
			utils.TwoDecimals(cpuMetrics.Idle),
		}
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	} else {
		slog.Error("Error getting cpu metrics", "err", err)
	}

	if perCoreUsage, err := getPerCoreCpuUsage(ctx, cacheTimeMs); err == nil {
		systemStats.CpuCoresUsage = perCoreUsage
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	return nil
}

func updateLoadStats(ctx context.Context, systemStats *system.Stats) error {
	avgstat, err := load.AvgWithContext(ctx)
	if err == nil {
		systemStats.LoadAvg[0] = avgstat.Load1
		systemStats.LoadAvg[1] = avgstat.Load5
		systemStats.LoadAvg[2] = avgstat.Load15
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	slog.Error("Error getting load average", "err", err)
	return nil
}

func (a *App) updateMemoryStats(ctx context.Context, systemStats *system.Stats) error {
	v, err := mem.VirtualMemoryWithContext(ctx)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		return nil
	}

	systemStats.MemoryBytes = system.MemoryBytes{
		Total: v.Total, Available: v.Available, Free: v.Free, Cached: v.Cached,
		Buffers: v.Buffers, Shared: v.Shared, SwapTotal: v.SwapTotal, SwapFree: v.SwapFree,
	}

	swapUsed := usedSwapBytes(v.SwapTotal, v.SwapFree, v.SwapCached)
	systemStats.Swap = utils.BytesToGigabytes(v.SwapTotal)
	systemStats.SwapUsed = utils.BytesToGigabytes(swapUsed)
	if v.SwapTotal > 0 {
		systemStats.SwapPct = utils.TwoDecimals(float64(swapUsed) / float64(v.SwapTotal) * 100)
	}
	systemStats.MemAvailable = utils.BytesToGigabytes(v.Available)
	systemStats.MemCached = utils.BytesToGigabytes(v.Cached)
	systemStats.MemBuffers = utils.BytesToGigabytes(v.Buffers)
	cacheBuff := v.Cached + v.Buffers - v.Shared
	if cacheBuff <= 0 {
		cacheBuff = max(v.Total-v.Free-v.Used, 0)
	}
	if a.memCalc == "htop" {
		v.Used = v.Total - (v.Free + cacheBuff)
		v.UsedPercent = float64(v.Used) / float64(v.Total) * 100.0
	}
	var arcBytes uint64
	if a.systemInfoManager.zfs {
		arcBytes, _ = ARCSize()
		if arcBytes > 0 && arcBytes < v.Used {
			v.Used -= arcBytes
			v.UsedPercent = float64(v.Used) / float64(v.Total) * 100.0
			systemStats.MemZfsArc = utils.BytesToGigabytes(arcBytes)
		}
	}
	systemStats.Mem = utils.BytesToGigabytes(v.Total)
	systemStats.MemBuffCache = utils.BytesToGigabytes(cacheBuff)
	systemStats.MemUsed = utils.BytesToGigabytes(v.Used)
	systemStats.MemPct = utils.TwoDecimals(v.UsedPercent)
	systemStats.MemoryBytes.Used = v.Used
	systemStats.MemoryBytes.ZFSArc = arcBytes
	return nil
}

func usedSwapBytes(total, free, cached uint64) uint64 {
	if free >= total {
		return 0
	}
	used := total - free
	if cached >= used {
		return 0
	}
	return used - cached
}

func (a *App) updateGPUStats(cacheTimeMs uint16, systemStats *system.Stats) {
	info := &a.systemInfoManager.systemInfo
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
			if a.sensorConfig.primarySensor == gpu.Name {
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

func (a *App) updateSystemInfoStats(ctx context.Context, systemStats *system.Stats) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info := &a.systemInfoManager.systemInfo
	info.Cpu = systemStats.Cpu
	info.LoadAvg = systemStats.LoadAvg
	info.MemPct = systemStats.MemPct
	info.DiskPct = systemStats.DiskPct
	info.Battery = systemStats.Battery
	if uptime, err := host.UptimeWithContext(ctx); err == nil {
		info.Uptime = uptime
	} else if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	info.BandwidthBytes = systemStats.Bandwidth[0] + systemStats.Bandwidth[1]
	info.Threads = a.systemInfoManager.systemDetails.Threads
	return nil
}

// getOsPrettyName attempts to get the pretty OS name from /etc/os-release on Linux systems
func getOsPrettyName() (string, error) {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "PRETTY_NAME="); ok {
			value := after
			value = strings.Trim(value, `"`)
			return value, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", errors.New("pretty name not found")
}
