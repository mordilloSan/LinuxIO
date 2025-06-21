package docker

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/image"
)

// List all images
func ListImages() (any, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer cli.Close()

	images, err := cli.ImageList(context.Background(), image.ListOptions{All: true})
	if err != nil {
		return nil, fmt.Errorf("failed to list images: %w", err)
	}

	return images, nil
}
