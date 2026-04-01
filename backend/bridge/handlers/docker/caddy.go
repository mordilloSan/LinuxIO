package docker

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/strslice"
	"github.com/docker/go-connections/nat"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/go-logger/logger"
)

const (
	caddyImage          = "caddy:2-alpine"
	caddyContainerName  = "linuxio-caddy"
	caddyAdminURL       = "http://localhost:2019/load"
	caddyConfigDir      = "/run/linuxio/caddy"
	caddyDataDir        = "/run/linuxio/caddy/data"
	ProxyPortLabel      = "io.linuxio.container.proxy.port"
	proxySubdomainLabel = "io.linuxio.container.proxy.subdomain"
	proxyEnabledLabel   = "io.linuxio.container.proxy.enabled"
)

// CaddyRoute describes one proxied host.
type CaddyRoute struct {
	Host      string `json:"host"`
	Container string `json:"container"`
	Port      string `json:"port"`
}

// CaddyStatus is returned by GetCaddyStatus.
type CaddyStatus struct {
	Enabled    bool         `json:"enabled"`
	BaseDomain string       `json:"baseDomain"`
	Running    bool         `json:"running"`
	Routes     []CaddyRoute `json:"routes"`
}

// GetCaddyStatus returns the current Caddy proxy status.
func GetCaddyStatus(username string) (any, error) {
	cfg, _, err := config.Load(username)
	if err != nil {
		return nil, err
	}

	running := isCaddyRunning()
	routes, _ := buildRoutes(cfg.Docker.Proxy)

	return CaddyStatus{
		Enabled:    cfg.Docker.Proxy.CaddyEnabled,
		BaseDomain: cfg.Docker.Proxy.BaseDomain,
		Running:    running,
		Routes:     routes,
	}, nil
}

// EnableCaddy deploys the Caddy container and generates the initial Caddyfile.
func EnableCaddy(username string) (any, error) {
	cfg, _, err := config.Load(username)
	if err != nil {
		return nil, err
	}

	if err := ensureCaddyDirs(); err != nil {
		return nil, fmt.Errorf("failed to create caddy config dirs: %w", err)
	}

	if err := deployCaddyContainer(); err != nil {
		return nil, fmt.Errorf("failed to deploy caddy: %w", err)
	}

	cfg.Docker.Proxy.CaddyEnabled = true
	if _, err := config.Save(username, cfg); err != nil {
		return nil, err
	}

	// Give Caddy a moment to start before attempting first reload
	time.Sleep(2 * time.Second)
	_ = reloadCaddyfile(cfg.Docker.Proxy)

	return map[string]any{"message": "Caddy deployed"}, nil
}

// DisableCaddy stops and removes the Caddy container.
func DisableCaddy(username string) (any, error) {
	cfg, _, err := config.Load(username)
	if err != nil {
		return nil, err
	}

	if err := removeCaddyContainer(); err != nil {
		logger.Warnf("remove error: %v", err)
	}

	cfg.Docker.Proxy.CaddyEnabled = false
	if _, err := config.Save(username, cfg); err != nil {
		return nil, err
	}

	return map[string]any{"message": "Caddy removed"}, nil
}

// ReloadCaddy regenerates the Caddyfile from current containers and reloads Caddy.
func ReloadCaddy(username string) (any, error) {
	cfg, _, err := config.Load(username)
	if err != nil {
		return nil, err
	}
	if err := reloadCaddyfile(cfg.Docker.Proxy); err != nil {
		return nil, err
	}
	return map[string]any{"message": "Caddy reloaded"}, nil
}

// ConnectToProxy attaches a container to linuxio-docker so Caddy can reach it.
func ConnectToProxy(containerID string) (any, error) {
	ConnectToProxyNetwork(containerID)
	return map[string]any{"message": "connected"}, nil
}

// ── internal helpers ──────────────────────────────────────────────────────────

func ensureCaddyDirs() error {
	for _, dir := range []string{caddyConfigDir, caddyDataDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func deployCaddyContainer() error {
	cli, err := getClient()
	if err != nil {
		return err
	}
	defer releaseClient(cli)

	ctx := context.Background()

	// Remove any existing (stopped) container first
	_ = cli.ContainerRemove(ctx, caddyContainerName, container.RemoveOptions{Force: true})

	// Pull image
	rc, pullErr := cli.ImagePull(ctx, caddyImage, image.PullOptions{})
	if pullErr != nil {
		logger.Warnf("pull warning: %v", pullErr)
	} else {
		_ = rc.Close()
	}

	portSet := nat.PortSet{
		"80/tcp":   struct{}{},
		"443/tcp":  struct{}{},
		"2019/tcp": struct{}{},
	}

	resp, err := cli.ContainerCreate(ctx,
		&container.Config{
			Image:        caddyImage,
			ExposedPorts: portSet,
			Labels: map[string]string{
				"io.linuxio.managed":         "true",
				"com.docker.compose.project": "linuxio",
				"io.linuxio.container.icon":  "di:caddy",
			},
			Cmd: strslice.StrSlice{"caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"},
		},
		&container.HostConfig{
			PortBindings: nat.PortMap{
				"80/tcp":   []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "80"}},
				"443/tcp":  []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "443"}},
				"2019/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "2019"}},
			},
			Mounts: []mount.Mount{
				{Type: mount.TypeBind, Source: caddyConfigDir, Target: "/etc/caddy"},
				{Type: mount.TypeBind, Source: caddyDataDir, Target: "/data"},
			},
			RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		},
		&network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				linuxIONetworkName: {},
			},
		},
		nil,
		caddyContainerName,
	)
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	logger.Infof("container started: %s", resp.ID[:12])
	return nil
}

