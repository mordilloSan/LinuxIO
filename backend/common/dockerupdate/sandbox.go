package dockerupdate

import "github.com/mordilloSan/LinuxIO/backend/common/version"

// RunnerPath is the root-sandboxed Docker updater the scheduled service
// executes. The service, with its sandbox, is a package unit
// (packaging/systemd/linuxio-docker-update.service).
const RunnerPath = version.LibexecDir + "/linuxio-docker-update"
