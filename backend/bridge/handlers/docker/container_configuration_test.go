package docker

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func testContainerConfiguration() apischema.ContainerConfiguration {
	return apischema.ContainerConfiguration{
		Name:  "web",
		Image: "nginx:latest",
		RestartPolicy: apischema.ContainerRestartPolicy{
			Name: "no",
		},
		Networks: []apischema.ContainerNetworkAttachment{{Name: "bridge"}},
	}
}

func TestContainerCreateOptionsMapsSupportedConfiguration(t *testing.T) {
	configuration := testContainerConfiguration()
	configuration.Command = []string{"serve", "--port", "8080"}
	configuration.Entrypoint = []string{"/entrypoint"}
	configuration.Environment = []apischema.ContainerEnvironmentVariable{{Name: "TOKEN", Value: "secret"}}
	configuration.Ports = []apischema.ContainerPortBinding{{ContainerPort: 8080, HostIP: "127.0.0.1", HostPort: "18080", Protocol: "tcp"}}
	configuration.Mounts = []apischema.ContainerMountConfiguration{
		{Type: "bind", Source: "/srv/config", Destination: "/app/config", ReadOnly: true},
		{Type: "volume", Source: "data", Destination: "/app/data"},
	}
	configuration.Networks = []apischema.ContainerNetworkAttachment{{Name: "frontend", Aliases: []string{"web", " api "}}}
	configuration.RestartPolicy = apischema.ContainerRestartPolicy{Name: "on-failure", MaximumRetryCount: 3}
	configuration.User = "1000:1000"
	configuration.WorkingDirectory = "/app"

	options, err := containerCreateOptions(configuration)
	if err != nil {
		t.Fatalf("containerCreateOptions: %v", err)
	}
	if options.Name != "web" || options.Config.Image != "nginx:latest" || !slices.Equal(options.Config.Cmd, configuration.Command) || !slices.Equal(options.Config.Entrypoint, configuration.Entrypoint) {
		t.Fatalf("basic options = %#v", options)
	}
	if !slices.Equal(options.Config.Env, []string{"TOKEN=secret"}) || options.Config.User != "1000:1000" || options.Config.WorkingDir != "/app" {
		t.Fatalf("runtime options = %#v", options.Config)
	}
	port := network.MustParsePort("8080/tcp")
	bindings := options.HostConfig.PortBindings[port]
	if len(bindings) != 1 || bindings[0].HostIP.String() != "127.0.0.1" || bindings[0].HostPort != "18080" {
		t.Fatalf("port bindings = %#v", bindings)
	}
	if len(options.HostConfig.Mounts) != 2 || options.HostConfig.Mounts[0].Type != mount.TypeBind || !options.HostConfig.Mounts[0].ReadOnly {
		t.Fatalf("mounts = %#v", options.HostConfig.Mounts)
	}
	endpoint := options.NetworkingConfig.EndpointsConfig["frontend"]
	if endpoint == nil || !slices.Equal(endpoint.Aliases, []string{"web", "api"}) || options.HostConfig.NetworkMode != "frontend" {
		t.Fatalf("networking = %#v / %q", endpoint, options.HostConfig.NetworkMode)
	}
	if options.HostConfig.RestartPolicy.Name != "on-failure" || options.HostConfig.RestartPolicy.MaximumRetryCount != 3 {
		t.Fatalf("restart policy = %#v", options.HostConfig.RestartPolicy)
	}
}

