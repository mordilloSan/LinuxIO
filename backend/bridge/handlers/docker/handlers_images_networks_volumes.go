package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func (h dockerHandlers) handleListImages(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := ListImages(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteImage(ctx context.Context, req apischema.ImageIDRequest, emit bridgeipc.Events) error {
	result, err := DeleteImage(ctx, req.ImageID)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleListNetworks(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := ListDockerNetworks(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleCreateNetwork(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	result, err := CreateDockerNetwork(ctx, req.Name)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteNetwork(ctx context.Context, req apischema.IDRequest, emit bridgeipc.Events) error {
	result, err := DeleteDockerNetwork(ctx, req.ID)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleListVolumes(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := ListVolumes(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleCreateVolume(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	result, err := CreateVolume(ctx, req.Name)
	return bridgeipc.EmitResult(emit, result, err)
}

func (h dockerHandlers) handleDeleteVolume(ctx context.Context, req apischema.NameRequest, emit bridgeipc.Events) error {
	result, err := DeleteVolume(ctx, req.Name)
	return bridgeipc.EmitResult(emit, result, err)
}
