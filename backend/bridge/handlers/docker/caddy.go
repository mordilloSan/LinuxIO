package docker

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
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

// GetCaddyStatus returns the current Caddy proxy status.
func GetCaddyStatus(ctx context.Context, username string, store *config.UserStore) (any, error) {
	cfg, _, err := config.SnapshotForUser(ctx, username, store)
	if err != nil {
		return nil, err
	}

	running := isCaddyRunning(ctx)
	routes, _ := buildRoutes(ctx, cfg.Docker.Proxy)

	return apischema.CaddyStatusResponse{
		Enabled:    cfg.Docker.Proxy.CaddyEnabled,
		BaseDomain: cfg.Docker.Proxy.BaseDomain,
		Running:    running,
		Routes:     routes,
	}, nil
}

// EnableCaddy deploys the Caddy container and generates the initial Caddyfile.
func EnableCaddy(ctx context.Context, username string, store *config.UserStore) (any, error) {
	if err := ensureCaddyDirs(); err != nil {
		return nil, fmt.Errorf("failed to create caddy config dirs: %w", err)
	}

	if err := deployCaddyContainer(ctx); err != nil {
		return nil, fmt.Errorf("failed to deploy caddy: %w", err)
	}

	cfg, _, err := config.UpdateForUser(ctx, username, store, func(cfg *config.Settings) error {
		cfg.Docker.Proxy.CaddyEnabled = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Give Caddy a moment to start before attempting first reload
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	_ = reloadCaddyfile(ctx, cfg.Docker.Proxy)

	return map[string]any{"message": "Caddy deployed"}, nil
}

// DisableCaddy stops and removes the Caddy container.
func DisableCaddy(ctx context.Context, username string, store *config.UserStore) (any, error) {
	if err := removeCaddyContainer(ctx); err != nil {
		slog.Warn("failed to remove caddy container", "component", "docker", "subsystem", "caddy", "error", err)
	}

	if _, _, err := config.UpdateForUser(ctx, username, store, func(cfg *config.Settings) error {
		cfg.Docker.Proxy.CaddyEnabled = false
		return nil
	}); err != nil {
		return nil, err
	}

	return map[string]any{"message": "Caddy removed"}, nil
}

// ReloadCaddy regenerates the Caddyfile from current containers and reloads Caddy.
func ReloadCaddy(ctx context.Context, username string, store *config.UserStore) (any, error) {
	cfg, _, err := config.SnapshotForUser(ctx, username, store)
	if err != nil {
		return nil, err
	}
	if err := reloadCaddyfile(ctx, cfg.Docker.Proxy); err != nil {
		return nil, err
	}
	return map[string]any{"message": "Caddy reloaded"}, nil
}

// ConnectToProxy attaches a container to linuxio-docker so Caddy can reach it.
func ConnectToProxy(ctx context.Context, containerID string) (any, error) {
	ConnectToProxyNetwork(ctx, containerID)
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

func deployCaddyContainer(ctx context.Context) error {
	cli, err := getClient()
	if err != nil {
		return err
	}
	defer releaseClient(cli)

	// Remove any existing (stopped) container first
	_, _ = cli.ContainerRemove(ctx, caddyContainerName, client.ContainerRemoveOptions{Force: true})

	// Pull image
	rc, pullErr := cli.ImagePull(ctx, caddyImage, client.ImagePullOptions{})
	if pullErr != nil {
		slog.Warn("failed to pull caddy image", "component", "docker", "subsystem", "caddy", "image", caddyImage, "error", pullErr)
	} else {
		_ = rc.Close()
	}

	portSet := network.PortSet{
		network.MustParsePort("80/tcp"):   struct{}{},
		network.MustParsePort("443/tcp"):  struct{}{},
		network.MustParsePort("2019/tcp"): struct{}{},
	}

	resp, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:        caddyImage,
			ExposedPorts: portSet,
			Labels: map[string]string{
				"io.linuxio.managed":         "true",
				"com.docker.compose.project": "linuxio",
				"io.linuxio.container.icon":  "di:caddy",
			},
			Cmd: []string{"caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"},
		},
		HostConfig: &container.HostConfig{
			PortBindings: network.PortMap{
				network.MustParsePort("80/tcp"): []network.PortBinding{{
					HostIP:   netip.MustParseAddr("0.0.0.0"),
					HostPort: "80",
				}},
				network.MustParsePort("443/tcp"): []network.PortBinding{{
					HostIP:   netip.MustParseAddr("0.0.0.0"),
					HostPort: "443",
				}},
				network.MustParsePort("2019/tcp"): []network.PortBinding{{
					HostIP:   netip.MustParseAddr("127.0.0.1"),
					HostPort: "2019",
				}},
			},
			Mounts: []mount.Mount{
				{Type: mount.TypeBind, Source: caddyConfigDir, Target: "/etc/caddy"},
				{Type: mount.TypeBind, Source: caddyDataDir, Target: "/data"},
			},
			RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
		},
		NetworkingConfig: &network.NetworkingConfig{
			EndpointsConfig: map[string]*network.EndpointSettings{
				linuxIONetworkName: {},
			},
		},
		Name: caddyContainerName,
	})
	if err != nil {
		return fmt.Errorf("create container: %w", err)
	}

	if _, err := cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start container: %w", err)
	}
	slog.Info("caddy container started", "component", "docker", "subsystem", "caddy", "container", resp.ID[:12])
	return nil
}

