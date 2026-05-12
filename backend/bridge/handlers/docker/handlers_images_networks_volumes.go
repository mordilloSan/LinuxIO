package docker

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func (h dockerHandlers) handleListImages(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := ListImages()
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteImage(ctx context.Context, args []string, emit ipc.Events) error {
	id, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("delete_image requested", "component", "docker", "image", id)
	result, err := DeleteImage(id)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleListNetworks(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := ListDockerNetworks()
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleCreateNetwork(ctx context.Context, args []string, emit ipc.Events) error {
	name, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("create_network requested", "component", "docker", "network", name)
	result, err := CreateDockerNetwork(name)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteNetwork(ctx context.Context, args []string, emit ipc.Events) error {
	name, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("delete_network requested", "component", "docker", "network", name)
	result, err := DeleteDockerNetwork(name)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleListVolumes(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := ListVolumes()
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleCreateVolume(ctx context.Context, args []string, emit ipc.Events) error {
	name, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("create_volume requested", "component", "docker", "volume", name)
	result, err := CreateVolume(name)
	return rpc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteVolume(ctx context.Context, args []string, emit ipc.Events) error {
	name, err := rpc.Arg(args, 0)
	if err != nil {
		return err
	}
	slog.Info("delete_volume requested", "component", "docker", "volume", name)
	result, err := DeleteVolume(name)
	return rpc.EmitResult(emit, result, err)
}
