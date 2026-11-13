package dockerupdate

import "github.com/mordilloSan/LinuxIO/backend/common/version"

// RunnerPath is the root-sandboxed worker binary the scheduled service
// executes.
const RunnerPath = version.BinDir + "/linuxio-docker-update"

// UnitSpec is how the scheduled update worker is confined and ordered; the
// service unit text (timer.go) is derived from it.
type UnitSpec struct {
	Requires        []string
	After           []string
	Wants           []string
	ProtectSystem   string
	ProtectHome     string
	ReadWritePaths  []string
	PrivateTmp      bool
	NoNewPrivileges bool
}

// WorkerUnit is the confinement of the update worker.
var WorkerUnit = UnitSpec{
	Requires:        []string{"docker.service"},
	After:           []string{"docker.service", "network-online.target"},
	Wants:           []string{"network-online.target"},
	ProtectSystem:   "strict",
	ProtectHome:     "read-only",
	ReadWritePaths:  []string{version.DataDir, "/run"},
	PrivateTmp:      true,
	NoNewPrivileges: true,
}

// scheduledArgs are the worker arguments that run one scheduled pass.
func scheduledArgs() []string {
	return []string{"run", "--config", ConfigPath}
}
