package docker

import (
	"context"
	"fmt"
	"sort"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/docker/docker/api/types/image"
)

// List all images
func ListImages() (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	images, err := cli.ImageList(context.Background(), image.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	// Sort images by Created date (newest first)
	sort.Slice(images, func(i, j int) bool {
		return images[i].Created > images[j].Created
	})

	return images, nil
}

// Delete an image
func DeleteImage(imageID string) (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	_, err = cli.ImageRemove(context.Background(), imageID, image.RemoveOptions{Force: false, PruneChildren: true})
	if err != nil {
		return nil, fmt.Errorf("failed to remove image: %w", err)
	}

	return nil, nil
}
