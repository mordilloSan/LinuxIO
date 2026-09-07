package docker

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/monitoring"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func TestContainerMetricsFromSnapshot(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	fullID := "abc123456789def123456789def123456789def123456789def123456789def1"
	readRate := float64(300)
	writeRate := float64(400)
	snapshot := monitoring.ContainerMetricsSnapshot{
		CapturedAtMs:      now.Add(-30 * time.Second).UnixMilli(),
		CollectorInterval: 15 * time.Second,
		Samples: map[string]monitoring.ContainerMetricSample{
			fullID[:12]: {
				ID:                           fullID[:12],
				CPUPercent:                   125.5,
				MemoryUsageBytes:             512 << 20,
				NetworkReceiveBytesPerSecond: 100,
				NetworkSendBytesPerSecond:    200,
				BlockReadBytesPerSecond:      &readRate,
				BlockWriteBytesPerSecond:     &writeRate,
			},
		},
	}
	ctr := container.Summary{ID: fullID, State: container.StateRunning}

	got := containerMetricsFromSnapshot(ctr, snapshot, nil, now)
	if got.Status != apischema.ContainerMetricsStatusAvailable || got.CapturedAtMs == nil || *got.CapturedAtMs != snapshot.CapturedAtMs {
		t.Fatalf("metrics status/capture = %#v", got)
	}
	if got.CPUPercent == nil || *got.CPUPercent != 125.5 || got.MemoryUsageBytes == nil || *got.MemoryUsageBytes != 512<<20 {
		t.Fatalf("CPU/memory metrics = %#v", got)
	}
	if got.NetworkReceiveBytesPerSecond == nil || *got.NetworkReceiveBytesPerSecond != 100 ||
		got.NetworkSendBytesPerSecond == nil || *got.NetworkSendBytesPerSecond != 200 {
		t.Fatalf("network rates = %#v", got)
	}
	if got.BlockReadBytesPerSecond == nil || *got.BlockReadBytesPerSecond != readRate ||
		got.BlockWriteBytesPerSecond == nil || *got.BlockWriteBytesPerSecond != writeRate {
		t.Fatalf("block rates = %#v", got)
	}
}

func TestContainerMetricsFromSnapshotReportsState(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	fullID := "abc123456789def123456789def123456789def123456789def123456789def1"
	sample := monitoring.ContainerMetricSample{ID: fullID[:12]}
	fresh := monitoring.ContainerMetricsSnapshot{
		CapturedAtMs:      now.UnixMilli(),
		CollectorInterval: 15 * time.Second,
		Samples:           map[string]monitoring.ContainerMetricSample{sample.ID: sample},
	}

	tests := []struct {
		name     string
		ctr      container.Summary
		snapshot monitoring.ContainerMetricsSnapshot
		err      error
		want     apischema.ContainerMetricsStatus
	}{
		{name: "stale", ctr: container.Summary{ID: fullID, State: container.StateRunning}, snapshot: monitoring.ContainerMetricsSnapshot{CapturedAtMs: now.Add(-2 * time.Minute).UnixMilli(), CollectorInterval: 15 * time.Second, Samples: fresh.Samples}, want: apischema.ContainerMetricsStatusStale},
		{name: "stopped", ctr: container.Summary{ID: fullID, State: container.StateExited}, snapshot: fresh, want: apischema.ContainerMetricsStatusNotRunning},
		{name: "sample missing", ctr: container.Summary{ID: "missing", State: container.StateRunning}, snapshot: fresh, want: apischema.ContainerMetricsStatusUnavailable},
		{name: "monitoring unavailable", ctr: container.Summary{ID: fullID, State: container.StateRunning}, snapshot: fresh, err: errors.New("offline"), want: apischema.ContainerMetricsStatusUnavailable},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containerMetricsFromSnapshot(tt.ctr, tt.snapshot, tt.err, now)
			if got.Status != tt.want {
				t.Fatalf("status = %q, want %q", got.Status, tt.want)
			}
			if tt.want == apischema.ContainerMetricsStatusUnavailable || tt.want == apischema.ContainerMetricsStatusNotRunning {
				if got.CapturedAtMs != nil || got.CPUPercent != nil || got.MemoryUsageBytes != nil {
					t.Fatalf("unavailable metrics expose values: %#v", got)
				}
			}
		})
	}
}

