package dockerupdate

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	digest "github.com/opencontainers/go-digest"
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

	statuses, result, err := checkContainerImageUpdates(context.Background(), client, nil, targets, now)
	if err != nil {
		t.Fatalf("checkContainerImageUpdates: %v", err)
	}

	want := DockerUpdateCheckResult{Checked: 3, Updates: 2}
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
		statuses []ImageUpdateStatus
		err      error
	}
	done := make(chan checkResult, 1)
	go func() {
		statuses, _, err := checkContainerImageUpdates(context.Background(), client, nil, targets, time.Now())
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

	statuses, result, err := checkContainerImageUpdates(context.Background(), client, nil, targets, time.Now())
	if err != nil {
		t.Fatalf("checkContainerImageUpdates: %v", err)
	}
	want := DockerUpdateCheckResult{Checked: 4, Errors: 1, Uncheckable: 1}
	if result != want {
		t.Fatalf("result = %+v, want %+v", result, want)
	}
	if statuses[0].Err != "" || statuses[0].UpdateAvailable || statuses[0].LocalDigest != localDigest.String() {
		t.Fatalf("digest-pinned status = %+v", statuses[0])
	}
	if statuses[1].Err != "" || statuses[1].UpdateAvailable || statuses[1].LocalDigest != localDigest.String() {
		t.Fatalf("image-ID status = %+v", statuses[1])
	}
	if statuses[2].Err != "" || statuses[2].CheckState != DockerUpdateCheckStateUncheckable ||
		!strings.Contains(statuses[2].CheckReason, "no repository digest") {
		t.Fatalf("local-only status = %+v", statuses[2])
	}
	if statuses[3].CheckState != DockerUpdateCheckStateError ||
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

func TestInspectImageUpdateRegistryRetries(t *testing.T) {
	rateLimit := errors.New("Error response from daemon: toomanyrequests: retry-after: 787.973µs, allowed: 44000/minute")
	daemonTimeout := errors.New("Error response from daemon: Get \"https://lscr.io/v2/\": context deadline exceeded (Client.Timeout exceeded while awaiting headers)")
	tlsTimeout := errors.New("Error response from daemon: Get \"https://lscr.io/v2/\": net/http: TLS handshake timeout")
	dialTimeout := errors.New("Error response from daemon: Get \"https://lscr.io/v2/\": dial tcp 3.67.33.93:443: i/o timeout")
	refused := errors.New("Error response from daemon: Get \"https://lscr.io/v2/\": dial tcp 3.67.33.93:443: connect: connection refused")
	reset := errors.New("Error response from daemon: Get \"https://lscr.io/v2/\": read tcp 192.0.2.10:51234->3.67.33.93:443: read: connection reset by peer")
	denied := errors.New("unauthorized: authentication required")
	tests := []struct {
		name     string
		errors   []error
		wantErr  error
		wantWait time.Duration
	}{
		{name: "registry rate limit recovers", errors: []error{rateLimit, nil}, wantWait: 5 * time.Second},
		{name: "typed HTTP 429 recovers", errors: []error{fmt.Errorf("daemon: %w", errdefs.ErrResourceExhausted), nil}, wantWait: 5 * time.Second},
		{name: "plain HTTP 429 recovers", errors: []error{errors.New("Error response from daemon: Too Many Requests"), nil}, wantWait: 5 * time.Second},
		{name: "rate limit recovers on final attempt", errors: []error{rateLimit, rateLimit, rateLimit, rateLimit, rateLimit, nil}, wantWait: 155 * time.Second},
		{name: "persistent rate limit", errors: []error{rateLimit, rateLimit, rateLimit, rateLimit, rateLimit, rateLimit}, wantErr: rateLimit, wantWait: 155 * time.Second},
		{name: "daemon timeout recovers", errors: []error{daemonTimeout, nil}, wantWait: 5 * time.Second},
		{name: "typed timeout recovers", errors: []error{fmt.Errorf("daemon: %w", context.DeadlineExceeded), nil}, wantWait: 5 * time.Second},
		{name: "transport timeout recovers", errors: []error{&url.Error{Op: "Get", URL: "http://docker/distribution", Err: os.ErrDeadlineExceeded}, nil}, wantWait: 5 * time.Second},
		{name: "TLS handshake timeout recovers", errors: []error{tlsTimeout, nil}, wantWait: 5 * time.Second},
		{name: "dial timeout recovers", errors: []error{dialTimeout, nil}, wantWait: 5 * time.Second},
		{name: "refused connection recovers", errors: []error{refused, refused, nil}, wantWait: 15 * time.Second},
		{name: "reset connection recovers", errors: []error{reset, nil}, wantWait: 5 * time.Second},
		{name: "persistent daemon timeout", errors: []error{daemonTimeout, daemonTimeout, daemonTimeout, daemonTimeout, daemonTimeout, daemonTimeout}, wantErr: daemonTimeout, wantWait: 155 * time.Second},
		{name: "authentication failure", errors: []error{denied}, wantErr: denied},
		{name: "authentication failure after rate limit", errors: []error{rateLimit, denied}, wantErr: denied, wantWait: 5 * time.Second},
		{name: "missing image", errors: []error{errdefs.ErrNotFound}, wantErr: errdefs.ErrNotFound},
		{name: "canceled request", errors: []error{context.Canceled}, wantErr: context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				localDigest := digest.FromString("local")
				remoteDigest := digest.FromString("remote")
				const imageRef = "lscr.io/linuxserver/speedtest-tracker:latest"
				calls := 0
				cli := &fakeImageUpdateCheckClient{
					images: map[string]client.ImageInspectResult{
						"image-id": {InspectResponse: image.InspectResponse{
							RepoDigests: []string{"lscr.io/linuxserver/speedtest-tracker@" + localDigest.String()},
						}},
					},
					distributionHook: func(context.Context, string, client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
						calls++
						return distributionInspectResult(remoteDigest), tt.errors[calls-1]
					},
				}
				started := time.Now()
				observation, err := inspectImageUpdate(t.Context(), cli, "image-id", imageRef)
				if err != nil || !errors.Is(observation.err, tt.wantErr) {
					t.Fatalf("error = %v, observation error = %v, want %v", err, observation.err, tt.wantErr)
				}
				if tt.wantErr == nil && (!observation.updateAvailable || observation.remoteDigest != remoteDigest.String()) {
					t.Fatalf("successful observation = %+v", observation)
				}
				if calls != len(tt.errors) || len(cli.imageCalls) != 1 || time.Since(started) != tt.wantWait {
					t.Fatalf("registry attempts = %d, local inspections = %d, elapsed = %v; want %d, 1, %v",
						calls, len(cli.imageCalls), time.Since(started), len(tt.errors), tt.wantWait)
				}
			})
		})
	}
}

