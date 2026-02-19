package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/filters"
	"github.com/mordilloSan/go-logger/logger"
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

// SystemPrune removes unused Docker resources according to opts.
func SystemPrune(opts PruneOptions) (*PruneResult, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	ctx := context.Background()
	result := &PruneResult{}

	if opts.Containers {
		report, err := cli.ContainersPrune(ctx, filters.Args{})
		if err != nil {
			return nil, fmt.Errorf("container prune failed: %w", err)
		}
		result.ContainersDeleted = report.ContainersDeleted
		result.SpaceReclaimed += report.SpaceReclaimed
	}

	if opts.Images {
		// Prune all unused images (dangling=false includes tagged-but-unreferenced images, equiv. to docker image prune -a)
		imageFilters := filters.NewArgs(filters.Arg("dangling", "false"))
		report, err := cli.ImagesPrune(ctx, imageFilters)
		if err != nil {
			return nil, fmt.Errorf("image prune failed: %w", err)
		}
		for _, img := range report.ImagesDeleted {
			if img.Deleted != "" {
				result.ImagesDeleted = append(result.ImagesDeleted, img.Deleted)
			}
		}
		result.SpaceReclaimed += report.SpaceReclaimed
	}

	if opts.BuildCache {
		report, err := cli.BuildCachePrune(ctx, build.CachePruneOptions{All: true})
		if err != nil {
			return nil, fmt.Errorf("build cache prune failed: %w", err)
		}
		result.SpaceReclaimed += report.SpaceReclaimed
	}

	if opts.Networks {
		report, err := cli.NetworksPrune(ctx, filters.Args{})
		if err != nil {
			return nil, fmt.Errorf("network prune failed: %w", err)
		}
		result.NetworksDeleted = report.NetworksDeleted
	}

	if opts.Volumes {
		report, err := cli.VolumesPrune(ctx, filters.Args{})
		if err != nil {
			return nil, fmt.Errorf("volume prune failed: %w", err)
		}
		result.VolumesDeleted = report.VolumesDeleted
		result.SpaceReclaimed += report.SpaceReclaimed
	}

	return result, nil
}
