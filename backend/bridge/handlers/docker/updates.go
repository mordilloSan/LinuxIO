package docker

import (
	"context"
	"fmt"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dockerclient"
	"github.com/mordilloSan/LinuxIO/backend/common/dockerupdate"
)

// RefreshDockerImageUpdates checks all running containers against their
// configured registry references and replaces the persistent status snapshot.
func RefreshDockerImageUpdates(ctx context.Context) (apischema.DockerUpdateCheckResult, error) {
	cli, err := dockerclient.Get()
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("docker client error: %w", err)
	}
	defer dockerclient.Release(cli)
	return dockerupdate.RefreshImageUpdates(ctx, cli)
}

// RefreshContainerImageUpdate checks one container against its configured
// registry reference and refreshes only that container's status entry.
func RefreshContainerImageUpdate(ctx context.Context, containerID string) (apischema.DockerUpdateCheckResult, error) {
	cli, err := dockerclient.Get()
	if err != nil {
		return apischema.DockerUpdateCheckResult{}, fmt.Errorf("docker client error: %w", err)
	}
	defer dockerclient.Release(cli)
	return dockerupdate.RefreshContainerUpdate(ctx, cli, containerID)
}

// GetContainerAutoUpdate reports the scheduled-update configuration and timer state.
func GetContainerAutoUpdate(ctx context.Context) (apischema.DockerContainerAutoUpdateState, error) {
	cli, err := dockerclient.Get()
	if err != nil {
		return apischema.DockerContainerAutoUpdateState{}, fmt.Errorf("docker client error: %w", err)
	}
	defer dockerclient.Release(cli)
	return dockerupdate.GetAutoUpdate(ctx, cli)
}

// SetContainerAutoUpdate persists the scheduled-update configuration and
// enables or disables its timer.
func SetContainerAutoUpdate(ctx context.Context, opts apischema.DockerContainerAutoUpdateOptions) (apischema.DockerContainerAutoUpdateState, error) {
	cli, err := dockerclient.Get()
	if err != nil {
		return apischema.DockerContainerAutoUpdateState{}, fmt.Errorf("docker client error: %w", err)
	}
	defer dockerclient.Release(cli)
	return dockerupdate.SetAutoUpdate(ctx, cli, opts)
}

func applyContainerUpdateStatus(info *apischema.ContainerInfo, snap dockerupdate.StatusSnapshot) {
	if info == nil {
		return
	}
	if status, ok := snap.ForContainer(info.ID); ok {
		setContainerUpdateStatus(info, status)
		return
	}
	if status, ok := snap.ForContainerName(containerInfoPrimaryName(*info)); ok {
		setContainerUpdateStatus(info, status)
	}
}

func setContainerUpdateStatus(info *apischema.ContainerInfo, status dockerupdate.ImageUpdateStatus) {
	checkState := status.CheckState
	info.UpdateCheckState = new(checkState)
	info.UpdateCheckReason = apischema.Optional(status.CheckReason)
	info.UpdateAvailable = nil
	if checkState != apischema.DockerUpdateCheckStateUncheckable {
		info.UpdateAvailable = new(status.UpdateAvailable)
	}
	info.UpdateCheckedAt = new(status.CheckedAt.UnixMilli())
	info.UpdateError = apischema.Optional(status.Err)
}

func containerInfoPrimaryName(info apischema.ContainerInfo) string {
	if len(info.Names) == 0 {
		return ""
	}
	return strings.TrimPrefix(info.Names[0], "/")
}
