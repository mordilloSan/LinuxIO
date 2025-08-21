package filebrowser

import (
	"archive/tar"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/mordilloSan/LinuxIO/cmd/server/docker"
	"github.com/mordilloSan/LinuxIO/internal/logger"
)

//go:embed filebrowserConfig.yaml
var DefaultFilebrowserConfig []byte

//go:embed custom.css
var EmbeddedCSS []byte

var (
	dockerCli *client.Client
	dockerCtx context.Context
)

func StartServices(secret string) {
	logger.Infof("📦 Checking docker installation...")
	if err := docker.EnsureDockerAvailable(); err != nil {
		logger.Errorf("❌ Docker not available: %v", err)
	}

	var err error
	dockerCli, err = client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logger.Errorf("❌ Failed to init Docker client: %v", err)
		return
	}
	dockerCtx = context.Background()

	// Ensure custom network exists (ignore if already there)
	if _, err := dockerCli.NetworkCreate(dockerCtx, "bridge-linuxio", network.CreateOptions{}); err != nil {
		if !isNetworkExistsError(err) {
			logger.Errorf("Failed to create Docker network: %v", err)
		} else {
			logger.Infof("Docker network 'bridge-linuxio' already exists")
		}
	} else {
		logger.Infof("✅ Created Docker network 'bridge-linuxio'")
	}

	if err := startFileBrowserContainer(secret); err != nil {
		logger.Errorf("FileBrowser setup failed: %v", err)
	}
}

func isNetworkExistsError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return bytes.Contains([]byte(s), []byte("already exists")) || bytes.Contains([]byte(s), []byte("409"))
}

func startFileBrowserContainer(secret string) error {
	const (
		containerName = "filebrowser-linuxio"
		imageRef      = "docker.io/gtstef/filebrowser:latest"
		ctrCfgPath    = "/home/filebrowser/config.yaml"
		ctrCSSPath    = "/home/filebrowser/custom.css"
	)

	// 0) Build config contents in-memory
	cfg := bytes.ReplaceAll(DefaultFilebrowserConfig, []byte("{{SECRET_KEY}}"), []byte(secret))

	// 1) Remove any existing container
	var err error
	var containers []container.Summary
	containers, err = dockerCli.ContainerList(dockerCtx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	for _, c := range containers {
		for _, name := range c.Names {
			if name == "/"+containerName {
				logger.Infof("Found existing '%s' (status: %s), removing...", containerName, c.State)
				err = dockerCli.ContainerRemove(dockerCtx, c.ID, container.RemoveOptions{Force: true})
				if err != nil {
					return fmt.Errorf("remove existing container '%s': %w", containerName, err)
				}
				logger.Infof("Removed container '%s'", containerName)
			}
		}
	}

	// 2) Pull image if needed
	var rc io.ReadCloser
	rc, err = dockerCli.ImagePull(dockerCtx, imageRef, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()

	// 3) Create container (no host bind mounts for cfg/css)
	var resp container.CreateResponse
	resp, err = dockerCli.ContainerCreate(
		dockerCtx,
		&container.Config{
			Image: "gtstef/filebrowser",
			Healthcheck: &container.HealthConfig{
				Test:     []string{"CMD-SHELL", "wget --spider -q http://localhost:80/navigator/health || exit 1"},
				Interval: 5 * time.Second,
				Timeout:  10 * time.Second,
				Retries:  3,
			},
		},
		&container.HostConfig{
			NetworkMode: container.NetworkMode("bridge-linuxio"),
			Mounts: []mount.Mount{
				{
					Type:     mount.TypeBind,
					Source:   "/",
					Target:   "/server",
					ReadOnly: true,
				},
			},
			PortBindings: nat.PortMap{
				"80/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "8090"}},
			},
		},
		&network.NetworkingConfig{},
		nil,
		containerName,
	)
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	// 4) Stage config + CSS inside the container filesystem (no host temp files)
	files := map[string][]byte{
		ctrCfgPath: cfg,
		ctrCSSPath: EmbeddedCSS,
	}
	err = copyFilesToContainer(dockerCtx, resp.ID, files)
	if err != nil {
		// best-effort cleanup
		_ = dockerCli.ContainerRemove(dockerCtx, resp.ID, container.RemoveOptions{Force: true})
		return fmt.Errorf("copy config/css into container: %w", err)
	}

	// 5) Start the container
	err = dockerCli.ContainerStart(dockerCtx, resp.ID, container.StartOptions{})
	if err != nil {
		_ = dockerCli.ContainerRemove(dockerCtx, resp.ID, container.RemoveOptions{Force: true})
		return fmt.Errorf("start container: %w", err)
	}
	logger.Infof("Started FileBrowser container: %s", containerName)

	return nil
}

// copyFilesToContainer packs files into a tar and copies them to the container root.
// Keys must be absolute container paths like "/home/filebrowser/config.yaml".
func copyFilesToContainer(ctx context.Context, containerID string, files map[string][]byte) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	// ensure parent dir entry exists in the tar (handles /home/filebrowser)
	addDir := func(path string) error {
		if path == "/" || path == "" {
			return nil
		}
		hdr := &tar.Header{
			Name:     strings.TrimPrefix(path, "/") + "/",
			Typeflag: tar.TypeDir,
			Mode:     0o755,
			ModTime:  time.Now(),
		}
		return tw.WriteHeader(hdr)
	}

	dirsAdded := map[string]bool{}
	for p := range files {
		// add parent dirs once
		dir := p
		for {
			dir = parentDir(dir)
			if dir == "" || dir == "/" {
				break
			}
			if !dirsAdded[dir] {
				if err := addDir(dir); err != nil {
					_ = tw.Close()
					return err
				}
				dirsAdded[dir] = true
			}
		}
	}

	for p, content := range files {
		h := &tar.Header{
			Name:    strings.TrimPrefix(p, "/"),
			Mode:    0o644,
			Size:    int64(len(content)),
			ModTime: time.Now(),
		}
		if err := tw.WriteHeader(h); err != nil {
			_ = tw.Close()
			return err
		}
		if _, err := tw.Write(content); err != nil {
			_ = tw.Close()
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}

	return dockerCli.CopyToContainer(ctx, containerID, "/", &buf, container.CopyToContainerOptions{
		AllowOverwriteDirWithFile: true,
	})
}

func parentDir(p string) string {
	if p == "" || p == "/" {
		return ""
	}
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		if i == 0 {
			return "/"
		}
		return p[:i]
	}
	return ""
}
