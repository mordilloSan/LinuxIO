package docker

import (
	"context"
	"fmt"
	"sort"

	"github.com/docker/docker/api/types/volume"
)

// List all volumes
func ListVolumes(ctx context.Context) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	volumesResp, err := cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}
	if volumesResp.Volumes == nil {
		volumesResp.Volumes = []*volume.Volume{}
	}

	// Sort volumes by Name alphabetically
	sort.Slice(volumesResp.Volumes, func(i, j int) bool {
		return volumesResp.Volumes[i].Name < volumesResp.Volumes[j].Name
	})

	return volumesResp.Volumes, nil
}

// Delete a volume
func DeleteVolume(ctx context.Context, name string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	err = cli.VolumeRemove(ctx, name, true)
	if err != nil {
		return nil, fmt.Errorf("failed to remove volume: %w", err)
	}

	return nil, nil
}

// Create a volume
func CreateVolume(ctx context.Context, name string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	volume, err := cli.VolumeCreate(ctx, volume.CreateOptions{
		Name: name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create volume: %w", err)
	}

	return volume, nil
}
