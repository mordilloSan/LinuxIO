package docker

import (
	"context"
	"fmt"

	"github.com/moby/moby/client"
	"github.com/moby/moby/client/pkg/versions"
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

func volumePruneOptions(apiVersion string) client.VolumePruneOptions {
	if versions.GreaterThanOrEqualTo(apiVersion, "1.42") {
		return client.VolumePruneOptions{All: true}
	}

	return client.VolumePruneOptions{}
}

// SystemPrune removes unused Docker resources according to opts.
func SystemPrune(ctx context.Context, opts PruneOptions) (*PruneResult, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

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
	report, err := cli.ContainerPrune(ctx, client.ContainerPruneOptions{})
	if err != nil {
		return fmt.Errorf("container prune failed: %w", err)
	}
	result.ContainersDeleted = report.Report.ContainersDeleted
	result.SpaceReclaimed += report.Report.SpaceReclaimed
	return nil
}

func pruneImages(ctx context.Context, cli *client.Client, opts PruneOptions, result *PruneResult) error {
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

func pruneBuildCache(ctx context.Context, cli *client.Client, opts PruneOptions, result *PruneResult) error {
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

func pruneNetworks(ctx context.Context, cli *client.Client, opts PruneOptions, result *PruneResult) error {
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

func pruneVolumes(ctx context.Context, cli *client.Client, opts PruneOptions, result *PruneResult) error {
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
