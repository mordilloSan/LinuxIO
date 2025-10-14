package cleanup

import (
	"context"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"

	"github.com/mordilloSan/LinuxIO/backend/common/logger"
)

func CleanupFilebrowserContainer() {
	containerName := "/filebrowser-linuxio"
	timeout := 0 // seconds

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logger.Warnf("Failed to create Docker client: %v", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	if err := cli.ContainerStop(context.Background(), containerName, container.StopOptions{Timeout: &timeout}); err != nil {
		if errdefs.IsNotFound(err) {
			logger.Infof("Container %s was not running.", containerName)
		} else {
			logger.Warnf("Failed to stop container %s: %v", containerName, err)
		}
	} else {
		logger.Debugf("Stopped FileBrowser container: %s", containerName)
	}

	if err := cli.ContainerRemove(context.Background(), containerName, container.RemoveOptions{Force: true}); err != nil {
		if errdefs.IsNotFound(err) {
			logger.Infof("Container %s already removed.", containerName)
		} else {
			logger.Warnf("Failed to remove container %s: %v", containerName, err)
		}
	} else {
		logger.Infof("Removed FileBrowser container")
		logger.Debugf("Removed : %s", containerName)
	}

}
