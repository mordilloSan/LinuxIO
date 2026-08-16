package docker

import (
	"context"
	"errors"
	"io"
	"iter"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func updateStandaloneContainer(
	ctx context.Context,
	cli nativeContainerUpdateClient,
	before container.InspectResponse,
	imageRef string,
	result apischema.DockerContainerUpdateResult,
) (apischema.DockerContainerUpdateResult, error) {
	return updateStandaloneContainerWithProgress(ctx, cli, before, imageRef, result, nil, nil)
}

func TestComposeCommandArgsPreservesOverrideOrder(t *testing.T) {
	target := composeProjectTarget{
		Name:        "media",
		ConfigFiles: []string{"/srv/media/compose.yml", "/srv/media/compose.prod.yml"},
		WorkingDir:  "/srv/media",
	}
	got, err := composeCommandArgs(target, "up", "-d", "--no-deps", "server")
	if err != nil {
		t.Fatalf("composeCommandArgs: %v", err)
	}
	want := []string{
		"compose", "--progress=json", "--project-name", "media",
		"--file", "/srv/media/compose.yml",
		"--file", "/srv/media/compose.prod.yml",
		"up", "-d", "--no-deps", "server",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("composeCommandArgs = %#v, want %#v", got, want)
	}
}

func TestComposeCommandArgsRejectsIncompleteTarget(t *testing.T) {
	if _, err := composeCommandArgs(composeProjectTarget{}, "up"); err == nil {
		t.Fatal("composeCommandArgs accepted an empty project")
	}
	if _, err := composeCommandArgs(composeProjectTarget{Name: "media"}, "up"); err == nil {
		t.Fatal("composeCommandArgs accepted a project without config files")
	}
}

func TestComposeUpdateInputsRejectEnvironmentInterpolation(t *testing.T) {
	dir := t.TempDir()
	plain := filepath.Join(dir, "plain.yml")
	interpolated := filepath.Join(dir, "interpolated.yml")
	if err := os.WriteFile(plain, []byte("services:\n  web:\n    image: nginx:latest\n    command: echo $$HOSTNAME\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(interpolated, []byte("services:\n  web:\n    image: nginx:${IMAGE_TAG}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateComposeUpdateInputs(composeProjectTarget{Name: "web", ConfigFiles: []string{plain}}); err != nil {
		t.Fatalf("plain Compose config rejected: %v", err)
	}
	t.Setenv("IMAGE_TAG", "from-custom-env-file")
	if err := composePullAndUpServices(context.Background(), composeProjectTarget{Name: "web", ConfigFiles: []string{interpolated}}, []string{"web"}, nil); err == nil || !strings.Contains(err.Error(), "cannot reconstruct safely") {
		t.Fatalf("interpolated Compose config error = %v", err)
	}
}

func TestUpdateComposeContainerTreatsSameImageAsCurrent(t *testing.T) {
	withTempUpdateStatusPath(t)
	before := standaloneTestInspect()
	after := readyContainer("replacement", before.Image)
	fake := newStandaloneUpdateFake()
	fake.inspectResults["web"] = after
	fake.inspectResults["replacement"] = after

	result, err := updateComposeContainerWithRunner(
		context.Background(), fake, before,
		composeProjectTarget{Name: "project", ConfigFiles: []string{"compose.yml"}},
		"web", apischemaUpdateResult(before),
		func(context.Context, composeProjectTarget, string, composeLineEmitter) error { return nil },
	)
	if err != nil {
		t.Fatalf("updateComposeContainerWithRunner: %v", err)
	}
	if result.Updated || result.NewImageID != before.Image || result.ContainerID != after.ID {
		t.Fatalf("result = %+v, want successful current outcome", result)
	}
	status, ok := readUpdateStatusSnapshot().forContainer(after.ID)
	if !ok || status.CheckState != apischema.DockerUpdateCheckStateCurrent {
		t.Fatalf("status = %+v, %v; want current", status, ok)
	}
}

func TestContainerReadyClassification(t *testing.T) {
	tests := []struct {
		name     string
		inspect  container.InspectResponse
		ready    bool
		terminal bool
	}{
		{name: "missing state", inspect: container.InspectResponse{}, terminal: true},
		{name: "created", inspect: container.InspectResponse{State: &container.State{Status: "created"}}},
		{name: "restarting", inspect: container.InspectResponse{State: &container.State{Status: "restarting"}}},
		{name: "exited", inspect: container.InspectResponse{State: &container.State{Status: "exited"}}, terminal: true},
		{name: "removing", inspect: container.InspectResponse{State: &container.State{Status: "removing"}}, terminal: true},
		{name: "dead", inspect: container.InspectResponse{State: &container.State{Status: "dead"}}, terminal: true},
		{name: "paused", inspect: container.InspectResponse{State: &container.State{Status: "running", Running: true, Paused: true}}, terminal: true},
		{name: "restarting flag", inspect: container.InspectResponse{State: &container.State{Status: "running", Running: true, Restarting: true}}},
		{name: "running without health", inspect: container.InspectResponse{State: &container.State{Status: "running", Running: true}}, ready: true},
		{name: "starting health", inspect: container.InspectResponse{State: &container.State{Status: "running", Running: true, Health: &container.Health{Status: "starting"}}}},
		{name: "unknown health", inspect: container.InspectResponse{State: &container.State{Status: "running", Running: true, Health: &container.Health{Status: "unknown"}}}},
		{name: "healthy", inspect: readyContainer("ready", "sha256:image"), ready: true},
		{name: "unhealthy", inspect: container.InspectResponse{State: &container.State{Status: "running", Running: true, Health: &container.Health{Status: "unhealthy"}}}, terminal: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ready, terminal, err := containerReady(tc.inspect)
			if ready != tc.ready || terminal != tc.terminal {
				t.Fatalf("containerReady = (%v, %v, %v), want (%v, %v)", ready, terminal, err, tc.ready, tc.terminal)
			}
			if tc.terminal && err == nil {
				t.Fatal("terminal readiness result did not include an error")
			}
		})
	}
}

func TestWaitForContainerReadyRetriesCreatedThenRunningHealthy(t *testing.T) {
	client := &sequenceReadinessClient{results: []container.InspectResponse{
		{ID: "replacement", State: &container.State{Status: "created"}},
		readyContainer("replacement", "sha256:image"),
	}}
	got, err := waitForContainerReadyWithTiming(context.Background(), client, "replacement", time.Second, time.Millisecond)
	if err != nil {
		t.Fatalf("waitForContainerReadyWithTiming: %v", err)
	}
	if got.ID != "replacement" || len(client.results) != 0 {
		t.Fatalf("result = %+v, remaining inspections = %d", got, len(client.results))
	}
}

func TestValidateComposeServiceScopeRejectsReplicas(t *testing.T) {
	fake := newStandaloneUpdateFake()
	labels := map[string]string{
		"com.docker.compose.project": "media",
		"com.docker.compose.service": "web",
	}
	fake.containerItems = []container.Summary{
		{ID: "replica-1", Labels: labels},
		{ID: "replica-2", Labels: labels},
	}
	if err := validateComposeServiceScope(context.Background(), fake, "media", "web"); err == nil || !strings.Contains(err.Error(), "2 replicas") {
		t.Fatalf("validateComposeServiceScope error = %v", err)
	}
}

func TestValidateStandaloneUpdateRejectsUnsafeOwnership(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*container.InspectResponse)
		wantErr string
	}{
		{
			name: "stopped",
			mutate: func(inspect *container.InspectResponse) {
				inspect.State.Running = false
			},
			wantErr: "must be running and stable",
		},
		{
			name: "auto remove",
			mutate: func(inspect *container.InspectResponse) {
				inspect.HostConfig.AutoRemove = true
			},
			wantErr: "auto-remove",
		},
		{
			name: "swarm",
			mutate: func(inspect *container.InspectResponse) {
				inspect.Config.Labels["com.docker.swarm.service.name"] = "web"
			},
			wantErr: "Docker Swarm",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inspect := standaloneTestInspect()
			tc.mutate(&inspect)
			if err := validateStandaloneUpdate(inspect, false); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateStandaloneUpdate error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}

func TestValidateStandaloneDependentsRejectsNamespaceReferences(t *testing.T) {
	const fullID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, reference := range []string{"web", fullID, fullID[:12]} {
		for _, mode := range []struct {
			name   string
			set    func(*container.HostConfig)
			needle string
		}{
			{name: "network", set: func(h *container.HostConfig) { h.NetworkMode = container.NetworkMode("container:" + reference) }, needle: "network namespace"},
			{name: "ipc", set: func(h *container.HostConfig) { h.IpcMode = container.IpcMode("container:" + reference) }, needle: "IPC namespace"},
			{name: "pid", set: func(h *container.HostConfig) { h.PidMode = container.PidMode("container:" + reference) }, needle: "PID namespace"},
			{name: "cgroup", set: func(h *container.HostConfig) { h.Cgroup = container.CgroupSpec("container:" + reference) }, needle: "cgroup namespace"},
		} {
			t.Run(mode.name+"/"+reference, func(t *testing.T) {
				before := standaloneTestInspect()
				before.ID = fullID
				dependent := standaloneTestInspect()
				dependent.ID = "dependent"
				dependent.Name = "/consumer"
				mode.set(dependent.HostConfig)
				fake := newStandaloneUpdateFake()
				fake.containerItems = []container.Summary{{ID: before.ID}, {ID: dependent.ID, Names: []string{"/consumer"}}}
				fake.inspectResults[dependent.ID] = dependent
				if err := validateStandaloneDependents(context.Background(), fake, before); err == nil || !strings.Contains(err.Error(), mode.needle) {
					t.Fatalf("validateStandaloneDependents error = %v, want %q", err, mode.needle)
				}
			})
		}
	}
}

func TestValidateStandaloneDependentsRejectsVolumesFromReference(t *testing.T) {
	before := standaloneTestInspect()
	dependent := standaloneTestInspect()
	dependent.ID, dependent.Name = "dependent", "/consumer"
	dependent.HostConfig.VolumesFrom = []string{before.ID[:12] + ":ro"}
	fake := newStandaloneUpdateFake()
	fake.containerItems = []container.Summary{
		{ID: before.ID, Names: []string{before.Name}},
		{ID: dependent.ID, Names: []string{dependent.Name}},
	}
	fake.inspectResults[dependent.ID] = dependent

	err := validateStandaloneDependents(context.Background(), fake, before)
	if err == nil || !strings.Contains(err.Error(), "provides volumes") {
		t.Fatalf("validateStandaloneDependents error = %v", err)
	}
}

func TestScheduledStandaloneDependenciesInspectEachContainerOnce(t *testing.T) {
	providers := []container.InspectResponse{standaloneTestInspect(), standaloneTestInspect()}
	providers[0].ID, providers[0].Name = "provider-one", "/one"
	providers[1].ID, providers[1].Name = "provider-two", "/two"
	consumer := standaloneTestInspect()
	consumer.ID, consumer.Name = "consumer", "/consumer"

	fake := newStandaloneUpdateFake()
	fake.containerItems = []container.Summary{
		{ID: providers[0].ID, Names: []string{providers[0].Name}},
		{ID: providers[1].ID, Names: []string{providers[1].Name}},
		{ID: consumer.ID, Names: []string{consumer.Name}},
	}
	for _, inspect := range append(providers, consumer) {
		fake.inspectResults[inspect.ID] = inspect
	}
	state := &scheduledUpdateState{
		ctx:           context.Background(),
		allContainers: fake.containerItems,
		dependencyCli: fake,
	}

	first, err := state.standaloneDependencies()
	if err != nil {
		t.Fatalf("first standaloneDependencies: %v", err)
	}
	second, err := state.standaloneDependencies()
	if err != nil {
		t.Fatalf("second standaloneDependencies: %v", err)
	}
	if first != second {
		t.Fatal("scheduled dependency index was rebuilt")
	}
	for _, provider := range providers {
		if err := first.validate(provider); err != nil {
			t.Fatalf("validate %s: %v", provider.Name, err)
		}
	}
	inspectCalls := 0
	for _, call := range fake.calls {
		if strings.HasPrefix(call, "inspect-container:") {
			inspectCalls++
		}
	}
	if inspectCalls != len(fake.containerItems) {
		t.Fatalf("dependency inspections = %d, want %d", inspectCalls, len(fake.containerItems))
	}
}

func TestStandaloneCreateOptionsCopiesRuntimeConfiguration(t *testing.T) {
	inspect := standaloneTestInspect()
	inspect.Config.Env = []string{"PORT=8080"}
	inspect.Config.Labels["example"] = "value"
	inspect.HostConfig.Binds = []string{"/srv/data:/data:rw"}
	inspect.HostConfig.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyAlways}
	inspect.Mounts = []container.MountPoint{{
		Type:        mount.TypeVolume,
		Name:        "persistent-state",
		Destination: "/state",
		RW:          true,
	}}
	inspect.NetworkSettings = &container.NetworkSettings{Networks: map[string]*network.EndpointSettings{
		"app-net": {
			Aliases:    []string{"web"},
			DriverOpts: map[string]string{"com.example.option": "value"},
			NetworkID:  "runtime-network-id",
			EndpointID: "runtime-endpoint-id",
		},
	}}

	options, err := standaloneCreateOptions(inspect, "docker.io/library/nginx:latest", "web")
	if err != nil {
		t.Fatalf("standaloneCreateOptions: %v", err)
	}
	if options.Name != "web" || options.Config.Image != "docker.io/library/nginx:latest" {
		t.Fatalf("create options identity = %#v", options)
	}
	if !reflect.DeepEqual(options.Config.Env, inspect.Config.Env) || !reflect.DeepEqual(options.HostConfig.Binds, inspect.HostConfig.Binds) {
		t.Fatalf("runtime configuration was not preserved: %#v", options)
	}
	endpoint := options.NetworkingConfig.EndpointsConfig["app-net"]
	if endpoint == nil || !reflect.DeepEqual(endpoint.Aliases, []string{"web"}) {
		t.Fatalf("network endpoint = %#v", endpoint)
	}
	if endpoint.NetworkID != "" || endpoint.EndpointID != "" {
		t.Fatalf("operational network IDs leaked into create request: %#v", endpoint)
	}
	if len(options.HostConfig.Mounts) != 1 || options.HostConfig.Mounts[0].Source != "persistent-state" || options.HostConfig.Mounts[0].Target != "/state" {
		t.Fatalf("persistent volume was not preserved: %#v", options.HostConfig.Mounts)
	}

	options.Config.Env[0] = "PORT=9090"
	options.HostConfig.Binds[0] = "/tmp:/data"
	endpoint.Aliases[0] = "changed"
	if inspect.Config.Env[0] != "PORT=8080" || inspect.HostConfig.Binds[0] != "/srv/data:/data:rw" || inspect.NetworkSettings.Networks["app-net"].Aliases[0] != "web" {
		t.Fatal("create options mutate the inspected container configuration")
	}
}

func TestStandaloneCreateOptionsPreservesTmpfs(t *testing.T) {
	inspect := standaloneTestInspect()
	inspect.HostConfig.Tmpfs = map[string]string{"/run": "rw,size=64m"}
	inspect.Mounts = []container.MountPoint{{
		Type:        mount.TypeTmpfs,
		Destination: "/run",
		RW:          true,
	}}

	options, err := standaloneCreateOptions(inspect, "docker.io/library/nginx:latest", "web")
	if err != nil {
		t.Fatalf("standaloneCreateOptions: %v", err)
	}
	if !reflect.DeepEqual(options.HostConfig.Tmpfs, inspect.HostConfig.Tmpfs) {
		t.Fatalf("tmpfs configuration = %#v, want %#v", options.HostConfig.Tmpfs, inspect.HostConfig.Tmpfs)
	}
	if len(options.HostConfig.Mounts) != 0 {
		t.Fatalf("tmpfs was duplicated as a structured mount: %#v", options.HostConfig.Mounts)
	}
}

func TestStandaloneCreateOptionsRegeneratesDefaultHostname(t *testing.T) {
	const containerID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		name        string
		hostname    string
		networkMode container.NetworkMode
		want        string
	}{
		{
			name:     "default short ID",
			hostname: containerID[:12],
		},
		{
			name:     "explicit hostname",
			hostname: "web.internal",
			want:     "web.internal",
		},
		{
			name:        "host network hostname",
			hostname:    containerID[:12],
			networkMode: container.NetworkMode("host"),
			want:        containerID[:12],
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inspect := standaloneTestInspect()
			inspect.ID = containerID
			inspect.Config.Hostname = tc.hostname
			inspect.HostConfig.NetworkMode = tc.networkMode

			options, err := standaloneCreateOptions(inspect, "docker.io/library/nginx:latest", "web")
			if err != nil {
				t.Fatalf("standaloneCreateOptions: %v", err)
			}
			if options.Config.Hostname != tc.want {
				t.Fatalf("hostname = %q, want %q", options.Config.Hostname, tc.want)
			}
		})
	}
}

