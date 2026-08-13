package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	digest "github.com/opencontainers/go-digest"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type fakeImageUpdateCheckClient struct {
	mu                  sync.Mutex
	images              map[string]client.ImageInspectResult
	imageErrors         map[string]error
	distributions       map[string]client.DistributionInspectResult
	distributionErrors  map[string]error
	imageCalls          []string
	distributionCalls   []string
	distributionOptions []client.DistributionInspectOptions
	distributionHook    func(context.Context, string, client.DistributionInspectOptions) (client.DistributionInspectResult, error)
}

func distributionInspectResult(value digest.Digest) client.DistributionInspectResult {
	result := client.DistributionInspectResult{}
	result.Descriptor.Digest = value
	return result
}

func (f *fakeImageUpdateCheckClient) ImageInspect(
	_ context.Context,
	imageID string,
	_ ...client.ImageInspectOption,
) (client.ImageInspectResult, error) {
	f.mu.Lock()
	f.imageCalls = append(f.imageCalls, imageID)
	f.mu.Unlock()
	if err := f.imageErrors[imageID]; err != nil {
		return client.ImageInspectResult{}, err
	}
	result, ok := f.images[imageID]
	if !ok {
		return client.ImageInspectResult{}, fmt.Errorf("unexpected image inspect %q", imageID)
	}
	return result, nil
}

func (f *fakeImageUpdateCheckClient) DistributionInspect(
	ctx context.Context,
	imageRef string,
	options client.DistributionInspectOptions,
) (client.DistributionInspectResult, error) {
	f.mu.Lock()
	f.distributionCalls = append(f.distributionCalls, imageRef)
	f.distributionOptions = append(f.distributionOptions, options)
	f.mu.Unlock()
	if f.distributionHook != nil {
		return f.distributionHook(ctx, imageRef, options)
	}
	if err := f.distributionErrors[imageRef]; err != nil {
		return client.DistributionInspectResult{}, err
	}
	result, ok := f.distributions[imageRef]
	if !ok {
		return client.DistributionInspectResult{}, fmt.Errorf("unexpected distribution inspect %q", imageRef)
	}
	return result, nil
}

type boundedImageUpdateCheckClient struct {
	mu      sync.Mutex
	active  int
	maximum int
	release chan struct{}
	started chan struct{}
}

func (f *boundedImageUpdateCheckClient) ImageInspect(
	_ context.Context,
	imageID string,
	_ ...client.ImageInspectOption,
) (client.ImageInspectResult, error) {
	digestValue := digest.FromString("local-" + imageID)
	return client.ImageInspectResult{
		RepoDigests: []string{"docker.io/library/" + imageID + "@" + digestValue.String()}}, nil
}

func (f *boundedImageUpdateCheckClient) DistributionInspect(
	ctx context.Context,
	imageRef string,
	_ client.DistributionInspectOptions,
) (client.DistributionInspectResult, error) {
	f.mu.Lock()
	f.active++
	if f.active > f.maximum {
		f.maximum = f.active
	}
	f.mu.Unlock()

	f.started <- struct{}{}
	select {
	case <-ctx.Done():
		f.finishRequest()
		return client.DistributionInspectResult{}, ctx.Err()
	case <-f.release:
	}
	f.finishRequest()
	return distributionInspectResult(digest.FromString("remote-" + imageRef)), nil
}

func (f *boundedImageUpdateCheckClient) finishRequest() {
	f.mu.Lock()
	f.active--
	f.mu.Unlock()
}

