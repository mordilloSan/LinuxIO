package docker

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func (h dockerHandlers) handleListImages(ctx context.Context, _ apischema.NoRequest) ([]apischema.DockerImage, error) {
	return ListImages(ctx)
}

// The delete/create handlers below are void: their routes declare NoResponse, so
// the docker object or status string their domain functions return was never
// reachable — and on a Task route it lingered in the snapshot instead.
func (h dockerHandlers) handleDeleteImage(ctx context.Context, req apischema.ImageIDRequest) error {
	_, err := DeleteImage(ctx, req.ImageID)
	return err
}

func (h dockerHandlers) handleListNetworks(ctx context.Context, _ apischema.NoRequest) ([]apischema.DockerNetwork, error) {
	return ListDockerNetworks(ctx)
}

func (h dockerHandlers) handleCreateNetwork(ctx context.Context, req apischema.NameRequest) error {
	_, err := CreateDockerNetwork(ctx, req.Name)
	return err
}

func (h dockerHandlers) handleDeleteNetwork(ctx context.Context, req apischema.IDRequest) error {
	_, err := DeleteDockerNetwork(ctx, req.ID)
	return err
}

func (h dockerHandlers) handleListVolumes(ctx context.Context, _ apischema.NoRequest) ([]apischema.DockerVolume, error) {
	return ListVolumes(ctx)
}

func (h dockerHandlers) handleCreateVolume(ctx context.Context, req apischema.NameRequest) error {
	_, err := CreateVolume(ctx, req.Name)
	return err
}

func (h dockerHandlers) handleDeleteVolume(ctx context.Context, req apischema.NameRequest) error {
	_, err := DeleteVolume(ctx, req.Name)
	return err
}
