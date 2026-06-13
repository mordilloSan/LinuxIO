package docker

import (
	"context"
	"fmt"
	"sort"

	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
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

	result := make([]apischema.DockerImage, 0, len(imageItems))
	for _, item := range imageItems {
		result = append(result, dockerImageFromSummary(item))
	}
	return result, nil
}

func dockerImageFromSummary(item image.Summary) apischema.DockerImage {
	result := apischema.DockerImage{
		Created:     item.Created,
		ID:          item.ID,
		Labels:      item.Labels,
		RepoDigests: item.RepoDigests,
		RepoTags:    item.RepoTags,
		Size:        item.Size,
	}
	if item.Containers >= 0 {
		containers := int(item.Containers)
		result.Containers = &containers
	}
	if status, ok := imageUpdateStatusForImage(item); ok {
		result.UpdateAvailable = new(status.UpdateAvailable)
	}
	return result
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
