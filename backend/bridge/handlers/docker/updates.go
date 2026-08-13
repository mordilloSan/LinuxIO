package docker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// RefreshDockerImageUpdates checks all running containers against their
// configured registry references and replaces the persistent status snapshot.
func RefreshDockerImageUpdates(ctx context.Context) (apischema.DockerUpdateCheckResult, error) {
	cli, err := getClient()
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{All: false})
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("failed to list containers: %w", err)
	}
	targets := make([]containerImageUpdateTarget, 0, len(containers.Items))
	for _, ctr := range containers.Items {
		targets = append(targets, containerImageUpdateTarget{
			ContainerID:   ctr.ID,
			ContainerName: primaryContainerName(ctr),
			ImageID:       ctr.ImageID,
			ImageRef:      ctr.Image,
		})
	}

	statuses, result, err := checkContainerImageUpdates(ctx, cli, targets, time.Now())
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, err
	}
	if err := writeUpdateStatuses(ctx, statuses); err != nil {
		return apischema.DockerUpdateCheckResult{}, err
	}
	return result, nil
}

// RefreshContainerImageUpdate checks one container against its configured
// registry reference and refreshes only that container's status entry.
func RefreshContainerImageUpdate(ctx context.Context, containerID string) (apischema.DockerUpdateCheckResult, error) {
	cli, err := getClient()
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	inspectResult, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("inspect container: %w", err)
	}
	inspect := inspectResult.Container
	name := strings.TrimPrefix(inspect.Name, "/")
	if name == "" {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("container %s has no name", inspect.ID)
	}

	imageRef := ""
	if inspect.Config != nil {
		imageRef = inspect.Config.Image
	}
	statuses, result, err := checkContainerImageUpdates(
		ctx,
		cli,
		[]containerImageUpdateTarget{{
			ContainerID:   inspect.ID,
			ContainerName: name,
			ImageID:       inspect.Image,
			ImageRef:      imageRef,
		}},
		time.Now(),
	)
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, err
	}
	if err := mergeUpdateStatuses(ctx, statuses); err != nil {
		return apischema.DockerUpdateCheckResult{}, err
	}
	return result, nil
}

func updateContainer(ctx context.Context, containerID string) (apischema.DockerContainerUpdateResult, error) {
	release, err := acquireDockerUpdateLock(ctx)
	if err != nil {
		return apischema.DockerContainerUpdateResult{}, err
	}
	defer release()

	cli, err := getClient()
	if err != nil {
		return apischema.DockerContainerUpdateResult{}, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)
	if recoveryErr := recoverStandaloneUpdate(ctx, cli, defaultStandaloneUpdateJournal); recoveryErr != nil {
		return apischema.DockerContainerUpdateResult{}, fmt.Errorf("recover previous standalone Docker update: %w", recoveryErr)
	}

	inspectResult, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return apischema.DockerContainerUpdateResult{}, fmt.Errorf("inspect container: %w", err)
	}
	return updateInspectedContainer(ctx, cli, inspectResult.Container)
}