func TestUpdateStandaloneContainerRecreatesAndKeepsRollbackUntilReady(t *testing.T) {
	withTempUpdateStatusPath(t)
	before := standaloneTestInspect()
	fake := &fakeNativeUpdateClient{
		pulledImage: client.ImageInspectResult{InspectResponse: image.InspectResponse{ID: "sha256:new"}},
		inspectResults: map[string]container.InspectResponse{
			"replacement": readyContainer("replacement", "sha256:new"),
		},
	}
	result := apischemaUpdateResult(before)

	result, err := updateStandaloneContainer(
		context.Background(), fake, before, "docker.io/library/nginx:latest", result,
	)
	if err != nil {
		t.Fatalf("updateStandaloneContainer: %v", err)
	}
	if !result.Updated || result.ContainerID != "replacement" || result.NewImageID != "sha256:new" {
		t.Fatalf("result = %+v", result)
	}
	wantCalls := []string{
		"list-containers",
		"pull:docker.io/library/nginx:latest",
		"inspect-image:docker.io/library/nginx:latest",
		"stop:old-container",
		"rename:old-container:linuxio-update-backup-old-containe",
		"create:web",
		"start:replacement",
		"inspect-container:replacement",
		"remove:old-container:false",
	}
	if !reflect.DeepEqual(fake.calls, wantCalls) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, wantCalls)
	}
}

