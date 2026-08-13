package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	digest "github.com/opencontainers/go-digest"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

type fakeImageUpdateCheckClient struct {
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
	f.imageCalls = append(f.imageCalls, imageID)
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
	f.distributionCalls = append(f.distributionCalls, imageRef)
	f.distributionOptions = append(f.distributionOptions, options)
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
