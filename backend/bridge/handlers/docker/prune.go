package docker

import (
	"context"
	"fmt"

	"github.com/moby/moby/client"
	"github.com/moby/moby/client/pkg/versions"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dockerclient"
)

func volumePruneOptions(apiVersion string) client.VolumePruneOptions {
	if versions.GreaterThanOrEqualTo(apiVersion, "1.42") {
		return client.VolumePruneOptions{All: true}
	}

	return client.VolumePruneOptions{}
}

// SystemPrune removes unused Docker resources according to opts.
func SystemPrune(ctx context.Context, opts apischema.DockerSystemPruneRequest) (*apischema.DockerSystemPruneResponse, error) {
	cli, err := dockerclient.Get()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer dockerclient.Release(cli)

	result := &apischema.DockerSystemPruneResponse{}

	for _, step := range []func(context.Context, *client.Client, apischema.DockerSystemPruneRequest, *apischema.DockerSystemPruneResponse) error{
		pruneContainers, pruneImages, pruneBuildCache, pruneNetworks, pruneVolumes,
	} {
		if err := step(ctx, cli, opts, result); err != nil {
			return nil, withPruned(err, result)
		}
	}

	return result, nil
}

// withPruned names what earlier steps already removed, since a failed prune
// returns no response to carry it.
func withPruned(err error, result *apischema.DockerSystemPruneResponse) error {
	if result.SpaceReclaimed == 0 && len(result.ContainersDeleted)+len(result.ImagesDeleted)+len(result.NetworksDeleted)+len(result.VolumesDeleted) == 0 {
		return err
	}
	return fmt.Errorf("%w (already pruned: %d containers, %d images, %d networks, %d volumes, %d bytes reclaimed)",
		err, len(result.ContainersDeleted), len(result.ImagesDeleted), len(result.NetworksDeleted), len(result.VolumesDeleted), result.SpaceReclaimed)
}

func pruneContainers(ctx context.Context, cli *client.Client, opts apischema.DockerSystemPruneRequest, result *apischema.DockerSystemPruneResponse) error {
	if !opts.Containers {
		return nil
	}
	report, err := cli.ContainerPrune(ctx, client.ContainerPruneOptions{})
	if err != nil {
		return fmt.Errorf("container prune failed: %w", err)
	}
	result.ContainersDeleted = report.Report.ContainersDeleted
	result.SpaceReclaimed += report.Report.SpaceReclaimed
	return nil
}

func pruneImages(ctx context.Context, cli *client.Client, opts apischema.DockerSystemPruneRequest, result *apischema.DockerSystemPruneResponse) error {
	if !opts.Images {
		return nil
	}
	report, err := cli.ImagePrune(ctx, client.ImagePruneOptions{
		Filters: client.Filters{}.Add("dangling", "false"),
	})
	if err != nil {
		return fmt.Errorf("image prune failed: %w", err)
	}
	for _, img := range report.Report.ImagesDeleted {
		if img.Deleted != "" {
			result.ImagesDeleted = append(result.ImagesDeleted, img.Deleted)
		}
	}
	result.SpaceReclaimed += report.Report.SpaceReclaimed
	return nil
}

func pruneBuildCache(ctx context.Context, cli *client.Client, opts apischema.DockerSystemPruneRequest, result *apischema.DockerSystemPruneResponse) error {
	if !opts.BuildCache {
		return nil
	}
	report, err := cli.BuildCachePrune(ctx, client.BuildCachePruneOptions{All: true})
	if err != nil {
		return fmt.Errorf("build cache prune failed: %w", err)
	}
	result.SpaceReclaimed += report.Report.SpaceReclaimed
	return nil
}

func pruneNetworks(ctx context.Context, cli *client.Client, opts apischema.DockerSystemPruneRequest, result *apischema.DockerSystemPruneResponse) error {
	if !opts.Networks {
		return nil
	}
	report, err := cli.NetworkPrune(ctx, client.NetworkPruneOptions{})
	if err != nil {
		return fmt.Errorf("network prune failed: %w", err)
	}
	result.NetworksDeleted = report.Report.NetworksDeleted
	return nil
}

func pruneVolumes(ctx context.Context, cli *client.Client, opts apischema.DockerSystemPruneRequest, result *apischema.DockerSystemPruneResponse) error {
	if !opts.Volumes {
		return nil
	}
	volumeOptions := volumePruneOptions(cli.ClientVersion())
	report, err := cli.VolumePrune(ctx, volumeOptions)
	if err != nil && volumeOptions.All {
		report, err = cli.VolumePrune(ctx, client.VolumePruneOptions{})
	}
	if err != nil {
		return fmt.Errorf("volume prune failed: %w", err)
	}
	result.VolumesDeleted = report.Report.VolumesDeleted
	result.SpaceReclaimed += report.Report.SpaceReclaimed
	return nil
}