func TestUpdateStandaloneContainerRollsBackStartFailure(t *testing.T) {
	before := standaloneTestInspect()
	fake := &fakeNativeUpdateClient{
		pulledImage: client.ImageInspectResult{InspectResponse: image.InspectResponse{ID: "sha256:new"}},
		startErrors: map[string]error{"replacement": errors.New("start failed")},
	}
	result := apischemaUpdateResult(before)

	_, err := updateStandaloneContainer(
		context.Background(), fake, before, "docker.io/library/nginx:latest", result,
	)
	if err == nil || !strings.Contains(err.Error(), "start failed") {
		t.Fatalf("updateStandaloneContainer error = %v", err)
	}
	wantSuffix := []string{
		"start:replacement",
		"remove:replacement:true",
		"rename:old-container:web",
		"start:old-container",
	}
	if len(fake.calls) < len(wantSuffix) || !reflect.DeepEqual(fake.calls[len(fake.calls)-len(wantSuffix):], wantSuffix) {
		t.Fatalf("rollback calls = %#v, want suffix %#v", fake.calls, wantSuffix)
	}
}

func TestUpdateStandaloneContainerStopsBeforeMutationWhenPreparationFails(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*fakeNativeUpdateClient, error)
		wantCalls []string
	}{
		{
			name: "pull request",
			configure: func(fake *fakeNativeUpdateClient, injected error) {
				fake.pullErr = injected
			},
			wantCalls: []string{"list-containers", "pull:docker.io/library/nginx:latest"},
		},
		{
			name: "pull response",
			configure: func(fake *fakeNativeUpdateClient, injected error) {
				fake.pullWaitErr = injected
			},
			wantCalls: []string{"list-containers", "pull:docker.io/library/nginx:latest"},
		},
		{
			name: "pull close",
			configure: func(fake *fakeNativeUpdateClient, injected error) {
				fake.pullCloseErr = injected
			},
			wantCalls: []string{"list-containers", "pull:docker.io/library/nginx:latest"},
		},
		{
			name: "pulled image inspection",
			configure: func(fake *fakeNativeUpdateClient, injected error) {
				fake.imageInspectErr = injected
			},
			wantCalls: []string{
				"list-containers",
				"pull:docker.io/library/nginx:latest",
				"inspect-image:docker.io/library/nginx:latest",
			},
		},
		{
			name: "stop",
			configure: func(fake *fakeNativeUpdateClient, injected error) {
				fake.stopErr = injected
			},
			wantCalls: []string{
				"list-containers",
				"pull:docker.io/library/nginx:latest",
				"inspect-image:docker.io/library/nginx:latest",
				"stop:old-container",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			injected := errors.New("injected " + tc.name + " failure")
			fake := newStandaloneUpdateFake()
			tc.configure(fake, injected)

			_, err := updateStandaloneContainer(
				context.Background(), fake, standaloneTestInspect(), "docker.io/library/nginx:latest", apischemaUpdateResult(standaloneTestInspect()),
			)
			if !errors.Is(err, injected) {
				t.Fatalf("updateStandaloneContainer error = %v, want injected error", err)
			}
			if !reflect.DeepEqual(fake.calls, tc.wantCalls) {
				t.Fatalf("calls = %#v, want %#v", fake.calls, tc.wantCalls)
			}
		})
	}
}

