// Package plugins is the catalogue of metric plugins: their names, which of
// them keep history, how a collected sample projects onto each plugin's JSON
// payload, and how stored payloads average into coarser history buckets.
package plugins

import (
	"slices"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
)

// The plugins the bridge reads history for take their names from the shared
// wire contract in monitoring/api.
const (
	CPU                = monitoringapi.PluginCPU
	Mem                = monitoringapi.PluginMem
	Swap               = "swap"
	Load               = "load"
	DiskIO             = monitoringapi.PluginDiskIO
	FS                 = "fs"
	Network            = monitoringapi.PluginNetwork
	GPU                = "gpu"
	Sensors            = "sensors"
	Containers         = monitoringapi.PluginContainers
	ContainerTelemetry = monitoringapi.PluginContainerTelemetry
	Processes          = "processes"
	Programs           = "programs"
	Connections        = "connections"
	IRQ                = "irq"
	Smart              = "smart"
)

var names = []string{
	CPU,
	Mem,
	Swap,
	Load,
	DiskIO,
	FS,
	Network,
	GPU,
	Sensors,
	Containers,
	ContainerTelemetry,
	Processes,
	Programs,
	Connections,
	IRQ,
	Smart,
}

var defaultHistory = []string{
	CPU,
	Mem,
	Swap,
	DiskIO,
	Network,
	Containers,
	ContainerTelemetry,
}

var liveOnly = map[string]struct{}{
	Processes: {},
	Programs:  {},
}

// Names returns the canonical v1 plugin order.
func Names() []string {
	return slices.Clone(names)
}

// DefaultHistory returns the default plugin history allowlist.
func DefaultHistory() []string {
	return slices.Clone(defaultHistory)
}

// IsName reports whether name is a known plugin.
func IsName(name string) bool {
	return slices.Contains(names, name)
}

// IsLiveOnly reports whether a plugin is collected for live responses only and
// is never persisted to the metrics database.
func IsLiveOnly(name string) bool {
	_, ok := liveOnly[name]
	return ok
}

// HistoryCapable returns the plugins that can keep history, in canonical order.
func HistoryCapable() []string {
	out := make([]string, 0, len(names)-len(liveOnly))
	for _, plugin := range names {
		if !IsLiveOnly(plugin) {
			out = append(out, plugin)
		}
	}
	return out
}
