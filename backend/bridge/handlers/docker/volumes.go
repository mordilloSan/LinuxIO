package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"sort"

	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// List all volumes
func ListVolumes(ctx context.Context) ([]apischema.DockerVolume, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	volumesResp, err := cli.VolumeList(ctx, client.VolumeListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list volumes: %w", err)
	}
	return dockerVolumesFromSDK(volumesResp.Items), nil
}

// dockerVolumesFromSDK maps Docker's list response to our stable API model. It
// deliberately uses VolumeList only; UsageData is shown when Docker includes
// it, but we never make a separate, expensive disk-usage request for it.
func dockerVolumesFromSDK(volumes []volume.Volume) []apischema.DockerVolume {
	if len(volumes) == 0 {
		return []apischema.DockerVolume{}
	}

	result := make([]apischema.DockerVolume, 0, len(volumes))
	for _, item := range volumes {
		result = append(result, dockerVolumeFromSDK(item))
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func dockerVolumeFromSDK(item volume.Volume) apischema.DockerVolume {
	result := apischema.DockerVolume{
		ClusterVolume: opaqueDockerValue(item.ClusterVolume),
		Driver:        item.Driver,
		Labels:        cloneStringMap(item.Labels),
		Mountpoint:    item.Mountpoint,
		Name:          item.Name,
		Options:       cloneStringMap(item.Options),
		Status:        cloneAnyMap(item.Status),
	}
	if item.CreatedAt != "" {
		result.CreatedAt = new(item.CreatedAt)
	}
	if item.Scope != "" {
		result.Scope = new(item.Scope)
	}
	if item.UsageData != nil {
		result.UsageData = &apischema.DockerVolumeUsageData{
			RefCount: item.UsageData.RefCount,
			Size:     item.UsageData.Size,
		}
	}
	return result
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]string, len(values))
	maps.Copy(clone, values)
	return clone
}

func cloneAnyMap(values map[string]any) map[string]any {
	if len(values) == 0 {
		return nil
	}
	clone := make(map[string]any, len(values))
	maps.Copy(clone, values)
	return clone
}

// opaqueDockerValue preserves complex SDK payloads without making the route's
// result untyped. ClusterVolume is an optional Swarm CSI object with a large,
// evolving nested shape, so an opaque JSON object is the stable boundary.
func opaqueDockerValue(value any) map[string]any {
	if value == nil {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil
	}
	return result
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
