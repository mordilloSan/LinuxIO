package container

import "time"

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
	Name      string    `json:"name" cbor:"0,keyasint"`
	Cpu       float64   `json:"cpu_percent" cbor:"1,keyasint"`
	Mem       float64   `json:"memory_mb" cbor:"2,keyasint"`
	Bandwidth [2]uint64 `json:"bandwidth_bytes,omitempty,omitzero" cbor:"9,keyasint,omitzero"` // [sent bytes, recv bytes]

	Health DockerHealth `json:"-" cbor:"5,keyasint"`
	Status string       `json:"-" cbor:"6,keyasint"`
	Id     string       `json:"-" cbor:"7,keyasint"`
	FullID string       `json:"-" cbor:"-"`
	Image  string       `json:"-" cbor:"8,keyasint"`
	Ports  string       `json:"-" cbor:"10,keyasint"`
	// PrevCpu     [2]uint64    `json:"-"`
	CpuSystem    uint64    `json:"-"`
	CpuContainer uint64    `json:"-"`
	PrevReadTime time.Time `json:"-"`
}