func removeCaddyContainer() error {
	cli, err := getClient()
	if err != nil {
		return err
	}
	defer releaseClient(cli)

	return cli.ContainerRemove(context.Background(), caddyContainerName, container.RemoveOptions{Force: true})
}

func isCaddyRunning() bool {
	cli, err := getClient()
	if err != nil {
		return false
	}
	defer releaseClient(cli)

	list, err := cli.ContainerList(context.Background(), container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", caddyContainerName)),
	})
	if err != nil {
		return false
	}
	for _, c := range list {
		if c.State == "running" {
			return true
		}
	}
	return false
}

// buildRoutes lists running containers with proxy labels and builds the route table.
func buildRoutes(proxyCfg config.DockerProxy) ([]CaddyRoute, error) {
	cli, err := getClient()
	if err != nil {
		return nil, err
	}
	defer releaseClient(cli)

	list, err := cli.ContainerList(context.Background(), container.ListOptions{All: false})
	if err != nil {
		return nil, err
	}

	var routes []CaddyRoute
	for _, c := range list {
		port := c.Labels[ProxyPortLabel]
		if port == "" {
			continue
		}
		if c.Labels[proxyEnabledLabel] == "false" {
			continue
		}

		name := caddyContainerShortName(c)
		subdomain := c.Labels[proxySubdomainLabel]
		if subdomain == "" {
			subdomain = name
		}

		host := buildHost(subdomain, proxyCfg.BaseDomain)
		routes = append(routes, CaddyRoute{Host: host, Container: name, Port: port})
	}
	return routes, nil
}

// buildHost returns the full hostname for a subdomain given the base domain config.
func buildHost(subdomain, baseDomain string) string {
	if baseDomain == "" {
		return subdomain + ".localhost"
	}
	return subdomain + "." + strings.TrimPrefix(baseDomain, ".")
}

// caddyContainerShortName returns the best human-readable name for routing.
func caddyContainerShortName(c container.Summary) string {
	serviceName := c.Labels["com.docker.compose.service"]
	projectName := c.Labels["com.docker.compose.project"]
	if len(c.Names) == 0 {
		if serviceName != "" {
			return serviceName
		}
		return c.ID[:12]
	}
	containerName := strings.TrimPrefix(c.Names[0], "/")
	if serviceName != "" && projectName != "" {
		if strings.HasPrefix(containerName, projectName+"-"+serviceName+"-") {
			return serviceName
		}
	}
	return containerName
}

// reloadCaddyfile generates the Caddyfile and reloads Caddy via its Admin API.
func reloadCaddyfile(proxyCfg config.DockerProxy) error {
	routes, err := buildRoutes(proxyCfg)
	if err != nil {
		return fmt.Errorf("build routes: %w", err)
	}

	caddyfile := generateCaddyfile(routes, proxyCfg)

	cfgPath := filepath.Join(caddyConfigDir, "Caddyfile")
	if err = os.WriteFile(cfgPath, []byte(caddyfile), 0o644); err != nil {
		return fmt.Errorf("write Caddyfile: %w", err)
	}

	req, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		caddyAdminURL,
		bytes.NewReader([]byte(caddyfile)),
	)
	if err != nil {
		return fmt.Errorf("build reload request: %w", err)
	}
	req.Header.Set("Content-Type", "text/caddyfile")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("caddy admin API unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("caddy reload returned %s", resp.Status)
	}

	logger.Infof("Caddyfile reloaded (%d routes)", len(routes))
	return nil
}

// generateCaddyfile builds the Caddyfile text from routes.
func generateCaddyfile(routes []CaddyRoute, proxyCfg config.DockerProxy) string {
	var b strings.Builder

	b.WriteString("{\n")
	b.WriteString("\tadmin 0.0.0.0:2019\n")
	if proxyCfg.BaseDomain == "" {
		// Local-only: disable automatic HTTPS to avoid cert warnings on .localhost
		b.WriteString("\tauto_https off\n")
	}
	b.WriteString("}\n\n")

	for _, r := range routes {
		b.WriteString(r.Host + " {\n")
		fmt.Fprintf(&b, "\treverse_proxy %s:%s\n", r.Container, r.Port)
		if proxyCfg.BaseDomain != "" && proxyCfg.TLSEmail != "" {
			fmt.Fprintf(&b, "\ttls %s\n", proxyCfg.TLSEmail)
		}
		b.WriteString("}\n\n")
	}

	return b.String()
}