func TestInspectImageUpdateRegistryBackoffPreservesDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		localDigest := digest.FromString("local")
		const imageRef = "lscr.io/linuxserver/speedtest-tracker:latest"
		cli := &fakeImageUpdateCheckClient{
			images: map[string]client.ImageInspectResult{
				"image-id": {InspectResponse: image.InspectResponse{
					RepoDigests: []string{"lscr.io/linuxserver/speedtest-tracker@" + localDigest.String()},
				}},
			},
			distributionErrors: map[string]error{imageRef: errdefs.ErrResourceExhausted},
		}
		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()
		started := time.Now()
		observation, err := inspectImageUpdate(ctx, cli, "image-id", imageRef)
		if !errors.Is(err, context.DeadlineExceeded) || observation.err != nil {
			t.Fatalf("cancellation = %v, observation error = %v", err, observation.err)
		}
		if len(cli.distributionCalls) != 1 || time.Since(started) != 500*time.Millisecond {
			t.Fatalf("registry attempts = %d, elapsed = %v; want 1, 500ms", len(cli.distributionCalls), time.Since(started))
		}
	})
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
		nil,
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
	if statuses != nil || result != (DockerUpdateCheckResult{}) {
		t.Fatalf("partial result returned after cancellation: statuses=%+v result=%+v", statuses, result)
	}
}

func TestCheckContainerImageUpdatesPreservesCancellationWithoutRegistryWork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	statuses, result, err := checkContainerImageUpdates(
		ctx,
		&fakeImageUpdateCheckClient{},
		nil,
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
	if statuses != nil || result != (DockerUpdateCheckResult{}) {
		t.Fatalf("result after cancellation: statuses=%+v result=%+v", statuses, result)
	}
}