func TestCheckContainerImageUpdatesDetectsAndDeduplicates(t *testing.T) {
	localNginx := digest.FromString("local-nginx")
	remoteNginx := digest.FromString("remote-nginx")
	redisDigest := digest.FromString("redis")
	client := &fakeImageUpdateCheckClient{
		images: map[string]client.ImageInspectResult{
			"nginx-id": {
				InspectResponse: image.InspectResponse{
					RepoDigests: []string{"docker.io/library/nginx@" + localNginx.String()},
				},
			},
			"redis-id": {
				InspectResponse: image.InspectResponse{
					RepoDigests: []string{"docker.io/library/redis@" + redisDigest.String()},
				},
			},
		},
		distributions: map[string]client.DistributionInspectResult{
			"docker.io/library/nginx:latest": distributionInspectResult(remoteNginx),
			"docker.io/library/redis:7":      distributionInspectResult(redisDigest),
		},
	}
	targets := []containerImageUpdateTarget{
		{ContainerID: "c1", ContainerName: "nginx-1", ImageID: "nginx-id", ImageRef: "nginx"},
		{ContainerID: "c2", ContainerName: "nginx-2", ImageID: "nginx-id", ImageRef: "nginx:latest"},
		{ContainerID: "c3", ContainerName: "redis", ImageID: "redis-id", ImageRef: "redis:7"},
	}
	now := time.Now()

	statuses, result, err := checkContainerImageUpdates(context.Background(), client, targets, now)
	if err != nil {
		t.Fatalf("checkContainerImageUpdates: %v", err)
	}

	want := apischema.DockerUpdateCheckResult{Checked: 3, Updates: 2}
	if result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
	if len(statuses) != 3 {
		t.Fatalf("len(statuses) = %d, want 3", len(statuses))
	}
	if !statuses[0].UpdateAvailable || !statuses[1].UpdateAvailable || statuses[2].UpdateAvailable {
		t.Fatalf("update statuses = %+v", statuses)
	}
	if statuses[0].LocalDigest != localNginx.String() || statuses[0].RemoteDigest != remoteNginx.String() {
		t.Fatalf("nginx digests = %+v", statuses[0])
	}
	if statuses[2].LocalDigest != redisDigest.String() || statuses[2].RemoteDigest != redisDigest.String() {
		t.Fatalf("redis digests = %+v", statuses[2])
	}
	if len(client.distributionCalls) != 2 {
		t.Fatalf("distribution calls = %v, want two unique image checks", client.distributionCalls)
	}
	for _, options := range client.distributionOptions {
		if options.EncodedRegistryAuth != "" {
			t.Fatalf("registry auth = %q, want anonymous access", options.EncodedRegistryAuth)
		}
	}
}

func TestCheckContainerImageUpdatesBoundsParallelRegistryChecksAndPreservesOrder(t *testing.T) {
	client := &boundedImageUpdateCheckClient{
		release: make(chan struct{}),
		started: make(chan struct{}, 8),
	}
	targets := make([]containerImageUpdateTarget, 8)
	for i := range targets {
		name := fmt.Sprintf("image-%d", i)
		targets[i] = containerImageUpdateTarget{
			ContainerID:   fmt.Sprintf("container-%d", i),
			ContainerName: name,
			ImageID:       name,
			ImageRef:      name,
		}
	}

	type checkResult struct {
		statuses []imageUpdateStatus
		err      error
	}
	done := make(chan checkResult, 1)
	go func() {
		statuses, _, err := checkContainerImageUpdates(context.Background(), client, targets, time.Now())
		done <- checkResult{statuses: statuses, err: err}
	}()

	for range imageUpdateObservationConcurrency {
		select {
		case <-client.started:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for bounded registry checks to start")
		}
	}
	select {
	case <-client.started:
		t.Fatalf("more than %d registry checks started concurrently", imageUpdateObservationConcurrency)
	case <-time.After(50 * time.Millisecond):
	}
	close(client.release)

	var result checkResult
	select {
	case result = <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for registry checks to finish")
	}
	if result.err != nil {
		t.Fatalf("checkContainerImageUpdates: %v", result.err)
	}
	client.mu.Lock()
	maximum := client.maximum
	client.mu.Unlock()
	if maximum != imageUpdateObservationConcurrency {
		t.Fatalf("maximum parallel checks = %d, want %d", maximum, imageUpdateObservationConcurrency)
	}
	if len(result.statuses) != len(targets) {
		t.Fatalf("statuses = %d, want %d", len(result.statuses), len(targets))
	}
	for i, status := range result.statuses {
		if status.ContainerID != targets[i].ContainerID || status.ContainerName != targets[i].ContainerName {
			t.Fatalf("status %d = %+v, want target %+v", i, status, targets[i])
		}
	}
}