func TestApplyContainerConfigurationPreservesAttachedPrimaryNetwork(t *testing.T) {
	configuration := testContainerConfiguration()
	configuration.Networks = []apischema.ContainerNetworkAttachment{{Name: "frontend"}, {Name: "backend"}}
	options, err := standaloneCreateOptions(standaloneTestInspect(), configuration.Image, configuration.Name)
	if err != nil {
		t.Fatalf("standaloneCreateOptions: %v", err)
	}
	options.HostConfig.NetworkMode = "backend"
	options.HostConfig.Mounts = []mount.Mount{{
		Type:        mount.TypeBind,
		Source:      "/srv/config",
		Target:      "/app/config",
		ReadOnly:    true,
		BindOptions: &mount.BindOptions{Propagation: mount.PropagationRShared},
	}}
	options.NetworkingConfig.EndpointsConfig["backend"] = &network.EndpointSettings{
		Aliases:    []string{"old"},
		DriverOpts: map[string]string{"com.example.option": "value"},
	}
	configuration.Mounts = []apischema.ContainerMountConfiguration{{
		Type: "bind", Source: "/srv/config", Destination: "/app/config", ReadOnly: true,
	}}
	configuration.Networks[1].Aliases = []string{"api"}

	if err := applyContainerConfiguration(&options, configuration); err != nil {
		t.Fatalf("applyContainerConfiguration: %v", err)
	}
	if options.HostConfig.NetworkMode != "backend" {
		t.Fatalf("network mode = %q, want backend", options.HostConfig.NetworkMode)
	}
	if options.HostConfig.Mounts[0].BindOptions.Propagation != mount.PropagationRShared {
		t.Fatalf("mount options = %#v", options.HostConfig.Mounts[0])
	}
	endpoint := options.NetworkingConfig.EndpointsConfig["backend"]
	if endpoint.DriverOpts["com.example.option"] != "value" || !slices.Equal(endpoint.Aliases, []string{"api"}) {
		t.Fatalf("endpoint = %#v", endpoint)
	}
}

func TestContainerConfigurationRejectsUnsafeOrAmbiguousValues(t *testing.T) {
	tests := map[string]func(*apischema.ContainerConfiguration){
		"duplicate environment": func(configuration *apischema.ContainerConfiguration) {
			configuration.Environment = []apischema.ContainerEnvironmentVariable{{Name: "TOKEN"}, {Name: " TOKEN "}}
		},
		"relative bind source": func(configuration *apischema.ContainerConfiguration) {
			configuration.Mounts = []apischema.ContainerMountConfiguration{{Type: "bind", Source: "config", Destination: "/app/config"}}
		},
		"host network with ports": func(configuration *apischema.ContainerConfiguration) {
			configuration.Networks = []apischema.ContainerNetworkAttachment{{Name: "host"}}
			configuration.Ports = []apischema.ContainerPortBinding{{ContainerPort: 80, HostPort: "8080", Protocol: "tcp"}}
		},
		"unknown protocol": func(configuration *apischema.ContainerConfiguration) {
			configuration.Ports = []apischema.ContainerPortBinding{{ContainerPort: 80, Protocol: "http"}}
		},
		"null working directory": func(configuration *apischema.ContainerConfiguration) {
			configuration.WorkingDirectory = "/app\x00data"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			configuration := testContainerConfiguration()
			mutate(&configuration)
			if _, err := containerCreateOptions(configuration); err == nil {
				t.Fatal("invalid configuration was accepted")
			}
		})
	}
}

