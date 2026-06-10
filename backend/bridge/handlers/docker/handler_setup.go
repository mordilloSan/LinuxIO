package docker

import (
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

type dockerHandlers struct {
	rt runtime.Runtime
}

func newDockerHandlers(rt runtime.Runtime) dockerHandlers {
	return dockerHandlers{rt: rt}
}

func prepareDockerHandlers(router *bridgeipc.Router, handlers dockerHandlers) {
	RegisterJobRoutes(router, handlers.rt)

	if err := initIconCache(); err != nil {
		slog.Warn("failed to initialize icon cache", "component", "docker", "subsystem", "icons", "error", err)
	}
}
