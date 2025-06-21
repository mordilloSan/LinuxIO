package docker

import (
	"bytes"
	"context"
	"fmt"
	embed "go-backend"
	"go-backend/internal/config"
	"go-backend/internal/logger"
	"go-backend/internal/utils"
	"io"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

var (
	dockerCli *client.Client
	dockerCtx context.Context
)

func StartServices() {

	logger.Infof("📦 Checking docker installation...")
	if err := config.EnsureDockerAvailable(); err != nil {
		logger.Errorf("❌ Docker not available: %v", err)
	}

	var err error
	dockerCli, err = client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logger.Errorf("❌ Failed to init Docker client: %v", err)
		return
	}
	dockerCtx = context.Background() // Or use context.WithTimeout()

	// Ensure custom Docker network exists (ignore error if already exists)
	resp, err := dockerCli.NetworkCreate(dockerCtx, "linuxio-net", network.CreateOptions{})
	if err != nil {
		if isNetworkExistsError(err) {
			logger.Infof("Docker network 'linuxio-net' already exists")
		} else {
			logger.Errorf("Failed to create Docker network: %v", err)
		}
	} else {
		logger.Infof("✅ Created Docker network 'linuxio-net' (ID: %s, Warning: %s)", resp.ID, resp.Warning)
	}

	// Start FileBrowser container (microservice)

	if err := utils.EnsureDefaultFile("/etc/linuxio/filebrowserConfig.yaml", embed.DefaultFilebrowserConfig); err != nil {
		logger.Errorf("FileBrowser setup failed: %v", err)
	}
	if err := startFileBrowserContainer(); err != nil {
		logger.Errorf("FileBrowser setup failed: %v", err)
	}
}

func isNetworkExistsError(err error) bool {
	return err != nil && (bytes.Contains([]byte(err.Error()), []byte("already exists")) || bytes.Contains([]byte(err.Error()), []byte("409")))
}

func startFileBrowserContainer() error {
	containerName := "filebrowser"
	configPath := "/etc/linuxio/filebrowserConfig.yaml"
	serverPath := "/"

	// 1. Check if the container exists (stopped or running)
	containers, err := dockerCli.ContainerList(dockerCtx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list Docker containers: %w", err)
	}
	for _, c := range containers {
		for _, name := range c.Names {
			if name == "/"+containerName {
				if c.State == "running" {
					logger.Infof("Docker container '%s' is already running", containerName)
					return nil // Already running, do nothing
				}
				// Exists but stopped: try to start
				if err := dockerCli.ContainerStart(dockerCtx, c.ID, container.StartOptions{}); err != nil {
					return fmt.Errorf("failed to start existing FileBrowser container: %w", err)
				}
				logger.Infof("Started existing Docker container '%s'", containerName)
				return nil
			}
		}
	}

	// 2. Pull image if not already present (docker will skip if present)
	out, err := dockerCli.ImagePull(dockerCtx, "docker.io/gtstef/filebrowser:latest", image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull FileBrowser image: %w", err)
	}
	defer out.Close()
	io.Copy(io.Discard, out) // Always drain!

	// 3. Create the container
	resp, err := dockerCli.ContainerCreate(
		dockerCtx,
		&container.Config{
			Image: "gtstef/filebrowser",
		},
		&container.HostConfig{
			NetworkMode: container.NetworkMode("linuxio-net"),
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeBind,
					Source: configPath,
					Target: "/home/filebrowser/config.yaml",
				},
				{
					Type:   mount.TypeBind,
					Source: serverPath,
					Target: "/server",
				},
			},
		},
		&network.NetworkingConfig{},
		nil,
		containerName,
	)
	if err != nil {
		return fmt.Errorf("failed to create FileBrowser container: %w", err)
	}

	// 4. Start the container
	if err := dockerCli.ContainerStart(dockerCtx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start FileBrowser container: %w", err)
	}

	logger.Infof("Created and started new FileBrowser Docker container '%s'", containerName)
	return nil
}
