package control

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, apischema.VersionResponse]("control.version", apischema.RetrySafe()).Handle(handleVersion),
	// These calls are accepted by the native control service before it can tear
	// down this bridge. A lost response is therefore an expected, ambiguous
	// transport outcome; callers must not retry it automatically.
	apischema.MutationVoid("control.reboot", handleReboot, apischema.Privileged()),
	apischema.MutationVoid("control.power_off", handlePowerOff, apischema.Privileged()),
)

var Routes = api.Routes()

// RegisterHandlers registers host control and version handlers.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleReboot(ctx context.Context, _ apischema.NoRequest) error {
	return Reboot(ctx)
}

func handlePowerOff(ctx context.Context, _ apischema.NoRequest) error {
	return PowerOff(ctx)
}
