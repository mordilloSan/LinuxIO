package docker

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func (h dockerHandlers) handleListContainers(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := ListContainers(ctx)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleStartContainer(ctx context.Context, args []string, emit ipc.Events) error {
	id, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("start_container requested", "component", "docker", "container", id)
	result, err := StartContainer(ctx, id)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleStopContainer(ctx context.Context, args []string, emit ipc.Events) error {
	id, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("stop_container requested", "component", "docker", "container", id)
	result, err := StopContainer(ctx, id)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleRemoveContainer(ctx context.Context, args []string, emit ipc.Events) error {
	id, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("remove_container requested", "component", "docker", "container", id)
	result, err := RemoveContainer(ctx, id)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleRestartContainer(ctx context.Context, args []string, emit ipc.Events) error {
	id, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("restart_container requested", "component", "docker", "container", id)
	result, err := RestartContainer(ctx, id)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleStartAllStopped(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("start_all_stopped requested", "component", "docker")
	result, err := StartAllStopped(ctx)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleStopAllRunning(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("stop_all_running requested", "component", "docker")
	result, err := StopAllRunning(ctx)
	return rpc.EmitResult(emit, result, err)
}