func TestCheckContainerImageUpdatesClassifiesBuiltImages(t *testing.T) {
	withTempUpdateStatusPath(t)
	localDigest := digest.FromString("local")
	client := &fakeImageUpdateCheckClient{
		images: map[string]client.ImageInspectResult{
			"built-id":  {InspectResponse: image.InspectResponse{}},
			"loaded-id": {InspectResponse: image.InspectResponse{}},
			"pulled-id": {InspectResponse: image.InspectResponse{RepoDigests: []string{"docker.io/library/nginx@" + localDigest.String()}}},
		},
		distributions: map[string]client.DistributionInspectResult{
			"docker.io/library/nginx:latest": distributionInspectResult(localDigest),
		},
	}
	targets := []containerImageUpdateTarget{
		{ContainerID: "app", ContainerName: "media-app-1", ImageID: "built-id", ImageRef: "media-app"},
		{ContainerID: "loaded", ContainerName: "loaded", ImageID: "loaded-id", ImageRef: "local:test"},
		{ContainerID: "web", ContainerName: "web", ImageID: "pulled-id", ImageRef: "nginx:latest"},
	}
	var asked [][]string
	classify := func(_ context.Context, uncheckable []containerImageUpdateTarget) map[string]string {
		var ids []string
		for _, target := range uncheckable {
			ids = append(ids, target.ContainerID)
		}
		asked = append(asked, ids)
		return map[string]string{"app": builtReason("media", "app")}
	}

	statuses, result, err := checkContainerImageUpdates(context.Background(), client, classify, targets, time.Now())
	if err != nil {
		t.Fatalf("checkContainerImageUpdates: %v", err)
	}
	// Only the images with no repository digest are classified, in one call.
	if len(asked) != 1 || strings.Join(asked[0], ",") != "app,loaded" {
		t.Fatalf("classifier asked about %v, want one call for app and loaded", asked)
	}
	if want := (DockerUpdateCheckResult{Built: 1, Checked: 3, Uncheckable: 1}); result != want {
		t.Fatalf("result = %+v, want %+v (built is counted on its own)", result, want)
	}
	if statuses[0].CheckState != DockerUpdateCheckStateBuilt || statuses[0].UpdateAvailable ||
		statuses[0].CheckReason != `built on this host by Compose service "app" of project "media"; rebuild it once from LinuxIO to be told when its base images have newer versions` {
		t.Fatalf("built status = %+v", statuses[0])
	}
	if statuses[1].CheckState != DockerUpdateCheckStateUncheckable || !strings.Contains(statuses[1].CheckReason, "no repository digest") {
		t.Fatalf("loaded status = %+v, want it uncheckable as before", statuses[1])
	}
	if statuses[2].CheckState != DockerUpdateCheckStateCurrent {
		t.Fatalf("pulled status = %+v, want current", statuses[2])
	}

	// A check with nothing uncheckable does not classify.
	asked = nil
	if _, _, err := checkContainerImageUpdates(context.Background(), client, classify, targets[2:], time.Now()); err != nil || asked != nil {
		t.Fatalf("check of a pulled image: error %v, classifier asked about %v", err, asked)
	}
}

func TestClassifyComposeBuiltReadsEachProjectOnce(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(composeFile, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := filepath.Join(dir, "args")
	script := "#!/bin/sh\necho \"$*\" >> " + args + "\n" +
		"case \"$*\" in\n" +
		"*'--project-name broken '*) echo 'yaml: line 3: did not find expected key' >&2; exit 1 ;;\n" +
		`*) echo '{"services": {"app": {"build": {"context": "."}}, "worker": {"build": {"context": "."}, "image": "media-worker"}, "cache": {"image": "redis:7"}}}' ;;` + "\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	labels := func(project, service string) map[string]string {
		return map[string]string{
			"com.docker.compose.project":              project,
			"com.docker.compose.service":              service,
			"com.docker.compose.project.config_files": composeFile,
			"com.docker.compose.project.working_dir":  dir,
		}
	}
	noService := labels("media", "")
	targets := []containerImageUpdateTarget{
		{ContainerID: "app", ContainerName: "media-app-1", Labels: labels("media", "app")},
		{ContainerID: "worker", ContainerName: "media-worker-1", Labels: labels("media", "worker")},
		{ContainerID: "cache", ContainerName: "media-cache-1", Labels: labels("media", "cache")},
		{ContainerID: "loaded", ContainerName: "loaded"},
		{ContainerID: "unnamed", ContainerName: "unnamed", Labels: noService},
		{ContainerID: "broken", ContainerName: "broken-app-1", Labels: labels("broken", "app")},
	}

	reasons := classifyComposeBuilt(nil)(t.Context(), targets)
	want := map[string]string{
		"app":    builtReason("media", "app"),
		"worker": builtReason("media", "worker"),
	}
	if !maps.Equal(reasons, want) {
		t.Fatalf("reasons = %v, want %v", reasons, want)
	}
	called, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(called)), "\n")
	if len(lines) != 2 || !strings.HasSuffix(lines[0], "config --format json app worker cache") ||
		!strings.Contains(lines[1], "--project-name broken ") {
		t.Fatalf("docker called with %q, want one config read per project", lines)
	}
}

