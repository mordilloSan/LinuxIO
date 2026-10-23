// Package host collects host-level readings: CPU usage and frequencies, memory
// and load, battery, the ZFS ARC, and the host details reported once per
// connection.
package host

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"runtime"

	mobysystem "github.com/moby/moby/api/types/system"
	psutilCPU "github.com/shirou/gopsutil/v4/cpu"
	psutilHost "github.com/shirou/gopsutil/v4/host"
	psutilMem "github.com/shirou/gopsutil/v4/mem"

	"github.com/mordilloSan/LinuxIO/backend/common/osrelease"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/collector"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

// Manager owns dynamic host info, mostly-static host details, the dirty flag
// for incremental detail propagation, and the ZFS capability bit detected at
// startup. Callers serialise access to Info and Details.
type Manager struct {
	Info         systemmodel.Info    // Host system info (dynamic)
	Details      systemmodel.Details // Host system details (static, once-per-connection)
	detailsDirty bool                // Whether system details have changed and need to be resent
	zfs          bool                // true if system has arcstats
	memCalc      string              // MEM_CALC: memory-used formula
}

// ContainerRuntime is the part of the Docker or Podman manager that host
// details draw on.
type ContainerRuntime interface {
	IsPodman() bool
	GetHostInfo(context.Context) (mobysystem.Info, error)
}

// NewManager returns a Manager configured from the environment.
func NewManager() *Manager {
	memCalc, _ := config.LookupEnv(config.EnvMemCalc)
	return &Manager{memCalc: memCalc}
}

// Refresh sets the initial, non-changing values about the host system.
func (m *Manager) Refresh(ctx context.Context, dockerManager ContainerRuntime) {
	m.Info.AgentVersion = version.Version

	// get host info from Docker if available
	var hostInfo mobysystem.Info

	if dockerManager != nil {
		m.Details.Podman = dockerManager.IsPodman()
		hostInfo, _ = dockerManager.GetHostInfo(ctx)
	}

	m.Details.Hostname, _ = os.Hostname()
	if arch, err := psutilHost.KernelArch(); err == nil {
		m.Details.Arch = arch
	} else {
		m.Details.Arch = runtime.GOARCH
	}

	platform, _, _, _ := psutilHost.PlatformInformation()
	m.Details.OsName = hostInfo.OperatingSystem
	if m.Details.OsName == "" {
		if prettyName, err := getOsPrettyName(); err == nil {
			m.Details.OsName = prettyName
		} else {
			m.Details.OsName = platform
		}
	}
	m.Details.Kernel = hostInfo.KernelVersion
	if m.Details.Kernel == "" {
		m.Details.Kernel, _ = psutilHost.KernelVersion()
	}

	// cpu model
	if info, err := psutilCPU.Info(); err == nil && len(info) > 0 {
		m.Details.CpuModel = info[0].ModelName
	}
	// cores / threads
	cores, _ := psutilCPU.Counts(false)
	threads := hostInfo.NCPU
	if threads == 0 {
		threads, _ = psutilCPU.Counts(true)
	}
	// in lxc, logical cores reflects container limits, so use that as cores if lower
	if threads > 0 && threads < cores {
		cores = threads
	}
	m.Details.Cores = cores
	m.Details.Threads = threads
	slog.Info("Detected CPU", "model", m.Details.CpuModel, "cores", cores, "threads", threads, "arch", m.Details.Arch)

	// total memory
	m.Details.MemoryTotal = 0
	if hostInfo.MemTotal > 0 {
		m.Details.MemoryTotal = uint64(hostInfo.MemTotal)
	}
	if m.Details.MemoryTotal == 0 {
		if v, err := psutilMem.VirtualMemory(); err == nil {
			m.Details.MemoryTotal = v.Total
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

// AttachDetails returns details only for fresh collector-key responses.
func (m *Manager) AttachDetails(data *systemmodel.CombinedData, cacheTimeMs uint16, includeRequested bool) *systemmodel.CombinedData {
	if cacheTimeMs != collector.TickKey || (!includeRequested && !m.detailsDirty) {
		return data
	}

	// copy data to avoid adding details to the original cached struct
	response := *data
	response.Details = &m.Details
	m.detailsDirty = false
	return &response
}

// UpdateDetails applies a mutation to the static details payload and marks it
// for inclusion on the next fresh collector-key response.
func (m *Manager) UpdateDetails(updateFunc func(details *systemmodel.Details)) {
	updateFunc(&m.Details)
	m.detailsDirty = true
}

// getOsPrettyName attempts to get the pretty OS name from /etc/os-release on Linux systems
func getOsPrettyName() (string, error) {
	info, err := osrelease.Read()
	if err != nil {
		return "", err
	}
	if name := info["PRETTY_NAME"]; name != "" {
		return name, nil
	}
	return "", errors.New("pretty name not found")
}
