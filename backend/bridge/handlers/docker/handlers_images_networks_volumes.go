package docker

import (
	"context"

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleListImages(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListImages(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteImage(ctx context.Context, args []string, emit bridgeipc.Events) error {
	id, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := DeleteImage(ctx, id)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleListNetworks(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListDockerNetworks(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleCreateNetwork(ctx context.Context, args []string, emit bridgeipc.Events) error {
	name, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := CreateDockerNetwork(ctx, name)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteNetwork(ctx context.Context, args []string, emit bridgeipc.Events) error {
	name, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := DeleteDockerNetwork(ctx, name)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleListVolumes(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListVolumes(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleCreateVolume(ctx context.Context, args []string, emit bridgeipc.Events) error {
	name, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := CreateVolume(ctx, name)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteVolume(ctx context.Context, args []string, emit bridgeipc.Events) error {
	name, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	result, err := DeleteVolume(ctx, name)
	return bridgeipc.EmitResult(emit, result, err)
}