func TestUpdateStandaloneContainerRestoresOriginalAfterMutationFailures(t *testing.T) {
	backupName := standaloneBackupName("old-container")
	tests := []struct {
		name       string
		configure  func(*fakeNativeUpdateClient, error)
		wantSuffix []string
	}{
		{
			name: "rename",
			configure: func(fake *fakeNativeUpdateClient, injected error) {
				fake.renameErrors["old-container:"+backupName] = injected
			},
			wantSuffix: []string{
				"rename:old-container:" + backupName,
				"start:old-container",
			},
		},
		{
			name: "create",
			configure: func(fake *fakeNativeUpdateClient, injected error) {
				fake.createErr = injected
			},
			wantSuffix: []string{
				"create:web",
				"rename:old-container:web",
				"start:old-container",
			},
		},
		{
			name: "verification",
			configure: func(fake *fakeNativeUpdateClient, _ error) {
				fake.inspectResults["replacement"] = container.InspectResponse{
					ID:    "replacement",
					State: &container.State{Status: container.StateExited},
				}
			},
			wantSuffix: []string{
				"inspect-container:replacement",
				"remove:replacement:true",
				"rename:old-container:web",
				"start:old-container",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			injected := errors.New("injected " + tc.name + " failure")
			fake := newStandaloneUpdateFake()
			tc.configure(fake, injected)

			_, err := updateStandaloneContainer(
				context.Background(), fake, standaloneTestInspect(), "docker.io/library/nginx:latest", apischemaUpdateResult(standaloneTestInspect()),
			)
			if tc.name == "verification" {
				if err == nil || !strings.Contains(err.Error(), "verify replacement") {
					t.Fatalf("updateStandaloneContainer error = %v, want verification error", err)
				}
			} else if !errors.Is(err, injected) {
				t.Fatalf("updateStandaloneContainer error = %v, want injected error", err)
			}
			if len(fake.calls) < len(tc.wantSuffix) || !reflect.DeepEqual(fake.calls[len(fake.calls)-len(tc.wantSuffix):], tc.wantSuffix) {
				t.Fatalf("calls = %#v, want suffix %#v", fake.calls, tc.wantSuffix)
			}
		})
	}
}

