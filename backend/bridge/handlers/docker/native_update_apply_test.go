package docker

import (
	"context"
	"errors"
	"io"
	"iter"
	"reflect"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

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
			if err := validateStandaloneUpdate(inspect); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("validateStandaloneUpdate error = %v, want %q", err, tc.wantErr)
			}
		})
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
	return client.ContainerListResult{}, nil
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
