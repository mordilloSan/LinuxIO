package docker

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/mordilloSan/go-logger/logger"
)

const (
	monitoringProjectName = "linuxio-monitoring"
	monitoringGlobalDir   = "/var/lib/linuxIO/monitoring"
	monitoringComposePath = "/var/lib/linuxIO/monitoring/docker-compose.yml"
	monitoringProxyURL    = "/proxy/grafana/"
)

type MonitoringServiceStatus struct {
	Name    string `json:"name"`
	State   string `json:"state"`
	Status  string `json:"status"`
	Running bool   `json:"running"`
}

type MonitoringStackStatus struct {
	Enabled  bool                      `json:"enabled"`
	Running  bool                      `json:"running"`
	URL      string                    `json:"url"`
	Services []MonitoringServiceStatus `json:"services"`
}

func EnableMonitoringStack() (map[string]any, error) {
	if err := ensureMonitoringStackFiles(); err != nil {
		return nil, err
	}

	if err := composeUpWithSDK(context.Background(), monitoringProjectName, monitoringComposePath, monitoringGlobalDir, true, nil); err != nil {
		return nil, fmt.Errorf("compose up failed: %w", err)
	}

	return map[string]any{
		"message": "Monitoring stack enabled",
		"url":     monitoringProxyURL,
	}, nil
}

func DisableMonitoringStack() (map[string]any, error) {
	if _, err := os.Stat(monitoringComposePath); os.IsNotExist(err) {
		return map[string]any{"message": "Monitoring stack already disabled"}, nil
	}

	if err := composeDownWithSDK(context.Background(), monitoringProjectName, monitoringComposePath, monitoringGlobalDir, false, nil); err != nil {
		return nil, fmt.Errorf("compose down failed: %w", err)
	}

	if err := os.Remove(monitoringComposePath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove compose file: %w", err)
	}

	return map[string]any{"message": "Monitoring stack disabled"}, nil
}

func ReloadMonitoringStack() (map[string]any, error) {
	if err := ensureMonitoringStackFiles(); err != nil {
		return nil, err
	}

	if err := composeUpWithSDK(context.Background(), monitoringProjectName, monitoringComposePath, monitoringGlobalDir, true, nil); err != nil {
		return nil, fmt.Errorf("compose up failed: %w", err)
	}

	return map[string]any{
		"message": "Monitoring stack reloaded",
		"url":     monitoringProxyURL,
	}, nil
}

func GetMonitoringStackStatus() (*MonitoringStackStatus, error) {
	status := &MonitoringStackStatus{
		URL: monitoringProxyURL,
	}

	if _, err := os.Stat(monitoringComposePath); err == nil {
		status.Enabled = true
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat compose file: %w", err)
	}

	services, err := listMonitoringServices()
	if err != nil {
		return nil, err
	}
	status.Services = services

	for _, svc := range services {
		if svc.Running {
			status.Running = true
			break
		}
	}

	return status, nil
}

func ensureMonitoringStackFiles() error {
	EnsureLinuxIONetwork()

	if err := os.MkdirAll(monitoringGlobalDir, 0o755); err != nil {
		return fmt.Errorf("create monitoring directory: %w", err)
	}

	content := generateMonitoringCompose()
	if err := os.WriteFile(monitoringComposePath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write monitoring compose file: %w", err)
	}

	return nil
}

func listMonitoringServices() ([]MonitoringServiceStatus, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "com.docker.compose.project="+monitoringProjectName)),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list monitoring containers: %w", err)
	}

	services := make([]MonitoringServiceStatus, 0, len(containers))
	for _, ctr := range containers {
		name := ctr.Labels["com.docker.compose.service"]
		if name == "" && len(ctr.Names) > 0 {
			name = strings.TrimPrefix(ctr.Names[0], "/")
		}
		services = append(services, MonitoringServiceStatus{
			Name:    name,
			State:   ctr.State,
			Status:  ctr.Status,
			Running: ctr.State == "running",
		})
	}

	sort.Slice(services, func(i, j int) bool {
		return services[i].Name < services[j].Name
	})

	return services, nil
}