func TestUpdateStandaloneContainerReportsRollbackFailures(t *testing.T) {
	primaryErr := errors.New("replacement start failed")
	removeErr := errors.New("replacement removal failed")
	restoreErr := errors.New("original rename failed")
	fake := newStandaloneUpdateFake()
	fake.startErrors["replacement"] = primaryErr
	fake.removeErrors["replacement"] = removeErr
	fake.renameErrors["old-container:web"] = restoreErr

	_, err := updateStandaloneContainer(
		context.Background(), fake, standaloneTestInspect(), "docker.io/library/nginx:latest", apischemaUpdateResult(standaloneTestInspect()),
	)
	for _, want := range []error{primaryErr, removeErr, restoreErr} {
		if !errors.Is(err, want) {
			t.Fatalf("updateStandaloneContainer error = %v, want %v", err, want)
		}
	}
	wantSuffix := []string{
		"start:replacement",
		"remove:replacement:true",
		"rename:old-container:web",
	}
	if len(fake.calls) < len(wantSuffix) || !reflect.DeepEqual(fake.calls[len(fake.calls)-len(wantSuffix):], wantSuffix) {
		t.Fatalf("calls = %#v, want suffix %#v", fake.calls, wantSuffix)
	}
}

func TestUpdateStandaloneContainerReportsRollbackStartFailure(t *testing.T) {
	primaryErr := errors.New("replacement create failed")
	restoreErr := errors.New("original start failed")
	fake := newStandaloneUpdateFake()
	fake.createErr = primaryErr
	fake.startErrors["old-container"] = restoreErr

	_, err := updateStandaloneContainer(
		context.Background(), fake, standaloneTestInspect(), "docker.io/library/nginx:latest", apischemaUpdateResult(standaloneTestInspect()),
	)
	if !errors.Is(err, primaryErr) || !errors.Is(err, restoreErr) {
		t.Fatalf("updateStandaloneContainer error = %v, want primary and rollback errors", err)
	}
}

