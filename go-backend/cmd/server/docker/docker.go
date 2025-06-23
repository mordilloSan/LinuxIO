package docker

import (
	"bytes"
	"context"
	"fmt"
	embed "go-backend"
	"go-backend/internal/config"
	"go-backend/internal/logger"
	"io"
	"os"
	"path/filepath"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
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
	if err := startFileBrowserContainer(); err != nil {
		logger.Errorf("FileBrowser setup failed: %v", err)
	}
}

func isNetworkExistsError(err error) bool {
	return err != nil && (bytes.Contains([]byte(err.Error()), []byte("already exists")) || bytes.Contains([]byte(err.Error()), []byte("409")))
}

// Write embedded config file before container starts
func writeFilebrowserConfig(path string, content []byte) error {
	return os.WriteFile(path, content, 0644)
}

func startFileBrowserContainer() error {
	containerName := "filebrowser"
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		return fmt.Errorf("XDG_RUNTIME_DIR not set")
	}
	dir := filepath.Join(runtimeDir, "linuxio")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create runtime config dir: %w", err)
	}
	configPath := filepath.Join(dir, "filebrowser-config.yaml")

	serverPath := "/"

	// 1. Always write embedded config before container starts!
	if err := writeFilebrowserConfig(configPath, embed.DefaultFilebrowserConfig); err != nil {
		return fmt.Errorf("failed to write embedded config: %w", err)
	}

	// 1. Check if the container exists (stopped or running)
	containers, err := dockerCli.ContainerList(dockerCtx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list Docker containers: %w", err)
	}
	for _, c := range containers {
		for _, name := range c.Names {
			if name == "/"+containerName {
				logger.Infof("⚠️ Found existing container '%s' (status: %s), removing...", containerName, c.State)
				// Always remove the container
				if err := dockerCli.ContainerRemove(dockerCtx, c.ID, container.RemoveOptions{
					Force: true,
				}); err != nil {
					return fmt.Errorf("failed to remove existing container '%s': %w", containerName, err)
				}
				logger.Infof("🗑️  Removed container '%s'", containerName)
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
			PortBindings: nat.PortMap{
				"80/tcp": []nat.PortBinding{
					{HostIP: "127.0.0.1", HostPort: "8090"},
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

	logger.Infof("Created and started new FileBrowser Docker container")
	return nil
}
