package docker

import (
	"context"
	"fmt"
	"sort"

	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
)

// List all images
func ListImages(ctx context.Context) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	images, err := cli.ImageList(ctx, client.ImageListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}
	imageItems := images.Items
	if imageItems == nil {
		imageItems = []image.Summary{}
	}

	// Sort images by Created date (newest first)
	sort.Slice(imageItems, func(i, j int) bool {
		return imageItems[i].Created > imageItems[j].Created
	})

	return imageItems, nil
}

// Delete an image
func DeleteImage(ctx context.Context, imageID string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	_, err = cli.ImageRemove(ctx, imageID, client.ImageRemoveOptions{Force: false, PruneChildren: true})
	if err != nil {
		return nil, fmt.Errorf("failed to remove image: %w", err)
	}

	return nil, nil
}