func TestUpdateStandaloneContainerReportsBackupCleanupFailure(t *testing.T) {
	withTempUpdateStatusPath(t)
	cleanupErr := errors.New("backup removal failed")
	fake := newStandaloneUpdateFake()
	fake.removeErrors["old-container"] = cleanupErr

	result, err := updateStandaloneContainer(
		context.Background(), fake, standaloneTestInspect(), "docker.io/library/nginx:latest", apischemaUpdateResult(standaloneTestInspect()),
	)
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("updateStandaloneContainer error = %v, want cleanup error", err)
	}
	if !result.Updated || result.ContainerID != "replacement" {
		t.Fatalf("result = %+v, want active replacement", result)
	}
}

func TestUpdateStoppedStandaloneContainerPreservesStoppedState(t *testing.T) {
	withTempUpdateStatusPath(t)
	before := standaloneTestInspect()
	before.State = &container.State{Status: container.StateExited}
	fake := newStandaloneUpdateFake()
	fake.inspectResults["replacement"] = container.InspectResponse{
		ID:     "replacement",
		Name:   "/web",
		Image:  "sha256:new",
		State:  &container.State{Status: container.StateCreated},
		Config: &container.Config{Image: "nginx:latest"},
	}

	result, err := updateStandaloneContainerWithDependenciesAndPolicy(
		context.Background(),
		fake,
		before,
		"docker.io/library/nginx:latest",
		apischemaUpdateResult(before),
		nil,
		nil,
		stoppedContainerUpdatePolicy{Allow: true},
	)
	if err != nil {
		t.Fatalf("update stopped standalone container: %v", err)
	}
	if !result.Updated || result.ContainerID != "replacement" || result.NewImageID != "sha256:new" {
		t.Fatalf("result = %+v", result)
	}
	calls := strings.Join(fake.calls, "|")
	if strings.Contains(calls, "stop:old-container") || strings.Contains(calls, "start:replacement") {
		t.Fatalf("stopped update changed lifecycle state: %v", fake.calls)
	}
	if !strings.Contains(calls, "remove:old-container:false") {
		t.Fatalf("rollback container was not removed: %v", fake.calls)
	}
}

func TestUpdateStoppedStandaloneContainerCanReviveReplacement(t *testing.T) {
	withTempUpdateStatusPath(t)
	before := standaloneTestInspect()
	before.State = &container.State{Status: container.StateExited}
	fake := newStandaloneUpdateFake()

	result, err := updateStandaloneContainerWithDependenciesAndPolicy(
		context.Background(),
		fake,
		before,
		"docker.io/library/nginx:latest",
		apischemaUpdateResult(before),
		nil,
		nil,
		stoppedContainerUpdatePolicy{Allow: true, Revive: true},
	)
	if err != nil || !result.Updated {
		t.Fatalf("revive stopped update result = %+v, error = %v", result, err)
	}
	calls := strings.Join(fake.calls, "|")
	if strings.Contains(calls, "stop:old-container") || !strings.Contains(calls, "start:replacement") {
		t.Fatalf("revive lifecycle calls = %v", fake.calls)
	}
}

func TestStoppedStandaloneRollbackDoesNotStartOriginal(t *testing.T) {
	before := standaloneTestInspect()
	before.State = &container.State{Status: container.StateExited}
	fake := newStandaloneUpdateFake()
	fake.createErr = errors.New("replacement create failed")

	_, err := updateStandaloneContainerWithDependenciesAndPolicy(
		context.Background(),
		fake,
		before,
		"docker.io/library/nginx:latest",
		apischemaUpdateResult(before),
		nil,
		nil,
		stoppedContainerUpdatePolicy{Allow: true},
	)
	if err == nil {
		t.Fatal("stopped update unexpectedly succeeded")
	}
	calls := strings.Join(fake.calls, "|")
	if strings.Contains(calls, "start:old-container") {
		t.Fatalf("rollback started the originally stopped container: %v", fake.calls)
	}
}

