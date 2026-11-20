package memory

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, apischema.SwapStatus]("memory.get_swap_status", apischema.RetrySafe()).Handle(handleGetSwapStatus),
	// Action is the requested mode: a dialog that takes up a running change
	// again needs it to say what the change left.
	apischema.MutationTask("memory.set_swap_mode", handleSetSwapMode, apischema.Privileged(), apischema.Progress[apischema.SwapProgress](), apischema.WithTaskMetadata(func(req apischema.SwapModeRequest) bridgeipc.TaskMetadata {
		return bridgeipc.TaskMetadata{Identity: []string{"memory", "swap_mode"}, Label: "Changing how memory overflows", Action: string(req.Mode)}
	})),
	apischema.Mutation("memory.set_swappiness", handleSetSwappiness, apischema.Privileged()),
	apischema.MutationTask("memory.drain_swap", handleDrainSwap, apischema.Privileged(), apischema.Progress[apischema.SwapProgress](), apischema.WithTaskMetadata(func(apischema.NoRequest) bridgeipc.TaskMetadata {
		return bridgeipc.TaskMetadata{Identity: []string{"memory", "drain_swap"}, Label: "Moving swapped data back into memory"}
	})),
)

var Routes = api.Routes()

func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleGetSwapStatus(ctx context.Context, _ apischema.NoRequest) (apischema.SwapStatus, error) {
	return GetSwapStatus(ctx)
}

func handleSetSwapMode(ctx context.Context, task *bridgeipc.Task, req apischema.SwapModeRequest) (apischema.SwapModeResult, error) {
	return SetSwapMode(ctx, req, func(p apischema.SwapProgress) { task.ReportProgress(p) })
}

func handleSetSwappiness(ctx context.Context, req apischema.SwappinessRequest) (apischema.SwapStatus, error) {
	return SetSwappiness(ctx, req.Swappiness)
}

func handleDrainSwap(ctx context.Context, task *bridgeipc.Task, _ apischema.NoRequest) (apischema.SwapStatus, error) {
	return DrainSwap(ctx, func(p apischema.SwapProgress) { task.ReportProgress(p) })
}
