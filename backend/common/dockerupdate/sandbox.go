package dockerupdate

import (
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// RunnerPath is the root-sandboxed worker binary both the scheduled service
// and every durable transient unit execute.
const RunnerPath = version.BinDir + "/linuxio-docker-update"

// UnitSpec is the one definition of how the update worker is confined and
// ordered. The scheduled service unit text (timer.go) and the bridge's durable
// transient-unit properties are both derived from it, so the two cannot drift.
type UnitSpec struct {
	Requires        []string
	After           []string
	Wants           []string
	ProtectSystem   string
	ProtectHome     string
	ReadWritePaths  []string
	PrivateTmp      bool
	NoNewPrivileges bool

	// The remaining fields apply to the durable transient unit only; the
	// scheduled service runs until it finishes.
	RuntimeLimit     time.Duration
	StopTimeout      time.Duration
	SyslogIdentifier string
}

// WorkerUnit is the confinement of every update worker.
var WorkerUnit = UnitSpec{
	Requires:         []string{"docker.service"},
	After:            []string{"docker.service", "network-online.target"},
	Wants:            []string{"network-online.target"},
	ProtectSystem:    "strict",
	ProtectHome:      "read-only",
	ReadWritePaths:   []string{version.DataDir, "/run"},
	PrivateTmp:       true,
	NoNewPrivileges:  true,
	RuntimeLimit:     20 * time.Minute,
	StopTimeout:      30 * time.Second,
	SyslogIdentifier: "linuxio-docker-update",
}

// DurableArgs are the worker arguments, after RunnerPath, that run one
// persisted durable operation.
func DurableArgs(operationID string) []string {
	return []string{"run-operation", "--id", operationID}
}

// scheduledArgs are the worker arguments that run one scheduled pass.
func scheduledArgs() []string {
	return []string{"run", "--config", ConfigPath}
}