func TestCheckContainerImageUpdatesHandlesImmutableAndFailedChecks(t *testing.T) {
	localDigest := digest.FromString("local")
	remoteRef := "docker.io/library/broken:latest"
	client := &fakeImageUpdateCheckClient{
		images: map[string]client.ImageInspectResult{
			"local-only-id": {
				InspectResponse: image.InspectResponse{},
			},
			"broken-id": {
				InspectResponse: image.InspectResponse{
					RepoDigests: []string{"docker.io/library/broken@" + localDigest.String()},
				},
			},
		},
		distributions: map[string]client.DistributionInspectResult{},
		distributionErrors: map[string]error{
			remoteRef: errors.New("registry unavailable"),
		},
	}
	targets := []containerImageUpdateTarget{
		{ContainerID: "c1", ImageID: "pinned-id", ImageRef: "nginx@" + localDigest.String()},
		{ContainerID: "c2", ImageID: "digest-id", ImageRef: localDigest.String()},
		{ContainerID: "c3", ImageID: "local-only-id", ImageRef: "local:test"},
		{ContainerID: "c4", ImageID: "broken-id", ImageRef: "broken:latest"},
	}

	statuses, result, err := checkContainerImageUpdates(context.Background(), client, targets, time.Now())
	if err != nil {
		t.Fatalf("checkContainerImageUpdates: %v", err)
	}
	want := apischema.DockerUpdateCheckResult{Checked: 4, Errors: 1, Uncheckable: 1}
	if result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
	if statuses[0].Err != "" || statuses[0].UpdateAvailable || statuses[0].LocalDigest != localDigest.String() {
		t.Fatalf("digest-pinned status = %+v", statuses[0])
	}
	if statuses[1].Err != "" || statuses[1].UpdateAvailable || statuses[1].LocalDigest != localDigest.String() {
		t.Fatalf("image-ID status = %+v", statuses[1])
	}
	if statuses[2].Err != "" || statuses[2].CheckState != apischema.DockerUpdateCheckStateUncheckable ||
		!strings.Contains(statuses[2].CheckReason, "no repository digest") {
		t.Fatalf("local-only status = %+v", statuses[2])
	}
	if statuses[3].CheckState != apischema.DockerUpdateCheckStateError ||
		!strings.Contains(statuses[3].Err, "registry unavailable") {
		t.Fatalf("failed registry status = %+v", statuses[3])
	}
	if len(client.imageCalls) != 2 {
		t.Fatalf("image calls = %v, want only movable references", client.imageCalls)
	}
	if len(client.distributionCalls) != 1 || client.distributionCalls[0] != remoteRef {
		t.Fatalf("distribution calls = %v, want [%s]", client.distributionCalls, remoteRef)
	}
}

func TestCheckContainerImageUpdatesPreservesCancellation(t *testing.T) {
	localDigest := digest.FromString("local")
	ctx, cancel := context.WithCancel(context.Background())
	client := &fakeImageUpdateCheckClient{
		images: map[string]client.ImageInspectResult{
			"image-id": {
				InspectResponse: image.InspectResponse{
					RepoDigests: []string{"docker.io/library/nginx@" + localDigest.String()},
				},
			},
		},
		distributionHook: func(context.Context, string, client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
			cancel()
			return client.DistributionInspectResult{}, context.Canceled
		},
	}

	statuses, result, err := checkContainerImageUpdates(
		ctx,
		client,
		[]containerImageUpdateTarget{{
			ContainerID: "c1",
			ImageID:     "image-id",
			ImageRef:    "nginx",
		}},
		time.Now(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if statuses != nil || result != (apischema.DockerUpdateCheckResult{}) {
		t.Fatalf("partial result returned after cancellation: statuses=%+v result=%+v", statuses, result)
	}
}

func TestCheckContainerImageUpdatesPreservesCancellationWithoutRegistryWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	statuses, result, err := checkContainerImageUpdates(
		ctx,
		&fakeImageUpdateCheckClient{},
		[]containerImageUpdateTarget{{
			ContainerID: "immutable",
			ImageID:     "sha256:local",
			ImageRef:    "nginx@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		}},
		time.Now(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if statuses != nil || result != (apischema.DockerUpdateCheckResult{}) {
		t.Fatalf("result after cancellation: statuses=%+v result=%+v", statuses, result)
	}
}