func TestCreateConfiguredContainerPullsOnlyWhenImageIsMissing(t *testing.T) {
	fake := newStandaloneUpdateFake()
	fake.imageInspectErrs = []error{errdefs.ErrNotFound, nil}
	configuration := testContainerConfiguration()
	options, err := containerCreateOptions(configuration)
	if err != nil {
		t.Fatalf("containerCreateOptions: %v", err)
	}

	result, err := createConfiguredContainer(context.Background(), fake, apischema.ContainerCreateRequest{
		Configuration: configuration,
		Start:         true,
	}, options)
	if err != nil {
		t.Fatalf("createConfiguredContainer: %v", err)
	}
	if result.ContainerID != "replacement" || result.Name != "web" {
		t.Fatalf("result = %#v", result)
	}
	if !slices.Contains(fake.calls, "pull:nginx:latest") || !slices.Contains(fake.calls, "start:replacement") {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestEditConfiguredContainerUsesRollbackTransactionAndNewName(t *testing.T) {
	fake := newStandaloneUpdateFake()
	before := standaloneTestInspect()
	fake.inspectResults[before.ID] = before
	fake.inspectResults["replacement"] = readyContainer("replacement", "sha256:new")
	configuration := testContainerConfiguration()
	configuration.Name = "renamed-web"
	configuration.Image = "nginx:stable"
	journal := standaloneUpdateJournal{path: filepath.Join(t.TempDir(), "transaction.json")}

	result, err := editConfiguredContainer(context.Background(), fake, apischema.ContainerEditRequest{
		ContainerID:   before.ID,
		Configuration: configuration,
	}, &journal)
	if err != nil {
		t.Fatalf("editConfiguredContainer: %v", err)
	}
	if result.ContainerID != "replacement" || result.Name != "renamed-web" {
		t.Fatalf("result = %#v", result)
	}
	if len(fake.createOptions) != 1 || fake.createOptions[0].Name != "renamed-web" || fake.createOptions[0].Config.Image != "nginx:stable" {
		t.Fatalf("create options = %#v", fake.createOptions)
	}
	wantCalls := []string{
		"stop:old-container",
		"rename:old-container:linuxio-update-backup-old-containe",
		"create:renamed-web",
		"start:replacement",
		"remove:old-container:false",
	}
	for _, call := range wantCalls {
		if !slices.Contains(fake.calls, call) {
			t.Errorf("calls %q do not contain %q", fake.calls, call)
		}
	}
	if _, ok, err := journal.read(); err != nil || ok {
		t.Fatalf("journal remains after edit: exists=%v err=%v", ok, err)
	}
}

func TestEditConfiguredContainerRejectsComposeManagedContainer(t *testing.T) {
	fake := newStandaloneUpdateFake()
	before := standaloneTestInspect()
	before.Config.Labels = map[string]string{"com.docker.compose.project": "example"}
	fake.inspectResults[before.ID] = before
	journal := standaloneUpdateJournal{path: filepath.Join(t.TempDir(), "transaction.json")}

	_, err := editConfiguredContainer(context.Background(), fake, apischema.ContainerEditRequest{
		ContainerID:   before.ID,
		Configuration: testContainerConfiguration(),
	}, &journal)
	if err == nil || slices.ContainsFunc(fake.calls, func(call string) bool {
		return strings.HasPrefix(call, "stop:") || strings.HasPrefix(call, "rename:")
	}) {
		t.Fatalf("error = %v, calls = %#v", err, fake.calls)
	}
}

func TestEditConfiguredContainerRejectsAnonymousVolumesBeforeDowntime(t *testing.T) {
	fake := newStandaloneUpdateFake()
	before := standaloneTestInspect()
	before.Mounts = []container.MountPoint{{Type: mount.TypeVolume, Destination: "/data", RW: true}}
	before.HostConfig.Mounts = []mount.Mount{{Type: mount.TypeVolume, Target: "/data"}}
	fake.inspectResults[before.ID] = before
	journal := standaloneUpdateJournal{path: filepath.Join(t.TempDir(), "transaction.json")}

	_, err := editConfiguredContainer(context.Background(), fake, apischema.ContainerEditRequest{
		ContainerID:   before.ID,
		Configuration: testContainerConfiguration(),
	}, &journal)
	if err == nil || slices.ContainsFunc(fake.calls, func(call string) bool {
		return strings.HasPrefix(call, "stop:") || strings.HasPrefix(call, "rename:")
	}) {
		t.Fatalf("error = %v, calls = %#v", err, fake.calls)
	}
}

func TestCreateConfiguredContainerRemovesFailedStartedContainer(t *testing.T) {
	fake := newStandaloneUpdateFake()
	fake.startErrors["replacement"] = errors.New("start failed")
	configuration := testContainerConfiguration()
	options, err := containerCreateOptions(configuration)
	if err != nil {
		t.Fatalf("containerCreateOptions: %v", err)
	}

	_, err = createConfiguredContainer(context.Background(), fake, apischema.ContainerCreateRequest{
		Configuration: configuration,
		Start:         true,
	}, options)
	if err == nil || !slices.Contains(fake.calls, "remove:replacement:true") {
		t.Fatalf("error = %v, calls = %#v", err, fake.calls)
	}
}