// With the containerd image store a built image has a digest of its own, and
// the registry refuses to look it up: such a lookup is classified when the
// image's own labels name the container's Compose project and service, and a
// throttled one never is.
func TestCheckContainerImageUpdatesClassifiesRefusedLookups(t *testing.T) {
	withTempUpdateStatusPath(t)
	synctest.Test(t, func(t *testing.T) {
		localDigest := digest.FromString("local")
		denied := errors.New("Error response from daemon: errors:\ndenied: requested access to the resource is denied\nunauthorized: authentication required")
		throttled := errors.New("Error response from daemon: toomanyrequests: You have reached your pull rate limit")
		builtBy := func(service string) map[string]string {
			return map[string]string{"com.docker.compose.project": "media", "com.docker.compose.service": service}
		}
		ownDigest := func(id, name string, labels map[string]string) client.ImageInspectResult {
			inspect := imageWithLabels(t, id, labels)
			inspect.RepoDigests = []string{name + "@" + localDigest.String()}
			return client.ImageInspectResult{InspectResponse: inspect}
		}
		client := &fakeImageUpdateCheckClient{
			images: map[string]client.ImageInspectResult{
				"app-id":       ownDigest("app-id", "media-app", builtBy("app")),
				"nobuild-id":   ownDigest("nobuild-id", "media-nobuild", builtBy("nobuild")),
				"pulled-id":    ownDigest("pulled-id", "ghcr.io/org/app", map[string]string{"org.opencontainers.image.version": "1.2"}),
				"other-id":     ownDigest("other-id", "media-other", builtBy("elsewhere")),
				"throttled-id": ownDigest("throttled-id", "media-throttled", builtBy("throttled")),
			},
			distributionErrors: map[string]error{
				"docker.io/library/media-app:latest":       denied,
				"docker.io/library/media-nobuild:latest":   denied,
				"ghcr.io/org/app:latest":                   denied,
				"docker.io/library/media-other:latest":     denied,
				"docker.io/library/media-throttled:latest": throttled,
			},
		}
		targets := []containerImageUpdateTarget{
			{ContainerID: "app", ImageID: "app-id", ImageRef: "media-app", Labels: builtBy("app")},
			{ContainerID: "nobuild", ImageID: "nobuild-id", ImageRef: "media-nobuild", Labels: builtBy("nobuild")},
			{ContainerID: "pulled", ImageID: "pulled-id", ImageRef: "ghcr.io/org/app", Labels: builtBy("pulled")},
			{ContainerID: "other", ImageID: "other-id", ImageRef: "media-other", Labels: builtBy("other")},
			{ContainerID: "throttled", ImageID: "throttled-id", ImageRef: "media-throttled", Labels: builtBy("throttled")},
		}
		// Only app has a build section; the others would be built if asked.
		var asked []string
		classify := recordingClassifier(&asked, map[string]string{
			"app":       builtReason("media", "app"),
			"pulled":    builtReason("media", "pulled"),
			"other":     builtReason("media", "other"),
			"throttled": builtReason("media", "throttled"),
		})

		statuses, result, err := checkContainerImageUpdates(t.Context(), client, classify, targets, time.Now())
		if err != nil {
			t.Fatalf("checkContainerImageUpdates: %v", err)
		}
		// A pulled image, one built for another service, and a throttled
		// lookup are never classified.
		if strings.Join(asked, ",") != "app,nobuild" {
			t.Fatalf("classifier asked about %v, want the refused lookups of images built for their service", asked)
		}
		if statuses[0].CheckState != DockerUpdateCheckStateBuilt || statuses[0].Err != "" || statuses[0].UpdateAvailable {
			t.Fatalf("refused lookup of a built image = %+v, want built with no error", statuses[0])
		}
		for i, want := range []string{"denied", "denied", "denied", "toomanyrequests"} {
			status := statuses[i+1]
			if status.CheckState != DockerUpdateCheckStateError || !strings.Contains(status.Err, want) {
				t.Fatalf("%s = %+v, want the error as before", status.ContainerID, status)
			}
		}
		if want := (DockerUpdateCheckResult{Built: 1, Checked: 5, Errors: 4}); result != want {
			t.Fatalf("result = %+v, want %+v", result, want)
		}
	})
}