type fakeContainerInspector struct {
	inspect container.InspectResponse
	err     error
}

func (f fakeContainerInspector) ContainerInspect(ctx context.Context, _ string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	if f.err != nil {
		return client.ContainerInspectResult{}, f.err
	}
	if err := ctx.Err(); err != nil {
		return client.ContainerInspectResult{}, err
	}
	return client.ContainerInspectResult{Container: f.inspect}, nil
}

func TestContainerInspectInfoFromSDK(t *testing.T) {
	httpPort := network.MustParsePort("80/tcp")
	dnsPort := network.MustParsePort("53/udp")
	inspect := container.InspectResponse{
		ID:           "container-id",
		Created:      "2026-09-03T10:00:00Z",
		Image:        "sha256:image-id",
		Name:         "/example",
		RestartCount: 2,
		Config: &container.Config{
			Cmd:          []string{"serve", "--port", "80"},
			Entrypoint:   []string{"/entrypoint"},
			Env:          []string{"TOKEN=secret", "EMPTY", "ALPHA=first"},
			ExposedPorts: network.PortSet{httpPort: {}, dnsPort: {}},
			Image:        "example:latest",
			Labels:       map[string]string{"purpose": "test"},
			User:         "1000:1000",
			WorkingDir:   "/app",
		},
		HostConfig: &container.HostConfig{
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyOnFailure, MaximumRetryCount: 3},
		},
		State: &container.State{
			Status:    container.StateRunning,
			Running:   true,
			StartedAt: "2026-09-03T10:01:00Z",
			Health:    &container.Health{Status: container.Healthy, FailingStreak: 1},
		},
		Mounts: []container.MountPoint{{Type: "volume", Name: "data", Source: "/var/lib/docker/volumes/data/_data", Destination: "/data", RW: true}},
		NetworkSettings: &container.NetworkSettings{
			Networks: map[string]*network.EndpointSettings{
				"frontend": {IPAddress: netip.MustParseAddr("172.20.0.2"), Aliases: []string{"example", "web"}},
			},
			Ports: network.PortMap{
				httpPort: {{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: "8080"}},
			},
		},
	}

	got := containerInspectInfoFromSDK(inspect)
	if got.ID != inspect.ID || got.Name != "example" || got.Image != "example:latest" || got.ImageID != inspect.Image {
		t.Fatalf("identity = %#v", got)
	}
	if got.State.Status != "running" || !got.State.Running || got.Health == nil || got.Health.Status != "healthy" || got.Health.FailingStreak != 1 {
		t.Fatalf("state/health = %#v / %#v", got.State, got.Health)
	}
	if got.RestartPolicy.Name != "on-failure" || got.RestartPolicy.MaximumRetryCount != 3 {
		t.Fatalf("restart policy = %#v", got.RestartPolicy)
	}
	wantEnvironment := []apischema.ContainerEnvironmentVariable{
		{Name: "ALPHA", Value: "first"},
		{Name: "EMPTY"},
		{Name: "TOKEN", Value: "secret"},
	}
	if !reflect.DeepEqual(got.Environment, wantEnvironment) {
		t.Fatalf("environment = %#v, want %#v", got.Environment, wantEnvironment)
	}
	if len(got.Ports) != 2 || got.Ports[0].ContainerPort != 53 || got.Ports[0].HostPort != "" || got.Ports[1].ContainerPort != 80 || got.Ports[1].HostPort != "8080" {
		t.Fatalf("ports = %#v", got.Ports)
	}
	if got.Networks["frontend"].IPAddress != "172.20.0.2" || !reflect.DeepEqual(got.Networks["frontend"].Aliases, []string{"example", "web"}) || len(got.Mounts) != 1 || got.Mounts[0].Destination != "/data" {
		t.Fatalf("networks/mounts = %#v / %#v", got.Networks, got.Mounts)
	}
}

func TestInspectContainerPreservesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := inspectContainer(ctx, fakeContainerInspector{}, "container-id")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context cancellation", err)
	}
}

type fakeContainerKiller struct {
	options client.ContainerKillOptions
	err     error
}

type fakeContainerPauser struct {
	id  string
	err error
}

