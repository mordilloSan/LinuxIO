package docker

import (
	"context"

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleListContainers(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListContainers(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleStartContainer(ctx context.Context, args []string, emit bridgeipc.Events) error {
	id, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := StartContainer(ctx, id)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleStopContainer(ctx context.Context, args []string, emit bridgeipc.Events) error {
	id, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := StopContainer(ctx, id)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleRemoveContainer(ctx context.Context, args []string, emit bridgeipc.Events) error {
	id, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := RemoveContainer(ctx, id)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleRestartContainer(ctx context.Context, args []string, emit bridgeipc.Events) error {
	id, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := RestartContainer(ctx, id)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleStartAllStopped(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := StartAllStopped(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleStopAllRunning(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := StopAllRunning(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}
