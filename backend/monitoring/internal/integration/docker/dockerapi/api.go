// Package dockerapi contains inbound Docker Engine API wire types.
package dockerapi

import "time"

// Info is a container item from /containers/json.
type Info struct {
	Id      string
	IdShort string
	Names   []string
	Status  string
	State   string
	Image   string
	Health  struct {
		Status string
	}
	Ports []struct {
		PublicPort uint16
		IP         string
	}
}

// Stats is a container resource snapshot from /containers/{id}/stats.
type Stats struct {
	Read        time.Time `json:"read"` // Time of stats generation
	Networks    map[string]NetworkStats
	CPUStats    CPUStats    `json:"cpu_stats"`
	MemoryStats MemoryStats `json:"memory_stats"`
}

// HostInfo is Docker system information from /info.
type HostInfo struct {
	OperatingSystem string `json:"OperatingSystem"`
	KernelVersion   string `json:"KernelVersion"`
	NCPU            int    `json:"NCPU"`
	MemTotal        uint64 `json:"MemTotal"`
}

func (s *Stats) CalculateCPUPercentLinux(prevCPUContainer uint64, prevCPUSystem uint64) float64 {
	if s.CPUStats.CPUUsage.TotalUsage < prevCPUContainer || s.CPUStats.SystemUsage < prevCPUSystem {
		return 0.0
	}

	cpuDelta := s.CPUStats.CPUUsage.TotalUsage - prevCPUContainer
	systemDelta := s.CPUStats.SystemUsage - prevCPUSystem

	if systemDelta == 0 || prevCPUContainer == 0 {
		return 0.0
	}

	return float64(cpuDelta) / float64(systemDelta) * 100.0
}

type CPUStats struct {
	CPUUsage    CPUUsage `json:"cpu_usage"`
	SystemUsage uint64   `json:"system_cpu_usage,omitempty"`
}

type CPUUsage struct {
	// Total CPU time consumed, in nanoseconds.
	TotalUsage uint64 `json:"total_usage"`
}

type MemoryStats struct {
	Usage uint64           `json:"usage,omitempty"`
	Stats MemoryStatsStats `json:"stats"`
}

type MemoryStatsStats struct {
	Cache        uint64 `json:"cache,omitempty"`
	InactiveFile uint64 `json:"inactive_file,omitempty"`
}

type NetworkStats struct {
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}