func (f *fakeContainerPauser) ContainerPause(_ context.Context, id string, _ client.ContainerPauseOptions) (client.ContainerPauseResult, error) {
	f.id = id
	return client.ContainerPauseResult{}, f.err
}

type fakeContainerUnpauser struct {
	id  string
	err error
}

func (f *fakeContainerUnpauser) ContainerUnpause(_ context.Context, id string, _ client.ContainerUnpauseOptions) (client.ContainerUnpauseResult, error) {
	f.id = id
	return client.ContainerUnpauseResult{}, f.err
}

func (f *fakeContainerKiller) ContainerKill(_ context.Context, _ string, options client.ContainerKillOptions) (client.ContainerKillResult, error) {
	f.options = options
	return client.ContainerKillResult{}, f.err
}

type fakeContainerRemover struct {
	options client.ContainerRemoveOptions
	err     error
}

func (f *fakeContainerRemover) ContainerRemove(_ context.Context, _ string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	f.options = options
	return client.ContainerRemoveResult{}, f.err
}

func TestContainerDestructiveActionOptionsAndErrors(t *testing.T) {
	killer := &fakeContainerKiller{}
	if err := killContainer(context.Background(), killer, "container-id"); err != nil {
		t.Fatalf("killContainer: %v", err)
	}
	if killer.options.Signal != "SIGKILL" {
		t.Fatalf("kill signal = %q, want SIGKILL", killer.options.Signal)
	}

	remover := &fakeContainerRemover{}
	if err := removeContainer(context.Background(), remover, "container-id", false); err != nil {
		t.Fatalf("removeContainer: %v", err)
	}
	if remover.options.Force || remover.options.RemoveVolumes {
		t.Fatalf("safe remove options = %#v", remover.options)
	}
	if err := removeContainer(context.Background(), remover, "container-id", true); err != nil {
		t.Fatalf("force removeContainer: %v", err)
	}
	if !remover.options.Force || remover.options.RemoveVolumes {
		t.Fatalf("force remove options = %#v", remover.options)
	}

	want := errors.New("daemon rejected action")
	remover.err = want
	if err := removeContainer(context.Background(), remover, "container-id", false); !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped daemon error", err)
	}
}

func TestContainerPauseActionsUseDockerSDK(t *testing.T) {
	pauser := &fakeContainerPauser{}
	if err := pauseContainer(context.Background(), pauser, "container-id"); err != nil {
		t.Fatalf("pauseContainer: %v", err)
	}
	if pauser.id != "container-id" {
		t.Fatalf("paused container = %q", pauser.id)
	}

	unpauser := &fakeContainerUnpauser{}
	if err := unpauseContainer(context.Background(), unpauser, "container-id"); err != nil {
		t.Fatalf("unpauseContainer: %v", err)
	}
	if unpauser.id != "container-id" {
		t.Fatalf("unpaused container = %q", unpauser.id)
	}

	want := errors.New("daemon rejected action")
	pauser.err = want
	if err := pauseContainer(context.Background(), pauser, "container-id"); !errors.Is(err, want) {
		t.Fatalf("error = %v, want wrapped daemon error", err)
	}
}

func TestContainerHandlersRejectEmptyIDs(t *testing.T) {
	handlers := dockerHandlers{}
	request := apischema.ContainerIDRequest{ContainerID: "  "}
	tests := map[string]func() error{
		"inspect": func() error { _, err := handlers.handleInspectContainer(context.Background(), request); return err },
		"start":   func() error { return handlers.handleStartContainer(context.Background(), request) },
		"stop":    func() error { return handlers.handleStopContainer(context.Background(), request) },
		"restart": func() error { return handlers.handleRestartContainer(context.Background(), request) },
		"pause":   func() error { return handlers.handlePauseContainer(context.Background(), request) },
		"unpause": func() error { return handlers.handleUnpauseContainer(context.Background(), request) },
		"kill":    func() error { return handlers.handleKillContainer(context.Background(), request) },
		"remove": func() error {
			return handlers.handleRemoveContainer(context.Background(), apischema.ContainerRemoveRequest{ContainerID: "  "})
		},
	}
	for name, run := range tests {
		t.Run(name, func(t *testing.T) {
			if err := run(); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
				t.Fatalf("error = %v, want ErrInvalidArgs", err)
			}
		})
	}
}
