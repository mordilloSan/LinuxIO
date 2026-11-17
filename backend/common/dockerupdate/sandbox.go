package dockerupdate

import "github.com/mordilloSan/LinuxIO/backend/common/version"

// RunnerPath is the root-sandboxed Docker updater the scheduled service
// executes.
const RunnerPath = version.BinDir + "/linuxio-docker-update"

// UnitSpec is how the scheduled Docker updater is confined and ordered; the
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

// UpdaterUnit is the confinement of the Docker updater.
var UpdaterUnit = UnitSpec{
	Requires:        []string{"docker.service"},
	After:           []string{"docker.service", "network-online.target"},
	Wants:           []string{"network-online.target"},
	ProtectSystem:   "strict",
	ProtectHome:     "read-only",
	ReadWritePaths:  []string{version.DataDir, "/run"},
	PrivateTmp:      true,
	NoNewPrivileges: true,
}

// scheduledArgs are the updater's arguments for one scheduled pass. It takes
// no command word: starting it is the pass.
func scheduledArgs() []string {
	return []string{"--config", ConfigPath}
}
