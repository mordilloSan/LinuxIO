package monitoring

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Query[apischema.NoRequest, apischema.MonitoringConfig]("monitoring.get_config", apischema.Privileged()).Handle(handleGetConfig),
	apischema.Query[apischema.NoRequest, apischema.MonitoringStatus]("monitoring.get_status", apischema.Privileged()).Handle(handleGetStatus),
	apischema.Job[apischema.MonitoringConfigPatch, apischema.MonitoringConfigSetResult]("monitoring.set_config", apischema.Privileged()).Handle(handleSetConfig),
	apischema.Job[apischema.NoRequest, apischema.NoResponse]("monitoring.restart", apischema.Privileged()).Handle(handleRestart),
)

var Routes = api.Routes()

// RegisterHandlers registers monitoring admin handlers with the bridge.
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetConfig(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	cfg, err := FetchConfig(ctx)
	return bridgeipc.EmitResult(emit, cfg, err)
}

func handleGetStatus(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	status, err := FetchStatus(ctx)
	return bridgeipc.EmitResult(emit, status, err)
}

func handleSetConfig(ctx context.Context, req apischema.MonitoringConfigPatch, emit bridgeipc.Events) error {
	cfg, restartRequired, err := UpdateConfig(ctx, req)
	if err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, apischema.MonitoringConfigSetResult{
		Config:          cfg,
		RestartRequired: restartRequired,
	}, nil)
}

func handleRestart(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	return bridgeipc.EmitResult(emit, nil, RestartAgent(ctx))
}
