package docker

import (
	"context"
	"errors"
	"io"
	"iter"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/client"
)

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

type fakeImagePullResponse struct {
	waitErr  error
	closeErr error
}

func (f *fakeImagePullResponse) Read([]byte) (int, error) { return 0, io.EOF }

func (f *fakeImagePullResponse) Close() error { return f.closeErr }

func (f *fakeImagePullResponse) Wait(context.Context) error {
	return f.waitErr
}

func (f *fakeImagePullResponse) JSONMessages(context.Context) iter.Seq2[jsonstream.Message, error] {
	return func(func(jsonstream.Message, error) bool) {}
}

type fakeNativeUpdateClient struct {
	calls            []string
	pulledImage      client.ImageInspectResult
	pullErr          error
	pullWaitErr      error
	pullCloseErr     error
	imageInspectErr  error
	imageInspectErrs []error
	inspectResults   map[string]container.InspectResponse
	startErrors      map[string]error
	renameErrors     map[string]error
	removeErrors     map[string]error
	stopErr          error
	createErr        error
	createOptions    []client.ContainerCreateOptions
	containerItems   []container.Summary
}

func (f *fakeNativeUpdateClient) ImageInspect(
	_ context.Context,
	imageRef string,
	_ ...client.ImageInspectOption,
) (client.ImageInspectResult, error) {
	f.calls = append(f.calls, "inspect-image:"+imageRef)
	if len(f.imageInspectErrs) > 0 {
		err := f.imageInspectErrs[0]
		f.imageInspectErrs = f.imageInspectErrs[1:]
		return f.pulledImage, err
	}
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
	f.createOptions = append(f.createOptions, options)
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
