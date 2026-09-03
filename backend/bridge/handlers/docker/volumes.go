package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/volume"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

const maxDockerResourceEntries = 256

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
	containersResp, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list containers using volumes: %w", err)
	}
	volumes := dockerVolumesFromSDK(volumesResp.Items)
	attachVolumeContainers(volumes, containersResp.Items)
	return volumes, nil
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
		ClusterVolume:        opaqueDockerValue(item.ClusterVolume),
		Driver:               item.Driver,
		Labels:               cloneStringMap(item.Labels),
		Mountpoint:           item.Mountpoint,
		MountpointAccessible: volumeMountpointAccessible(item.Mountpoint),
		Name:                 item.Name,
		Options:              cloneStringMap(item.Options),
		Status:               cloneAnyMap(item.Status),
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

func volumeMountpointAccessible(path string) bool {
	if !filepath.IsAbs(path) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func attachVolumeContainers(volumes []apischema.DockerVolume, containers []container.Summary) {
	volumeIndexes := make(map[string]int, len(volumes))
	for index := range volumes {
		volumeIndexes[volumes[index].Name] = index
	}
	for _, item := range containers {
		attached := make(map[string]struct{}, len(item.Mounts))
		for _, mounted := range item.Mounts {
			index, ok := volumeIndexes[mounted.Name]
			if !ok || mounted.Type != mount.TypeVolume {
				continue
			}
			if _, exists := attached[mounted.Name]; exists {
				continue
			}
			attached[mounted.Name] = struct{}{}
			name := strings.TrimPrefix(firstString(item.Names), "/")
			if name == "" {
				name = item.ID
			}
			volumes[index].Containers = append(volumes[index].Containers, apischema.DockerVolumeContainer{
				ID: item.ID, Name: name, State: string(item.State),
			})
		}
	}
	for index := range volumes {
		sort.Slice(volumes[index].Containers, func(i, j int) bool {
			return volumes[index].Containers[i].Name < volumes[index].Containers[j].Name
		})
	}
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
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

type volumeRemover interface {
	VolumeRemove(context.Context, string, client.VolumeRemoveOptions) (client.VolumeRemoveResult, error)
}

// DeleteVolume removes an unused volume. Docker owns the in-use check.
func DeleteVolume(ctx context.Context, name string) error {
	cli, err := getClient()
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	return deleteVolume(ctx, cli, name)
}

func deleteVolume(ctx context.Context, cli volumeRemover, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return bridgeipc.NewError("volume name is required", 400)
	}
	if _, err := cli.VolumeRemove(ctx, name, client.VolumeRemoveOptions{}); err != nil {
		if errdefs.IsConflict(err) {
			return bridgeipc.NewError(fmt.Sprintf("volume %q is in use: %v", name, err), 409)
		}
		return fmt.Errorf("failed to remove volume %q: %w", name, err)
	}
	return nil
}

func CreateVolume(ctx context.Context, request apischema.DockerVolumeCreateRequest) error {
	options, err := volumeCreateOptions(request)
	if err != nil {
		return bridgeipc.NewError(err.Error(), 400)
	}
	cli, err := getClient()
	if err != nil {
		return fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	if _, err := cli.VolumeCreate(ctx, options); err != nil {
		return fmt.Errorf("failed to create volume %q: %w", options.Name, err)
	}
	return nil
}

func volumeCreateOptions(request apischema.DockerVolumeCreateRequest) (client.VolumeCreateOptions, error) {
	name := strings.TrimSpace(request.Name)
	if !containerNamePattern.MatchString(name) {
		return client.VolumeCreateOptions{}, fmt.Errorf("volume name must start with an alphanumeric character and contain only alphanumeric characters, underscores, periods, or hyphens")
	}
	driver := strings.TrimSpace(request.Driver)
	if driver == "" {
		return client.VolumeCreateOptions{}, fmt.Errorf("volume driver is required")
	}
	labels, err := normalizedDockerStringMap(request.Labels, "volume labels")
	if err != nil {
		return client.VolumeCreateOptions{}, err
	}
	return client.VolumeCreateOptions{Name: name, Driver: driver, Labels: labels}, nil
}

func normalizedDockerStringMap(values map[string]string, field string) (map[string]string, error) {
	if len(values) > maxDockerResourceEntries {
		return nil, fmt.Errorf("%s cannot contain more than %d entries", field, maxDockerResourceEntries)
	}
	if len(values) == 0 {
		return nil, nil
	}
	result := make(map[string]string, len(values))
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("%s cannot contain an empty key", field)
		}
		result[key] = value
	}
	return result, nil
}