func TestStoppedStandaloneJournalRecoversVerifiedCleanup(t *testing.T) {
	withTempUpdateStatusPath(t)
	journal := standaloneUpdateJournal{path: filepath.Join(t.TempDir(), "transaction.json")}
	before := standaloneTestInspect()
	before.State = &container.State{Status: container.StateExited}
	fake := newStandaloneUpdateFake()
	fake.inspectResults["replacement"] = container.InspectResponse{
		ID:     "replacement",
		Name:   "/web",
		Image:  "sha256:new",
		State:  &container.State{Status: container.StateCreated},
		Config: &container.Config{Image: "nginx:latest"},
	}
	cleanupErr := errors.New("backup removal failed")
	fake.removeErrors["old-container"] = cleanupErr

	result, err := updateStandaloneContainerWithDependenciesAndPolicy(
		context.Background(),
		fake,
		before,
		"docker.io/library/nginx:latest",
		apischemaUpdateResult(before),
		&journal,
		nil,
		stoppedContainerUpdatePolicy{Allow: true},
	)
	if !result.Updated || !errors.Is(err, cleanupErr) {
		t.Fatalf("stopped update result = %+v, error = %v", result, err)
	}
	backup := before
	backup.Name = "/" + standaloneBackupName(before.ID)
	fake.inspectResults[before.ID] = backup
	delete(fake.removeErrors, before.ID)

	if err := recoverStandaloneUpdate(context.Background(), fake, journal); err != nil {
		t.Fatalf("recover stopped verified update: %v", err)
	}
	if _, err := os.Stat(journal.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal still exists: %v", err)
	}
}

func TestStandaloneUpdateJournalRetriesVerifiedBackupCleanup(t *testing.T) {
	withTempUpdateStatusPath(t)
	journal := standaloneUpdateJournal{path: filepath.Join(t.TempDir(), "transaction.json")}
	cleanupErr := errors.New("backup removal failed")
	fake := newStandaloneUpdateFake()
	fake.removeErrors["old-container"] = cleanupErr

	result, err := updateStandaloneContainerWithProgress(
		context.Background(), fake, standaloneTestInspect(), "docker.io/library/nginx:latest", apischemaUpdateResult(standaloneTestInspect()), &journal, nil,
	)
	if !errors.Is(err, cleanupErr) || !result.Updated {
		t.Fatalf("update result = %+v, error = %v", result, err)
	}
	backup := standaloneTestInspect()
	backup.Name = "/" + standaloneBackupName(backup.ID)
	backup.State = &container.State{Status: container.StateExited}
	fake.inspectResults["old-container"] = backup
	delete(fake.removeErrors, "old-container")

	if err := recoverStandaloneUpdate(context.Background(), fake, journal); err != nil {
		t.Fatalf("recoverStandaloneUpdate: %v", err)
	}
	if _, err := os.Stat(journal.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal still exists: %v", err)
	}
	if got := fake.calls[len(fake.calls)-1]; got != "remove:old-container:false" {
		t.Fatalf("last recovery call = %q", got)
	}
}

