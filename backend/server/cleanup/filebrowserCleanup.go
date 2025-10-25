package cleanup

import (
	"context"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/mordilloSan/go_logger/logger"
)

func CleanupFilebrowserContainer(dev bool) {
	const baseName = "filebrowser-linuxio"

	containerName := baseName
	if dev {
		containerName = baseName + "-dev"
	}

	// Add leading slash for Docker API
	containerNameWithSlash := "/" + containerName
	timeout := 0 // seconds

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logger.Warnf("Failed to create Docker client: %v", err)
		return
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	if err := cli.ContainerStop(context.Background(), containerNameWithSlash, container.StopOptions{Timeout: &timeout}); err != nil {
		if errdefs.IsNotFound(err) {
			logger.Infof("Container %s was not running.", containerName)
		} else {
			logger.Warnf("Failed to stop container %s: %v", containerName, err)
		}
	} else {
		logger.Debugf("Stopped FileBrowser container: %s", containerName)
	}

	if err := cli.ContainerRemove(context.Background(), containerNameWithSlash, container.RemoveOptions{Force: true}); err != nil {
		if errdefs.IsNotFound(err) {
			logger.Infof("Container %s already removed.", containerName)
		} else {
			logger.Warnf("Failed to remove container %s: %v", containerName, err)
		}
	} else {
		logger.Infof("Removed FileBrowser container: %s", containerName)
	}
}
