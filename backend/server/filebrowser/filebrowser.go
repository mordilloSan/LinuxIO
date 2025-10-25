package filebrowser

import (
	"archive/tar"
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"

	"github.com/mordilloSan/LinuxIO/backend/server/bridge/handlers/docker"
	"github.com/mordilloSan/go_logger/logger"
)

//go:embed filebrowserConfig.yaml
var DefaultFilebrowserConfig []byte

//go:embed custom.css
var EmbeddedCSS []byte

var (
	dockerCli *client.Client
	dockerCtx context.Context
	BaseURL   string // BaseURL is the discovered http base (e.g. "http://127.0.0.1:port") that the reverse proxy should forward to.
)

func StartServices(secret string, debug bool, dev bool) {
	logger.Debugf("Checking docker installation...")
	if err := docker.EnsureDockerAvailable(); err != nil {
		logger.Errorf(" Docker not available: %v", err)
	}

	var err error
	dockerCli, err = client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		logger.Errorf(" Failed to init Docker client: %v", err)
		return
	}
	dockerCtx = context.Background()

	// Ensure custom network exists (ignore if already there)
	if _, err := dockerCli.NetworkCreate(dockerCtx, "bridge-linuxio", network.CreateOptions{}); err != nil {
		if !isNetworkExistsError(err) {
			logger.Errorf("Failed to create Docker network: %v", err)
		} else {
			logger.Debugf("Docker network 'bridge-linuxio' already exists")
		}
	} else {
		logger.Infof("Created Docker network 'bridge-linuxio'")
	}

	if err := startFileBrowserContainer(secret, debug, dev); err != nil {
		logger.Errorf("FileBrowser setup failed: %v", err)
	}
}

func startFileBrowserContainer(secret string, debug bool, dev bool) error {
	const (
		baseName   = "filebrowser-linuxio"
		imageRef   = "docker.io/gtstef/filebrowser:latest"
		ctrCfgPath = "/home/filebrowser/config.yaml"
		ctrCSSPath = "/home/filebrowser/custom.css"
	)

	containerName := baseName
	if dev {
		containerName = baseName + "-dev"
	}

	// 0) Build config contents in-memory
	apiLevels := []byte(`warning|error`)
	if debug {
		apiLevels = []byte(`info|warning|error|debug`)
	}
	cfg := DefaultFilebrowserConfig
	cfg = bytes.ReplaceAll(cfg, []byte("{{SECRET_KEY}}"), []byte(secret))
	cfg = bytes.ReplaceAll(cfg, []byte("{{API_LEVELS}}"), apiLevels)

	// 1) Remove any existing container
	var err error
	containers, err := dockerCli.ContainerList(dockerCtx, container.ListOptions{All: true})
	if err != nil {
		return fmt.Errorf("list containers: %w", err)
	}
	for _, c := range containers {
		for _, name := range c.Names {
			if name == "/"+containerName {
				logger.Infof("Found existing '%s' (status: %s), removing...", containerName, c.State)
				if removalErr := dockerCli.ContainerRemove(dockerCtx, c.ID, container.RemoveOptions{Force: true}); removalErr != nil {
					return fmt.Errorf("remove existing container '%s': %w", containerName, removalErr)
				}
				logger.Infof("Removed container '%s'", containerName)
			}
		}
	}

	// 2) Pull image if needed
	rc, err := dockerCli.ImagePull(dockerCtx, imageRef, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()

	// 3) Create container — publish container 80 to a RANDOM localhost port
	resp, err := dockerCli.ContainerCreate(
		dockerCtx,
		&container.Config{
			Image: "gtstef/filebrowser",
			Labels: map[string]string{
				"io.linuxio.component": "filebrowser",
				"io.linuxio.mode":      map[bool]string{true: "development", false: "production"}[dev],
			},
		},
		&container.HostConfig{
			NetworkMode: container.NetworkMode("bridge-linuxio"),
			Mounts: []mount.Mount{
				{
					Type:     mount.TypeBind,
					Source:   "/",
					Target:   "/server",
					ReadOnly: false,
				},
			},
		},
		&network.NetworkingConfig{},
		nil,
		containerName,
	)
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	// 4) Stage config + CSS inside the container
	files := map[string][]byte{
		ctrCfgPath: cfg,
		ctrCSSPath: EmbeddedCSS,
	}
	if copyErr := copyFilesToContainer(dockerCtx, resp.ID, files); copyErr != nil {
		_ = dockerCli.ContainerRemove(dockerCtx, resp.ID, container.RemoveOptions{Force: true})
		return fmt.Errorf("copy config/css into container: %w", copyErr)
	}

	// 5) Start the container
	if startErr := dockerCli.ContainerStart(dockerCtx, resp.ID, container.StartOptions{}); startErr != nil {
		_ = dockerCli.ContainerRemove(dockerCtx, resp.ID, container.RemoveOptions{Force: true})
		return fmt.Errorf("start container: %w", startErr)
	}
	logger.Infof("Started FileBrowser container")

	// Get container's IP on the Docker network
	inspect, err := dockerCli.ContainerInspect(dockerCtx, resp.ID)
	if err != nil {
		return fmt.Errorf("inspect container: %w", err)
	}

	// Extract IP from the bridge-linuxio network
	netSettings, ok := inspect.NetworkSettings.Networks["bridge-linuxio"]
	if !ok || netSettings.IPAddress == "" {
		return fmt.Errorf("container not properly connected to bridge-linuxio network")
	}

	containerIP := netSettings.IPAddress
	baseCandidate := fmt.Sprintf("http://%s:80", containerIP)

	// Wait for readiness (same health check logic)
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	readyURL := baseCandidate + "/navigator/health"
	deadline := time.Now().Add(15 * time.Second)
	for {
		resp, err := client.Get(readyURL)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 400 {
				break
			}
		}
		if time.Now().After(deadline) {
			logger.Warnf("FileBrowser not healthy yet at %s; proceeding anyway", readyURL)
			break
		}
		time.Sleep(300 * time.Millisecond)
	}

	BaseURL = baseCandidate
	logger.Infof("FileBrowser accessible at %s (Docker network IP, no published ports)", BaseURL)

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

func isNetworkExistsError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return bytes.Contains([]byte(s), []byte("already exists")) || bytes.Contains([]byte(s), []byte("409"))
}