func removeCaddyContainer(ctx context.Context) error {
	cli, err := getClient()
	if err != nil {
		return err
	}
	defer releaseClient(cli)

	_, err = cli.ContainerRemove(ctx, caddyContainerName, client.ContainerRemoveOptions{Force: true})
	return err
}

func isCaddyRunning(ctx context.Context) bool {
	cli, err := getClient()
	if err != nil {
		return false
	}
	defer releaseClient(cli)

	list, err := cli.ContainerList(ctx, client.ContainerListOptions{
		Filters: client.Filters{}.Add("name", caddyContainerName),
	})
	if err != nil {
		return false
	}
	for _, c := range list.Items {
		if c.State == "running" {
			return true
		}
	}
	return false
}

// buildRoutes lists running containers with proxy labels and builds the route table.
func buildRoutes(ctx context.Context, proxyCfg config.DockerProxy) ([]apischema.CaddyRoute, error) {
	cli, err := getClient()
	if err != nil {
		return nil, err
	}
	defer releaseClient(cli)

	list, err := cli.ContainerList(ctx, client.ContainerListOptions{All: false})
	if err != nil {
		return nil, err
	}

	var routes []apischema.CaddyRoute
	for _, c := range list.Items {
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
		routes = append(routes, apischema.CaddyRoute{Host: host, Container: name, Port: port})
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
func reloadCaddyfile(ctx context.Context, proxyCfg config.DockerProxy) error {
	routes, err := buildRoutes(ctx, proxyCfg)
	if err != nil {
		return fmt.Errorf("build routes: %w", err)
	}

	caddyfile := generateCaddyfile(routes, proxyCfg)

	cfgPath := filepath.Join(caddyConfigDir, "Caddyfile")
	if err = os.WriteFile(cfgPath, []byte(caddyfile), 0o644); err != nil {
		return fmt.Errorf("write Caddyfile: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
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
	slog.Info("Caddyfile reloaded", "component", "docker", "subsystem", "caddy", "route_count", len(routes))
	return nil
}

// generateCaddyfile builds the Caddyfile text from routes.
func generateCaddyfile(routes []apischema.CaddyRoute, proxyCfg config.DockerProxy) string {
	var b strings.Builder

	b.WriteString("{\n")
	b.WriteString("\tadmin 0.0.0.0:2019\n")
	if proxyCfg.BaseDomain == "" {
		// Local-only: disable automatic HTTPS to avoid cert warnings on .localhost
		b.WriteString("\tauto_https off\n")
	}
	b.WriteString("}\n\n")

	for _, r := range routes {
		b.WriteString(r.Host)
		b.WriteString(" {\n")
		fmt.Fprintf(&b, "\treverse_proxy %s:%s\n", r.Container, r.Port)
		if proxyCfg.BaseDomain != "" && proxyCfg.TLSEmail != "" {
			fmt.Fprintf(&b, "\ttls %s\n", proxyCfg.TLSEmail)
		}
		b.WriteString("}\n\n")
	}

	return b.String()
}