func TestStandaloneUpdateJournalDoesNotSweepUnjournaledBackups(t *testing.T) {
	journal := standaloneUpdateJournal{path: filepath.Join(t.TempDir(), "transaction.json")}
	fake := newStandaloneUpdateFake()
	if err := recoverStandaloneUpdate(context.Background(), fake, journal); err != nil {
		t.Fatalf("recoverStandaloneUpdate: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("unexpected Docker calls without a journal: %v", fake.calls)
	}
}

func newStandaloneUpdateFake() *fakeNativeUpdateClient {
	return &fakeNativeUpdateClient{
		pulledImage: client.ImageInspectResult{InspectResponse: image.InspectResponse{ID: "sha256:new"}},
		inspectResults: map[string]container.InspectResponse{
			"replacement": readyContainer("replacement", "sha256:new"),
		},
		startErrors:  make(map[string]error),
		renameErrors: make(map[string]error),
		removeErrors: make(map[string]error),
	}
}

func standaloneTestInspect() container.InspectResponse {
	return container.InspectResponse{
		ID:    "old-container",
		Name:  "/web",
		Image: "sha256:old",
		State: &container.State{Status: container.StateRunning, Running: true},
		Config: &container.Config{
			Image:  "nginx:latest",
			Labels: map[string]string{},
		},
		HostConfig: &container.HostConfig{},
	}
}

func readyContainer(id, imageID string) container.InspectResponse {
	return container.InspectResponse{
		ID:    id,
		Name:  "/web",
		Image: imageID,
		State: &container.State{
			Status:  container.StateRunning,
			Running: true,
			Health:  &container.Health{Status: container.Healthy},
		},
		Config: &container.Config{Image: "nginx:latest"},
	}
}

func apischemaUpdateResult(inspect container.InspectResponse) apischema.DockerContainerUpdateResult {
	return apischema.DockerContainerUpdateResult{
		ContainerID:     inspect.ID,
		ContainerName:   strings.TrimPrefix(inspect.Name, "/"),
		Image:           inspect.Config.Image,
		PreviousImageID: inspect.Image,
	}
}

type fakeImagePullResponse struct {
	waitErr  error
	closeErr error
}

type sequenceReadinessClient struct {
	results []container.InspectResponse
}

func (c *sequenceReadinessClient) ContainerInspect(
	context.Context,
	string,
	client.ContainerInspectOptions,
) (client.ContainerInspectResult, error) {
	if len(c.results) == 0 {
		return client.ContainerInspectResult{}, errors.New("no more readiness results")
	}
	result := c.results[0]
	c.results = c.results[1:]
	return client.ContainerInspectResult{Container: result}, nil
}

func (f *fakeImagePullResponse) Read([]byte) (int, error) { return 0, io.EOF }
func (f *fakeImagePullResponse) Close() error             { return f.closeErr }
func (f *fakeImagePullResponse) Wait(context.Context) error {
	return f.waitErr
}
func (f *fakeImagePullResponse) JSONMessages(context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(func(jsonstream.Message, error) bool) {}
}

type fakeNativeUpdateClient struct {
	calls           []string
	pulledImage     client.ImageInspectResult
	pullErr         error
	pullWaitErr     error
	pullCloseErr    error
	imageInspectErr error
	inspectResults  map[string]container.InspectResponse
	startErrors     map[string]error
	renameErrors    map[string]error
	removeErrors    map[string]error
	stopErr         error
	createErr       error
	containerItems  []container.Summary
}

func (f *fakeNativeUpdateClient) ImageInspect(
	_ context.Context,
	imageRef string,
	_ ...client.ImageInspectOption,
) (client.ImageInspectResult, error) {
	f.calls = append(f.calls, "inspect-image:"+imageRef)
	return f.pulledImage, f.imageInspectErr
}

func (f *fakeNativeUpdateClient) DistributionInspect(
	context.Context,
	string,
	client.DistributionInspectOptions,
) (client.DistributionInspectResult, error) {
	return client.DistributionInspectResult{}, errors.New("unexpected DistributionInspect call")
}

func (f *fakeNativeUpdateClient) ImagePull(
	_ context.Context,
	imageRef string,
	_ client.ImagePullOptions,
) (client.ImagePullResponse, error) {
	f.calls = append(f.calls, "pull:"+imageRef)
	if f.pullErr != nil {
		return nil, f.pullErr
	}
	return &fakeImagePullResponse{waitErr: f.pullWaitErr, closeErr: f.pullCloseErr}, nil
}

func (f *fakeNativeUpdateClient) ContainerInspect(
	_ context.Context,
	containerID string,
	_ client.ContainerInspectOptions,
) (client.ContainerInspectResult, error) {
	f.calls = append(f.calls, "inspect-container:"+containerID)
	inspect, ok := f.inspectResults[containerID]
	if !ok {
		return client.ContainerInspectResult{}, errors.New("unexpected container inspect")
	}
	return client.ContainerInspectResult{Container: inspect}, nil
}

func (f *fakeNativeUpdateClient) ContainerList(
	context.Context,
	client.ContainerListOptions,
) (client.ContainerListResult, error) {
	f.calls = append(f.calls, "list-containers")
	return client.ContainerListResult{Items: f.containerItems}, nil
}

func (f *fakeNativeUpdateClient) ContainerStop(
	_ context.Context,
	containerID string,
	_ client.ContainerStopOptions,
) (client.ContainerStopResult, error) {
	f.calls = append(f.calls, "stop:"+containerID)
	return client.ContainerStopResult{}, f.stopErr
}

func (f *fakeNativeUpdateClient) ContainerRename(
	_ context.Context,
	containerID string,
	options client.ContainerRenameOptions,
) (client.ContainerRenameResult, error) {
	f.calls = append(f.calls, "rename:"+containerID+":"+options.NewName)
	return client.ContainerRenameResult{}, f.renameErrors[containerID+":"+options.NewName]
}

func (f *fakeNativeUpdateClient) ContainerCreate(
	_ context.Context,
	options client.ContainerCreateOptions,
) (client.ContainerCreateResult, error) {
	f.calls = append(f.calls, "create:"+options.Name)
	if f.createErr != nil {
		return client.ContainerCreateResult{}, f.createErr
	}
	return client.ContainerCreateResult{ID: "replacement"}, nil
}

func (f *fakeNativeUpdateClient) ContainerStart(
	_ context.Context,
	containerID string,
	_ client.ContainerStartOptions,
) (client.ContainerStartResult, error) {
	f.calls = append(f.calls, "start:"+containerID)
	return client.ContainerStartResult{}, f.startErrors[containerID]
}

func (f *fakeNativeUpdateClient) ContainerRemove(
	_ context.Context,
	containerID string,
	options client.ContainerRemoveOptions,
) (client.ContainerRemoveResult, error) {
	f.calls = append(f.calls, "remove:"+containerID+":"+string(rune('0'+boolInt(options.Force))))
	if options.Force {
		f.calls[len(f.calls)-1] = "remove:" + containerID + ":true"
	} else {
		f.calls[len(f.calls)-1] = "remove:" + containerID + ":false"
	}
	return client.ContainerRemoveResult{}, f.removeErrors[containerID]
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
