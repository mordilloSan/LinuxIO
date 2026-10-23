package container

import (
	"time"
)

type DockerHealth = uint8

const (
	DockerHealthNone DockerHealth = iota
	DockerHealthStarting
	DockerHealthHealthy
	DockerHealthUnhealthy
)

var DockerHealthStrings = map[string]DockerHealth{
	"none":      DockerHealthNone,
	"starting":  DockerHealthStarting,
	"healthy":   DockerHealthHealthy,
	"unhealthy": DockerHealthUnhealthy,
}

// Docker container stats
type Stats struct {
	Name      string    `json:"name"`
	Cpu       float64   `json:"cpu_percent"`
	Mem       float64   `json:"memory_mb"`
	Bandwidth [2]uint64 `json:"bandwidth_bytes,omitempty,omitzero"` // [sent bytes, recv bytes]

	Health DockerHealth `json:"-"`
	Status string       `json:"-"`
	Id     string       `json:"-"`
	FullID string       `json:"-"`
	Image  string       `json:"-"`
	Ports  string       `json:"-"`
	// PrevCpu     [2]uint64    `json:"-"`
	CpuSystem    uint64    `json:"-"`
	CpuContainer uint64    `json:"-"`
	PrevReadTime time.Time `json:"-"`
}
