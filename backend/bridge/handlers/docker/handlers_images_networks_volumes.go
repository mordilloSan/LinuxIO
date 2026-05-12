package docker

import (
	"context"
	"log/slog"

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleListImages(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListImages()
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteImage(ctx context.Context, args []string, emit bridgeipc.Events) error {
	id, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("delete_image requested", "component", "docker", "image", id)
	result, err := DeleteImage(id)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleListNetworks(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListDockerNetworks()
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleCreateNetwork(ctx context.Context, args []string, emit bridgeipc.Events) error {
	name, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("create_network requested", "component", "docker", "network", name)
	result, err := CreateDockerNetwork(name)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteNetwork(ctx context.Context, args []string, emit bridgeipc.Events) error {
	name, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("delete_network requested", "component", "docker", "network", name)
	result, err := DeleteDockerNetwork(name)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleListVolumes(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListVolumes()
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleCreateVolume(ctx context.Context, args []string, emit bridgeipc.Events) error {
	name, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("create_volume requested", "component", "docker", "volume", name)
	result, err := CreateVolume(name)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteVolume(ctx context.Context, args []string, emit bridgeipc.Events) error {
	name, err := bridgeipc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("delete_volume requested", "component", "docker", "volume", name)
	result, err := DeleteVolume(name)
	return bridgeipc.EmitResult(emit, result, err)
}