// Docker Hub's pull limit is asked once, typed (Engine 29.10) or not; any
// other throttling is asked again.
func TestRegistryDigestDoesNotRetryDockerHubPullLimit(t *testing.T) {
	for name, tc := range map[string]struct {
		err       error
		wantCalls int
	}{
		"hub limit":       {errors.New("toomanyrequests: You have reached your pull rate limit"), 1},
		"typed hub limit": {fmt.Errorf("You have reached your pull rate limit: %w", errdefs.ErrResourceExhausted), 1},
		"typed throttle":  {fmt.Errorf("slow down: %w", errdefs.ErrResourceExhausted), registryRetries + 1},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				calls := 0
				fake := &fakeImageUpdateCheckClient{
					distributionHook: func(context.Context, string, client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
						calls++
						return client.DistributionInspectResult{}, tc.err
					},
				}
				if _, err := registryDigest(t.Context(), fake, "docker.io/library/nginx:latest"); err == nil {
					t.Fatal("registryDigest succeeded, want the registry's error")
				}
				if calls != tc.wantCalls {
					t.Fatalf("registry asked %d times, want %d", calls, tc.wantCalls)
				}
			})
		})
	}
}

// A run of a Docker job is the engine's: it is not checked, so it has no
// status and counts for nothing.
func TestCheckContainerImageUpdatesLeavesOutDockerJobRuns(t *testing.T) {
	withTempUpdateStatusPath(t)
	localDigest := digest.FromString("local")
	fake := &fakeImageUpdateCheckClient{
		images: map[string]client.ImageInspectResult{
			"image-id": {InspectResponse: image.InspectResponse{
				RepoDigests: []string{"docker.io/library/nginx@" + localDigest.String()},
			}},
		},
		distributionHook: func(context.Context, string, client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
			return distributionInspectResult(localDigest), nil
		},
	}
	targets := []containerImageUpdateTarget{
		{ContainerID: "web", ContainerName: "web", ImageID: "image-id", ImageRef: "nginx"},
		{ContainerID: "run", ContainerName: "media-nightly-1", ImageID: "image-id", ImageRef: "nginx", Labels: map[string]string{dockerJobLabel: "nightly"}},
	}
	statuses, result, err := checkContainerImageUpdates(t.Context(), fake, nil, targets, time.Now())
	if err != nil {
		t.Fatalf("checkContainerImageUpdates: %v", err)
	}
	if len(statuses) != 1 || statuses[0].ContainerID != "web" || result.Checked != 1 {
		t.Fatalf("statuses = %+v, result = %+v; want the service alone", statuses, result)
	}
	if targets[1].ContainerID != "run" {
		t.Fatalf("the caller's targets changed: %+v", targets)
	}
}

// recordingClassifier answers reasons, and adds the ID of each target it is
// asked about to asked.
func recordingClassifier(asked *[]string, reasons map[string]string) builtClassifier {
	return func(_ context.Context, targets []containerImageUpdateTarget) map[string]string {
		for _, target := range targets {
			*asked = append(*asked, target.ContainerID)
		}
		return reasons
	}
}