func generateMonitoringCompose() string {
	return `x-linuxio-stack:
  icon: "si:prometheus"

services:
  prometheus:
    image: prom/prometheus:latest
    container_name: linuxio-prometheus
    restart: unless-stopped
    mem_limit: 512m
    mem_reservation: 128m
    command:
      - --config.file=/etc/prometheus/prometheus.yml
      - --storage.tsdb.path=/prometheus
      - --storage.tsdb.retention.time=15d
      - --web.enable-lifecycle
    configs:
      - source: prometheus_yml
        target: /etc/prometheus/prometheus.yml
    volumes:
      - prometheus_data:/prometheus
    depends_on:
      cadvisor:
        condition: service_healthy
      node-exporter:
        condition: service_healthy
    networks:
      - linuxio-docker
    extra_hosts:
      - "host.docker.internal:host-gateway"
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:9090/-/healthy"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 15s
    labels:
      - "io.linuxio.container.icon=si:prometheus"

  cadvisor:
    image: ghcr.io/google/cadvisor:latest
    container_name: linuxio-cadvisor
    restart: unless-stopped
    mem_limit: 256m
    mem_reservation: 64m
    privileged: true
    devices:
      - /dev/kmsg
    volumes:
      - /:/rootfs:ro
      - /var/run:/var/run:ro
      - /sys:/sys:ro
      - /var/lib/docker:/var/lib/docker:ro
      - /dev/disk:/dev/disk:ro
    networks:
      - linuxio-docker
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:8080/healthz"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 15s
    labels:
      - "io.linuxio.container.icon=si:docker"

  node-exporter:
    image: quay.io/prometheus/node-exporter:latest
    container_name: linuxio-node-exporter
    restart: unless-stopped
    mem_limit: 64m
    mem_reservation: 16m
    network_mode: host
    pid: host
    volumes:
      - /:/host:ro,rslave
    command:
      - --path.rootfs=/host
    healthcheck:
      test: ["CMD", "wget", "--spider", "-q", "http://localhost:9100/metrics"]
      interval: 30s
      timeout: 10s
      retries: 3
      start_period: 15s
    labels:
      - "io.linuxio.container.icon=si:prometheus"

  grafana:
    image: grafana/grafana:latest
    container_name: linuxio-grafana
    restart: unless-stopped
    mem_limit: 256m
    mem_reservation: 64m
    depends_on:
      prometheus:
        condition: service_healthy
    environment:
      GF_SERVER_ROOT_URL: "%(protocol)s://%(domain)s/proxy/grafana/"
      GF_SERVER_SERVE_FROM_SUB_PATH: "true"
      GF_SECURITY_ALLOW_EMBEDDING: "true"
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: "Viewer"
      GF_AUTH_DISABLE_LOGIN_FORM: "true"
    configs:
      - source: grafana_datasources_yml
        target: /etc/grafana/provisioning/datasources/prometheus.yml
    volumes:
      - grafana_data:/var/lib/grafana
    networks:
      - linuxio-docker
    labels:
      - "io.linuxio.container.icon=si:grafana"
      - "io.linuxio.container.url=/proxy/grafana/"
      - "io.linuxio.container.proxy.port=3000"

volumes:
  prometheus_data:
  grafana_data:

configs:
  prometheus_yml:
    content: |
      global:
        scrape_interval: 15s
        evaluation_interval: 15s

      scrape_configs:
        - job_name: prometheus
          static_configs:
            - targets:
                - prometheus:9090

        - job_name: cadvisor
          static_configs:
            - targets:
                - cadvisor:8080

        - job_name: node
          static_configs:
            - targets:
                - host.docker.internal:9100

  grafana_datasources_yml:
    content: |
      apiVersion: 1

      datasources:
        - name: Prometheus
          type: prometheus
          access: proxy
          url: http://prometheus:9090
          isDefault: true
          editable: false

networks:
  linuxio-docker:
    external: true
    name: linuxio-docker
`
}
