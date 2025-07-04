package filebrowser

import (
	"bytes"
	"context"
	"fmt"
	embed "go-backend"
	"go-backend/internal/config"
	"go-backend/internal/logger"
	"go-backend/internal/session"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
)

var FilebrowserSecret string

var (
	dockerCli *client.Client
	dockerCtx context.Context
)

func StartServices(secret string, session *session.Session) {

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
	resp, err := dockerCli.NetworkCreate(dockerCtx, "bridge-linuxio", network.CreateOptions{})
	if err != nil {
		if isNetworkExistsError(err) {
			logger.Infof("Docker network 'bridge-linuxio' already exists")
		} else {
			logger.Errorf("Failed to create Docker network: %v", err)
		}
	} else {
		logger.Infof("✅ Created Docker network 'bridge-linuxio' (ID: %s, Warning: %s)", resp.ID, resp.Warning)
	}

	// Start FileBrowser container (microservice)
	if err := startFileBrowserContainer(secret, session); err != nil {
		logger.Errorf("FileBrowser setup failed: %v", err)
	}
}

func isNetworkExistsError(err error) bool {
	return err != nil && (bytes.Contains([]byte(err.Error()), []byte("already exists")) || bytes.Contains([]byte(err.Error()), []byte("409")))
}

// writeFilebrowserConfig replaces placeholder and writes the config file
func writeFilebrowserConfig(path string, rawContent []byte, secretKey string, session *session.Session) error {
	configStr := string(rawContent)
	configStr = strings.ReplaceAll(configStr, "{{SECRET_KEY}}", secretKey)
	configStr = strings.ReplaceAll(configStr, "{{USER_ID}}", session.User.ID)
	err := os.WriteFile(path, []byte(configStr), 0644)
	if err != nil {
		logger.Errorf("❌ Failed to write FileBrowser config to %s: %v", path, err)
		return err
	}
	logger.Infof("Wrote FileBrowser config with secret to %s", path)
	return nil
}

func startFileBrowserContainer(secret string, session *session.Session) error {
	containerName := "filebrowser-linuxio"
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
	// Get user's home directory from session user
	u, err := user.Lookup(session.User.Name)
	if err != nil {
		return fmt.Errorf("failed to lookup user %s: %w", session.User.Name, err)
	}
	// Set databasePath to user's home
	databasePath := filepath.Join(u.HomeDir, ".linuxio/filebrowser/data")
	if err := os.MkdirAll(databasePath, 0700); err != nil {
		return fmt.Errorf("failed to create filebrowser database dir: %w", err)
	}

	// 1. Write the embedded config before container starts
	if err := writeFilebrowserConfig(configPath, embed.DefaultFilebrowserConfig, secret, session); err != nil {
		return fmt.Errorf("failed to write embedded config: %w", err)
	}

	// 2. Remove any existing container (stopped or running)
	containers, err := dockerCli.ContainerList(dockerCtx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("failed to list Docker containers: %w", err)
	}
	for _, c := range containers {
		for _, name := range c.Names {
			if name == "/"+containerName {
				logger.Infof("Found existing container '%s' (status: %s), removing...", containerName, c.State)
				if err := dockerCli.ContainerRemove(dockerCtx, c.ID, container.RemoveOptions{
					Force: true,
				}); err != nil {
					return fmt.Errorf("failed to remove existing container '%s': %w", containerName, err)
				}
				logger.Infof("Removed container '%s'", containerName)
			}
		}
	}

	// 3. Pull image if not already present (docker will skip if present)
	out, err := dockerCli.ImagePull(dockerCtx, "docker.io/gtstef/filebrowser:latest", image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull FileBrowser image: %w", err)
	}
	defer func() {
		if cerr := out.Close(); cerr != nil {
			logger.Warnf("failed to close image pull stream: %v", cerr)
		}
	}()
	if _, err := io.Copy(io.Discard, out); err != nil {
		logger.Warnf("failed to drain docker image pull output: %v", err)
	}

	// 4. Create the container with the config mounted
	resp, err := dockerCli.ContainerCreate(
		dockerCtx,
		&container.Config{
			Image: "gtstef/filebrowser",
		},
		&container.HostConfig{
			NetworkMode: container.NetworkMode("bridge-linuxio"),
			Mounts: []mount.Mount{
				{
					Type:   mount.TypeBind,
					Source: configPath,
					Target: "/home/filebrowser/config.yaml",
				},
				{
					Type:   mount.TypeBind,
					Source: databasePath,
					Target: "/home/filebrowser/data",
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

	// 5. Start the container
	if err := dockerCli.ContainerStart(dockerCtx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("failed to start FileBrowser container: %w", err)
	} else {
		logger.Infof("Started FileBrowser container: %s", containerName)
	}

	// 6. Remove the config file from disk for security after container creation
	if err := os.Remove(configPath); err != nil {
		logger.Warnf("Could not remove temporary config file: %v", err)
	} else {
		logger.Debugf("Removed temporary config file: %s", configPath)
	}

	return nil
}
