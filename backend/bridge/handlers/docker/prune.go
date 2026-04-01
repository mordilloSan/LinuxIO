package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/versions"
	"github.com/docker/docker/client"
)

// PruneOptions controls which Docker resources are pruned.
type PruneOptions struct {
	Containers bool `json:"containers"`
	Images     bool `json:"images"`     // dangling only
	BuildCache bool `json:"buildCache"` //nolint:tagliatelle
	Networks   bool `json:"networks"`
	Volumes    bool `json:"volumes"`
}

// PruneResult summarises what was removed and how much space was reclaimed.
type PruneResult struct {
	ContainersDeleted []string `json:"containersDeleted,omitempty"`
	ImagesDeleted     []string `json:"imagesDeleted,omitempty"`
	NetworksDeleted   []string `json:"networksDeleted,omitempty"`
	VolumesDeleted    []string `json:"volumesDeleted,omitempty"`
	SpaceReclaimed    uint64   `json:"spaceReclaimed"`
}

func volumePruneFilters(apiVersion string) filters.Args {
	if versions.GreaterThanOrEqualTo(apiVersion, "1.42") {
		return filters.NewArgs(filters.Arg("all", "true"))
	}

	return filters.Args{}
}

// SystemPrune removes unused Docker resources according to opts.
func SystemPrune(opts PruneOptions) (*PruneResult, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	ctx := context.Background()
	result := &PruneResult{}

	if err := pruneContainers(ctx, cli, opts, result); err != nil {
		return nil, err
	}
	if err := pruneImages(ctx, cli, opts, result); err != nil {
		return nil, err
	}
	if err := pruneBuildCache(ctx, cli, opts, result); err != nil {
		return nil, err
	}
	if err := pruneNetworks(ctx, cli, opts, result); err != nil {
		return nil, err
	}
	if err := pruneVolumes(ctx, cli, opts, result); err != nil {
		return nil, err
	}

	return result, nil
}

func pruneContainers(ctx context.Context, cli *client.Client, opts PruneOptions, result *PruneResult) error {
	if !opts.Containers {
		return nil
	}
	report, err := cli.ContainersPrune(ctx, filters.Args{})
	if err != nil {
		return fmt.Errorf("container prune failed: %w", err)
	}
	result.ContainersDeleted = report.ContainersDeleted
	result.SpaceReclaimed += report.SpaceReclaimed
	return nil
}

func pruneImages(ctx context.Context, cli *client.Client, opts PruneOptions, result *PruneResult) error {
	if !opts.Images {
		return nil
	}
	report, err := cli.ImagesPrune(ctx, filters.NewArgs(filters.Arg("dangling", "false")))
	if err != nil {
		return fmt.Errorf("image prune failed: %w", err)
	}
	for _, img := range report.ImagesDeleted {
		if img.Deleted != "" {
			result.ImagesDeleted = append(result.ImagesDeleted, img.Deleted)
		}
	}
	result.SpaceReclaimed += report.SpaceReclaimed
	return nil
}

func pruneBuildCache(ctx context.Context, cli *client.Client, opts PruneOptions, result *PruneResult) error {
	if !opts.BuildCache {
		return nil
	}
	report, err := cli.BuildCachePrune(ctx, build.CachePruneOptions{All: true})
	if err != nil {
		return fmt.Errorf("build cache prune failed: %w", err)
	}
	result.SpaceReclaimed += report.SpaceReclaimed
	return nil
}

func pruneNetworks(ctx context.Context, cli *client.Client, opts PruneOptions, result *PruneResult) error {
	if !opts.Networks {
		return nil
	}
	report, err := cli.NetworksPrune(ctx, filters.Args{})
	if err != nil {
		return fmt.Errorf("network prune failed: %w", err)
	}
	result.NetworksDeleted = report.NetworksDeleted
	return nil
}

func pruneVolumes(ctx context.Context, cli *client.Client, opts PruneOptions, result *PruneResult) error {
	if !opts.Volumes {
		return nil
	}
	volumeFilters := volumePruneFilters(cli.ClientVersion())
	report, err := cli.VolumesPrune(ctx, volumeFilters)
	if err != nil && volumeFilters.Contains("all") {
		report, err = cli.VolumesPrune(ctx, filters.Args{})
	}
	if err != nil {
		return fmt.Errorf("volume prune failed: %w", err)
	}
	result.VolumesDeleted = report.VolumesDeleted
	result.SpaceReclaimed += report.SpaceReclaimed
	return nil
}