func TestRegistryRefusal(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"access denied":      {errors.New("Error response from daemon: errors:\ndenied: requested access to the resource is denied"), true},
		"no credentials":     {errors.New("unauthorized: authentication required"), true},
		"typed not found":    {fmt.Errorf("daemon: %w", errdefs.ErrNotFound), true},
		"unknown manifest":   {errors.New("manifest unknown: manifest unknown"), true},
		"throttled":          {errors.New("toomanyrequests: retry-after: 1s"), false},
		"docker hub limit":   {errors.New("Error response from daemon: You have reached your pull rate limit"), false},
		"typed hub limit":    {fmt.Errorf("You have reached your pull rate limit: %w", errdefs.ErrResourceExhausted), false},
		"timed out":          {errors.New("dial tcp 3.67.33.93:443: i/o timeout"), false},
		"no such host":       {errors.New("dial tcp: lookup registry-1.docker.io: no such host"), false},
		"unreachable":        {errors.New("dial tcp 3.67.33.93:443: connect: network is unreachable"), false},
		"anything else":      {errors.New("unexpected EOF"), false},
		"refused connection": {errors.New("dial tcp 3.67.33.93:443: connect: connection refused"), false},
	} {
		if got := registryRefusal(tc.err); got != tc.want {
			t.Errorf("%s: registryRefusal(%q) = %v, want %v", name, tc.err, got, tc.want)
		}
	}
}

// One service no longer in the files fails the read of the whole project:
// the others are then read one by one, so they are still found built.
func TestClassifyComposeBuiltReadsServicesAloneWhenTheProjectReadFails(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(composeFile, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := filepath.Join(dir, "args")
	script := "#!/bin/sh\necho \"$*\" >> " + args + "\n" +
		"case \"$*\" in\n" +
		"*' gone'*) echo 'no such service: gone' >&2; exit 1 ;;\n" +
		"*' app') echo '{\"services\": {\"app\": {\"build\": {\"context\": \".\"}}}}' ;;\n" +
		"*' cache') echo '{\"services\": {\"cache\": {\"image\": \"redis:7\"}}}' ;;\n" +
		"esac\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	labels := func(service string) map[string]string {
		return map[string]string{
			"com.docker.compose.project":              "media",
			"com.docker.compose.service":              service,
			"com.docker.compose.project.config_files": composeFile,
			"com.docker.compose.project.working_dir":  dir,
		}
	}
	targets := []containerImageUpdateTarget{
		{ContainerID: "app", ContainerName: "media-app-1", Labels: labels("app")},
		{ContainerID: "gone", ContainerName: "media-gone-1", Labels: labels("gone")},
		{ContainerID: "cache", ContainerName: "media-cache-1", Labels: labels("cache")},
	}

	reasons := classifyComposeBuilt(nil)(t.Context(), targets)
	if want := map[string]string{"app": builtReason("media", "app")}; !maps.Equal(reasons, want) {
		t.Fatalf("reasons = %v, want %v", reasons, want)
	}
	called, _ := os.ReadFile(args)
	if lines := strings.Split(strings.TrimSpace(string(called)), "\n"); len(lines) != 4 {
		t.Fatalf("docker called with %q, want the project read, then each service alone", lines)
	}
}

// A service label that starts with a dash would reach docker compose as an
// option: the container is refused before any Compose command runs.
func TestComposeTargetRefusesAServiceThatReadsAsAnOption(t *testing.T) {
	dir := t.TempDir()
	composeFile := filepath.Join(dir, "compose.yaml")
	if err := os.WriteFile(composeFile, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	labels := map[string]string{
		"com.docker.compose.project":              "media",
		"com.docker.compose.service":              "--output=/etc/cron.d/x",
		"com.docker.compose.project.config_files": composeFile,
		"com.docker.compose.project.working_dir":  dir,
	}
	_, _, managed, err := composeTargetForLabels(t.Context(), nil, "media-x-1", labels)
	if !managed || err == nil || !strings.Contains(err.Error(), "would read as an option") {
		t.Fatalf("managed = %v, error = %v; want the service refused", managed, err)
	}

	// The check leaves such a container as it found it and runs nothing.
	t.Setenv("PATH", dir)
	if reasons := classifyComposeBuilt(nil)(t.Context(), []containerImageUpdateTarget{{ContainerID: "x", ContainerName: "media-x-1", Labels: labels}}); len(reasons) != 0 {
		t.Fatalf("reasons = %v, want none", reasons)
	}
}
