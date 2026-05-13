package docker

import (
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

type dockerHandlers struct {
	username string
	store    *config.UserStore
}

func newDockerHandlers(rt runtime.Runtime) dockerHandlers {
	return dockerHandlers{
		username: rt.Username(),
		store:    rt.Store,
	}
}

func prepareDockerHandlers(router *bridgeipc.Router, handlers dockerHandlers) {
	RegisterJobRoutes(router, handlers.username, handlers.store)
	go watchtowerOnce.Do(func() { SyncWatchtowerStackWithStore(handlers.username, handlers.store) })

	if err := initIconCache(); err != nil {
		slog.Warn("failed to initialize icon cache", "component", "docker", "subsystem", "icons", "error", err)
	}
}
