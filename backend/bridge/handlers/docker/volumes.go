package docker

import (
	"context"
	"fmt"
	"sort"

	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"
)

// List all volumes
func ListVolumes(ctx context.Context) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	volumesResp, err := cli.VolumeList(ctx, client.VolumeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}
	volumes := volumesResp.Items
	if volumes == nil {
		volumes = []volume.Volume{}
	}

	// Sort volumes by Name alphabetically
	sort.Slice(volumes, func(i, j int) bool {
		return volumes[i].Name < volumes[j].Name
	})

	return volumes, nil
}

// Delete a volume
func DeleteVolume(ctx context.Context, name string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	if _, err = cli.VolumeRemove(ctx, name, client.VolumeRemoveOptions{Force: true}); err != nil {
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

	volume, err := cli.VolumeCreate(ctx, client.VolumeCreateOptions{
		Name: name,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create volume: %w", err)
	}

	return volume.Volume, nil
}
