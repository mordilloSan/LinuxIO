package dockerupdate

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"iter"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/jsonstream"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

func updateStandaloneForTest(
	ctx context.Context,
	cli ContainerClient,
	before container.InspectResponse,
	imageRef string,
	result DockerContainerUpdateResult,
) (DockerContainerUpdateResult, error) {
	return updateStandaloneContainer(ctx, cli, before, imageRef, result, nil, nil, stoppedContainerUpdatePolicy{}, nil)
}

func TestComposeCommandArgsPreservesOverrideOrder(t *testing.T) {
	target := ComposeTarget{
		Name:             "media",
		ConfigFiles:      []string{"/srv/media/compose.yml", "/srv/media/compose.prod.yml"},
		EnvironmentFiles: []string{"/srv/media/.env", "/srv/media/production.env"},
		WorkingDir:       "/srv/media",
	}
	got, err := composeCommandArgs(target, "json", "up", "-d", "--no-deps", "server")
	if err != nil {
		t.Fatalf("composeCommandArgs: %v", err)
	}
	want := []string{
		"compose", "--progress=json", "--project-name", "media",
		"--project-directory", "/srv/media",
		"--env-file", "/srv/media/.env",
		"--env-file", "/srv/media/production.env",
		"--file", "/srv/media/compose.yml",
		"--file", "/srv/media/compose.prod.yml",
		"up", "-d", "--no-deps", "server",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("composeCommandArgs = %#v, want %#v", got, want)
	}
}

func TestComposeCommandArgsRejectsIncompleteTarget(t *testing.T) {
	if _, err := composeCommandArgs(ComposeTarget{}, "json", "up"); err == nil {
		t.Fatal("composeCommandArgs accepted an empty project")
	}
	if _, err := composeCommandArgs(ComposeTarget{Name: "media"}, "json", "up"); err == nil {
		t.Fatal("composeCommandArgs accepted a project without config files")
	}
	if _, err := composeCommandArgs(ComposeTarget{Name: "media", ConfigFiles: []string{"compose.yml"}, EnvironmentFiles: []string{""}}, "json", "up"); err == nil {
		t.Fatal("composeCommandArgs accepted an empty environment file path")
	}
}

func TestComposePullServicesRetriesTransientRegistryFailures(t *testing.T) {
	// The exact line `docker compose pull` printed when GHCR throttled a scheduled run.
	const ghcrThrottle = `{"error":true,"message":"Error response from daemon: error from registry: retry-after: 449.12µs, allowed: 44000/minute"}`
	tests := []struct {
		name      string
		failures  int
		output    string
		wantPulls int
		wantWait  time.Duration
		wantErr   bool
	}{
		{name: "throttle recovers", failures: 2, output: ghcrThrottle, wantPulls: 3, wantWait: 15 * time.Second},
		{name: "persistent throttle", failures: 99, output: ghcrThrottle, wantPulls: 6, wantWait: 155 * time.Second, wantErr: true},
		{name: "missing image", failures: 99, output: "Error response from daemon: manifest unknown", wantPulls: 1, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			pulls := filepath.Join(dir, "pulls")
			script := fmt.Sprintf("#!/bin/sh\nn=0\n[ -f %[1]q ] && read n < %[1]q\nn=$((n + 1))\necho $n > %[1]q\n[ $n -le %[2]d ] || exit 0\necho '%[3]s'\nexit 1\n", pulls, tt.failures, tt.output)
			if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
				t.Fatalf("write fake docker: %v", err)
			}
			t.Setenv("PATH", dir)

			synctest.Test(t, func(t *testing.T) {
				collector := &ComposeMessageCollector{}
				started := time.Now()
				err := composePullServices(t.Context(), ComposeTarget{Name: "immich", ConfigFiles: []string{"compose.yml"}}, []string{"immich-server"}, collector.Emit)
				if (err != nil) != tt.wantErr || time.Since(started) != tt.wantWait {
					t.Fatalf("error = %v, elapsed = %v; want error %v, elapsed %v\n%s", err, time.Since(started), tt.wantErr, tt.wantWait, collector)
				}
			})
			got, err := os.ReadFile(pulls)
			if err != nil || strings.TrimSpace(string(got)) != fmt.Sprint(tt.wantPulls) {
				t.Fatalf("pull attempts = %q (%v), want %d", got, err, tt.wantPulls)
			}
		})
	}
}

// A cancelled Compose command gets SIGTERM, as systemd's stop gave it before
// the pass's unit signalled the program alone, so Compose stops what it
// started; it is killed only after composeStopDelay.
func TestRunComposeProjectStopsComposeWithSIGTERM(t *testing.T) {
	dir := t.TempDir()
	stopped := filepath.Join(dir, "stopped")
	script := "#!/bin/sh\ntrap 'echo term > " + stopped + "; exit 0' TERM\necho ready\nwhile :; do :; done\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ready := make(chan struct{})
	var once sync.Once
	done := make(chan error, 1)
	go func() {
		done <- RunComposeProject(ctx, ComposeTarget{Name: "media", ConfigFiles: []string{"compose.yml"}}, func(_, message string, _ *ComposeProgress) {
			if message == "ready" {
				once.Do(func() { close(ready) })
			}
		}, "up", "-d")
	}()
	<-ready
	cancel()
	if err := <-done; err == nil {
		t.Fatal("a cancelled Compose command reported success")
	}
	if got, err := os.ReadFile(stopped); err != nil || strings.TrimSpace(string(got)) != "term" {
		t.Fatalf("stopped = %q, %v; want Compose to get SIGTERM", got, err)
	}
	cmd := composeCommand(t.Context(), ComposeTarget{Name: "media"}, nil)
	if cmd.Cancel == nil || cmd.WaitDelay != composeStopDelay {
		t.Fatalf("cancel set %v, wait delay %v; want SIGTERM then a kill after %v", cmd.Cancel != nil, cmd.WaitDelay, composeStopDelay)
	}
}

func TestComposeCommandEnvironmentExcludesUntrackedInterpolationVariables(t *testing.T) {
	got := filterComposeCommandEnvironment([]string{
		"PATH=/usr/bin",
		"HOME=/root",
		"DOCKER_CONFIG=/root/.docker",
		"LC_ALL=C",
		"IMAGE_TAG=untracked",
		"COMPOSE_ENV_FILES=/tmp/untracked.env",
	})
	want := []string{
		"PATH=/usr/bin",
		"HOME=/root",
		"DOCKER_CONFIG=/root/.docker",
		"LC_ALL=C",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("filterComposeCommandEnvironment = %#v, want %#v", got, want)
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
		context.Background(), fake, composeRollback{}, before,
		ComposeTarget{Name: "project", ConfigFiles: []string{"compose.yml"}},
		"web", updateResultFor(before),
		func(context.Context, ComposeTarget, string, ComposeLineEmitter) error { return nil },
		nil,
	)
	if err != nil {
		t.Fatalf("updateComposeContainerWithRunner: %v", err)
	}
	if result.Updated || result.NewImageID != before.Image || result.ContainerID != after.ID {
		t.Fatalf("result = %+v, want successful current outcome", result)
	}
	status, ok := ReadStatusSnapshot().ForContainer(after.ID)
	if !ok || status.CheckState != DockerUpdateCheckStateCurrent {
		t.Fatalf("status = %+v, %v; want current", status, ok)
	}
}

func TestContainerReadyClassification(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	runningSince := func(started string) container.InspectResponse {
		return container.InspectResponse{State: &container.State{Status: "running", Running: true, StartedAt: started}}
	}
	tests := []struct {
		name     string
		inspect  container.InspectResponse
		ready    bool
		terminal bool
	}{
		// Without a health check, a container is ready once it has run as
		// long as Docker takes to count a start as successful.
		{name: "running without health, just started", inspect: runningSince(now.Add(-time.Second).Format(time.RFC3339Nano))},
		{name: "running without health, settled", inspect: runningSince(now.Add(-containerSettleTime).Format(time.RFC3339Nano)), ready: true},
		{name: "running without health, start unknown", inspect: runningSince("")},
		{name: "missing state", inspect: container.InspectResponse{}, terminal: true},
		{name: "created", inspect: container.InspectResponse{State: &container.State{Status: "created"}}},
		{name: "restarting", inspect: container.InspectResponse{State: &container.State{Status: "restarting"}}},
		{name: "exited", inspect: container.InspectResponse{State: &container.State{Status: "exited"}}, terminal: true},
		{name: "removing", inspect: container.InspectResponse{State: &container.State{Status: "removing"}}, terminal: true},
		{name: "dead", inspect: container.InspectResponse{State: &container.State{Status: "dead"}}, terminal: true},
		{name: "paused", inspect: container.InspectResponse{State: &container.State{Status: "running", Running: true, Paused: true}}, terminal: true},
		{name: "restarting flag", inspect: container.InspectResponse{State: &container.State{Status: "running", Running: true, Restarting: true}}},
		{name: "starting health", inspect: container.InspectResponse{State: &container.State{Status: "running", Running: true, Health: &container.Health{Status: "starting"}}}},
		{name: "unknown health", inspect: container.InspectResponse{State: &container.State{Status: "running", Running: true, Health: &container.Health{Status: "unknown"}}}},
		{name: "healthy", inspect: readyContainer("ready", "sha256:image"), ready: true},
		{name: "unhealthy", inspect: container.InspectResponse{State: &container.State{Status: "running", Running: true, Health: &container.Health{Status: "unhealthy"}}}, terminal: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ready, terminal, err := containerReady(tc.inspect, now, containerSettleTime)
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
	got, err := waitForContainerReadyWithTiming(context.Background(), client, "replacement", time.Second, time.Millisecond, containerSettleTime)
	if err != nil {
		t.Fatalf("waitForContainerReadyWithTiming: %v", err)
	}
	if got.ID != "replacement" || len(client.results) != 0 {
		t.Fatalf("result = %+v, remaining inspections = %d", got, len(client.results))
	}
}

func TestWaitForContainerReadyTimeoutNamesLastState(t *testing.T) {
	starting := container.InspectResponse{
		ID:           "replacement",
		RestartCount: 1,
		State:        &container.State{Status: "running", Running: true, Health: &container.Health{Status: "starting"}},
	}
	client := readinessClientFunc(func(context.Context) (container.InspectResponse, error) { return starting, nil })
	_, err := waitForContainerReadyWithTiming(context.Background(), client, "replacement", 20*time.Millisecond, time.Millisecond, containerSettleTime)
	// The bridge task runner replaces a deadline error with "operation timed
	// out", which would hide the state from the person who started the update.
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want a timeout that is not a context deadline error", err)
	}
	for _, want := range []string{"not ready after 20ms", `last state "running" (restart count 1, health "starting")`} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to contain %q", err, want)
		}
	}
}

func TestWaitForContainerReadyTimeoutDuringInspectNamesLastState(t *testing.T) {
	calls := 0
	client := readinessClientFunc(func(ctx context.Context) (container.InspectResponse, error) {
		calls++
		if calls == 1 {
			return container.InspectResponse{State: &container.State{Status: "created"}}, nil
		}
		<-ctx.Done()
		return container.InspectResponse{}, ctx.Err()
	})
	_, err := waitForContainerReadyWithTiming(context.Background(), client, "replacement", 20*time.Millisecond, time.Millisecond, containerSettleTime)
	if err == nil || !strings.Contains(err.Error(), `not ready after 20ms, last state "created"`) || strings.Contains(err.Error(), "exit code") {
		t.Fatalf("error = %v, want a timeout naming the created state without an exit code", err)
	}
}

func TestWaitForContainerReadyKeepsDaemonErrorAtTimeout(t *testing.T) {
	daemonDown := errors.New("cannot connect to the Docker daemon")
	client := readinessClientFunc(func(ctx context.Context) (container.InspectResponse, error) {
		<-ctx.Done()
		return container.InspectResponse{}, daemonDown
	})
	_, err := waitForContainerReadyWithTiming(context.Background(), client, "replacement", 20*time.Millisecond, time.Millisecond, containerSettleTime)
	if !errors.Is(err, daemonDown) {
		t.Fatalf("error = %v, want the daemon's error kept", err)
	}
}

func TestWaitForContainerReadyReturnsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := readinessClientFunc(func(context.Context) (container.InspectResponse, error) {
		cancel()
		return container.InspectResponse{State: &container.State{Status: "created"}}, nil
	})
	_, err := waitForContainerReadyWithTiming(ctx, client, "replacement", time.Minute, time.Millisecond, containerSettleTime)
	if !errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "not ready after") {
		t.Fatalf("error = %v, want the caller's cancellation alone", err)
	}
}

func TestWaitForContainerReadyStopsWhenContainerKeepsRestarting(t *testing.T) {
	restarting := func(restarts int) container.InspectResponse {
		return container.InspectResponse{
			ID:           "replacement",
			RestartCount: restarts,
			State:        &container.State{Status: "restarting", Running: true, Restarting: true, ExitCode: 127},
		}
	}
	client := &sequenceReadinessClient{results: []container.InspectResponse{
		{ID: "replacement", State: &container.State{Status: "created"}},
		restarting(1),
		restarting(2),
		restarting(3),
		readyContainer("replacement", "sha256:image"),
	}}
	_, err := waitForContainerReadyWithTiming(context.Background(), client, "replacement", time.Minute, time.Millisecond, containerSettleTime)
	if err == nil {
		t.Fatal("a container in a restart loop was reported ready")
	}
	for _, want := range []string{"keeps restarting: 3 restarts while waiting", `last state "restarting" (exit code 127, restart count 3)`} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to contain %q", err, want)
		}
	}
	if len(client.results) != 1 {
		t.Fatalf("remaining inspections = %d, want the wait to stop at the third restart", len(client.results))
	}
}

func TestWaitForContainerReadyRefusesRunningContainerInRestartLoop(t *testing.T) {
	running := func(restarts int) container.InspectResponse {
		return container.InspectResponse{
			ID:           "replacement",
			RestartCount: restarts,
			State:        &container.State{Status: "running", Running: true},
		}
	}
	client := &sequenceReadinessClient{results: []container.InspectResponse{
		{ID: "replacement", State: &container.State{Status: "created"}},
		running(3),
	}}
	_, err := waitForContainerReadyWithTiming(context.Background(), client, "replacement", time.Minute, time.Millisecond, containerSettleTime)
	if err == nil || !strings.Contains(err.Error(), "keeps restarting") {
		t.Fatalf("error = %v, want a running container between two crashes refused", err)
	}
}

func TestWaitForContainerReadyCountsRestartsFromFirstRead(t *testing.T) {
	earlier := container.InspectResponse{
		ID:           "kept",
		RestartCount: 7,
		State:        &container.State{Status: "running", Running: true, Health: &container.Health{Status: "starting"}},
	}
	ready := readyContainer("kept", "sha256:image")
	ready.RestartCount = 9
	client := &sequenceReadinessClient{results: []container.InspectResponse{earlier, ready}}
	got, err := waitForContainerReadyWithTiming(context.Background(), client, "kept", time.Second, time.Millisecond, containerSettleTime)
	if err != nil || got.ID != "kept" {
		t.Fatalf("result = %+v, %v; want restarts before the wait ignored", got, err)
	}
}

// A container without a health check that crashes a few seconds after it
// starts must not pass the wait: it is ready only once it has run for the
// settle time since its last start.
func TestWaitForContainerReadyWaitsForAContainerWithoutHealthCheckToSettle(t *testing.T) {
	for name, tc := range map[string]struct {
		restartAt time.Duration // when Docker restarts it; 0 for never
		want      time.Duration
	}{
		"runs on":                  {want: containerSettleTime},
		"restarts during the wait": {restartAt: 5 * time.Second, want: 5*time.Second + containerSettleTime},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				began := time.Now()
				client := readinessClientFunc(func(context.Context) (container.InspectResponse, error) {
					started, restarts := began, 0
					if tc.restartAt > 0 && time.Since(began) >= tc.restartAt {
						started, restarts = began.Add(tc.restartAt), 1
					}
					return container.InspectResponse{
						ID:           "replacement",
						RestartCount: restarts,
						State:        &container.State{Status: "running", Running: true, StartedAt: started.Format(time.RFC3339Nano)},
					}, nil
				})
				_, err := waitForContainerReadyWithTiming(t.Context(), client, "replacement", time.Minute, containerReadyPoll, containerSettleTime)
				if err != nil || time.Since(began) != tc.want {
					t.Fatalf("ready after %v, %v; want ready after %v", time.Since(began), err, tc.want)
				}
			})
		})
	}
}

// A container that has run longer than the settle time, such as a Compose
// service that was not recreated, is ready on the first read.
func TestWaitForContainerReadyAcceptsAContainerThatRanLongEnough(t *testing.T) {
	client := readinessClientFunc(func(context.Context) (container.InspectResponse, error) {
		return container.InspectResponse{
			ID:    "kept",
			State: &container.State{Status: "running", Running: true, StartedAt: time.Now().Add(-time.Hour).Format(time.RFC3339Nano)},
		}, nil
	})
	if _, err := waitForContainerReadyWithTiming(t.Context(), client, "kept", time.Second, time.Millisecond, time.Hour+time.Minute); err == nil {
		t.Fatal("a container that has not run the settle time was reported ready")
	}
	if _, err := waitForContainerReadyWithTiming(t.Context(), client, "kept", time.Second, time.Millisecond, containerSettleTime); err != nil {
		t.Fatalf("a container that ran an hour: %v", err)
	}
}

// justStarted is a running container without a health check that started at
// the current time.
func justStarted(id, imageID string) container.InspectResponse {
	inspect := readyContainer(id, imageID)
	inspect.State.Health = nil
	inspect.State.StartedAt = time.Now().Format(time.RFC3339Nano)
	return inspect
}

// The settle time is for a container that replaces a running one. A container
// created and started on the Containers page, or one-shot work that exits
// within seconds, keeps the old readiness: running on the first read.
func TestWaitForContainerReadyDoesNotSettleAFreshContainer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		began := time.Now()
		for _, inspect := range []container.InspectResponse{justStarted("created", "sha256:image"), {ID: "created", State: &container.State{Status: "running", Running: true}}} {
			client := readinessClientFunc(func(context.Context) (container.InspectResponse, error) { return inspect, nil })
			if _, err := WaitForContainerReady(t.Context(), client, "created"); err != nil || time.Since(began) != 0 {
				t.Fatalf("ready after %v, %v; want ready on the first read", time.Since(began), err)
			}
		}
	})
}

// Readiness ends in a failure of the container itself (exited, dead,
// unhealthy, restarting again and again) or without a verdict (out of time,
// paused, a state that cannot be read); only the first is one a rollback
// answers.
func TestWaitForContainerReadyMarksAFailureOfTheContainer(t *testing.T) {
	running := func(restarts int) container.InspectResponse {
		return container.InspectResponse{ID: "c", RestartCount: restarts, State: &container.State{Status: "running", Running: true, Health: &container.Health{Status: "starting"}}}
	}
	for name, tc := range map[string]struct {
		results []container.InspectResponse
		failed  bool
	}{
		"exited":           {results: []container.InspectResponse{{ID: "c", State: &container.State{Status: "exited", ExitCode: 1}}}, failed: true},
		"dead":             {results: []container.InspectResponse{{ID: "c", State: &container.State{Status: "dead", Dead: true}}}, failed: true},
		"unhealthy":        {results: []container.InspectResponse{{ID: "c", State: &container.State{Status: "running", Running: true, Health: &container.Health{Status: "unhealthy"}}}}, failed: true},
		"keeps restarting": {results: []container.InspectResponse{running(0), running(1), running(2), running(3)}, failed: true},
		"paused":           {results: []container.InspectResponse{{ID: "c", State: &container.State{Status: "running", Running: true, Paused: true}}}},
		"out of time":      {},
	} {
		t.Run(name, func(t *testing.T) {
			var client containerReadinessClient = &sequenceReadinessClient{results: tc.results}
			timeout := time.Minute
			if tc.results == nil {
				client = readinessClientFunc(func(context.Context) (container.InspectResponse, error) { return running(0), nil })
				timeout = 20 * time.Millisecond
			}
			_, err := waitForContainerReadyWithTiming(t.Context(), client, "c", timeout, time.Millisecond, containerSettleTime)
			if err == nil || errors.As(err, new(containerFailedError)) != tc.failed {
				t.Fatalf("error = %v; want a failure of the container: %v", err, tc.failed)
			}
		})
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

func TestStandaloneCreateOptionsPreservesHealthcheck(t *testing.T) {
	inspect := standaloneTestInspect()
	inspect.Config.Healthcheck = &container.HealthConfig{
		Test:     []string{"CMD", "true"},
		Interval: 30 * time.Second,
		Timeout:  5 * time.Second,
	}

	options, err := StandaloneCreateOptions(inspect, "nginx:latest", "web")
	if err != nil {
		t.Fatalf("StandaloneCreateOptions: %v", err)
	}
	if !reflect.DeepEqual(options.Config.Healthcheck, inspect.Config.Healthcheck) {
		t.Fatalf("healthcheck = %#v, want %#v", options.Config.Healthcheck, inspect.Config.Healthcheck)
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

	options, err := StandaloneCreateOptions(inspect, "docker.io/library/nginx:latest", "web")
	if err != nil {
		t.Fatalf("StandaloneCreateOptions: %v", err)
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

	options, err := StandaloneCreateOptions(inspect, "docker.io/library/nginx:latest", "web")
	if err != nil {
		t.Fatalf("StandaloneCreateOptions: %v", err)
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

			options, err := StandaloneCreateOptions(inspect, "docker.io/library/nginx:latest", "web")
			if err != nil {
				t.Fatalf("StandaloneCreateOptions: %v", err)
			}
			if options.Config.Hostname != tc.want {
				t.Fatalf("hostname = %q, want %q", options.Config.Hostname, tc.want)
			}
		})
	}
}

// An inspected container's labels include its image's. The replacement must
// not carry the old image's as its own, or they would override the new
// image's through every later update; the labels set on the container stay.
func TestUpdateStandaloneContainerDropsTheOldImageLabels(t *testing.T) {
	withTempUpdateStatusPath(t)
	before := standaloneTestInspect()
	before.Config.Labels = map[string]string{
		"org.opencontainers.image.version": "1.0.0",
		"org.opencontainers.image.title":   "set on the container",
		"traefik.enable":                   "true",
	}
	fake := newStandaloneUpdateFake()
	fake.images = map[string]image.InspectResponse{
		before.Image: imageWithLabels(t, before.Image, map[string]string{
			"org.opencontainers.image.version": "1.0.0",
			"org.opencontainers.image.title":   "nginx",
		}),
	}

	if _, err := updateStandaloneForTest(t.Context(), fake, before, "docker.io/library/nginx:latest", updateResultFor(before)); err != nil {
		t.Fatalf("updateStandaloneForTest: %v", err)
	}
	want := map[string]string{"org.opencontainers.image.title": "set on the container", "traefik.enable": "true"}
	if len(fake.createOptions) != 1 || !reflect.DeepEqual(fake.createOptions[0].Config.Labels, want) {
		t.Fatalf("replacement labels = %v, want %v", fake.createOptions[0].Config.Labels, want)
	}
	if before.Config.Labels["org.opencontainers.image.version"] != "1.0.0" {
		t.Fatal("the update changed the inspected container's labels")
	}
}

// imageConfigInspector answers ImageInspect with an image whose
// configuration is config (and a health check), or with err.
type imageConfigInspector struct {
	t           *testing.T
	config      map[string]any
	healthcheck *container.HealthConfig
	err         error
}

func (f imageConfigInspector) ImageInspect(_ context.Context, imageID string, _ ...client.ImageInspectOption) (client.ImageInspectResult, error) {
	if f.err != nil {
		return client.ImageInspectResult{}, f.err
	}
	data, err := json.Marshal(map[string]any{"Id": imageID, "Config": f.config})
	if err != nil {
		f.t.Fatal(err)
	}
	var inspect image.InspectResponse
	if err := json.Unmarshal(data, &inspect); err != nil {
		f.t.Fatal(err)
	}
	inspect.Config.Healthcheck = f.healthcheck
	return client.ImageInspectResult{InspectResponse: inspect}, nil
}

func oldImageDefaults(t *testing.T) imageConfigInspector {
	return imageConfigInspector{
		t: t,
		config: map[string]any{
			"Labels":       map[string]string{"org.opencontainers.image.version": "1.0"},
			"Env":          []string{"PATH=/old/bin", "LANG=C"},
			"Cmd":          []string{"serve"},
			"Entrypoint":   []string{"/entry"},
			"WorkingDir":   "/app",
			"User":         "app",
			"StopSignal":   "SIGQUIT",
			"ExposedPorts": map[string]any{"80/tcp": map[string]any{}},
			"Volumes":      map[string]any{"/data": map[string]any{}},
		},
		healthcheck: &container.HealthConfig{Test: []string{"CMD", "check"}, Interval: 30 * time.Second},
	}
}

// An inspected container's configuration is its image's merged with what was
// set for it, so a replacement from another image must not carry the old
// image's values as its own: each value equal to the old image's goes.
func TestDropImageDefaultsDropsWhatTheOldImageSet(t *testing.T) {
	for name, tc := range map[string]struct {
		set     func(*container.Config)
		dropped func(container.Config) bool
	}{
		"label":       {func(c *container.Config) { c.Labels = map[string]string{"org.opencontainers.image.version": "1.0"} }, func(c container.Config) bool { return len(c.Labels) == 0 }},
		"env entry":   {func(c *container.Config) { c.Env = []string{"PATH=/old/bin"} }, func(c container.Config) bool { return len(c.Env) == 0 }},
		"working dir": {func(c *container.Config) { c.WorkingDir = "/app" }, func(c container.Config) bool { return c.WorkingDir == "" }},
		"user":        {func(c *container.Config) { c.User = "app" }, func(c container.Config) bool { return c.User == "" }},
		"stop signal": {func(c *container.Config) { c.StopSignal = "SIGQUIT" }, func(c container.Config) bool { return c.StopSignal == "" }},
		"health check": {func(c *container.Config) {
			c.Healthcheck = &container.HealthConfig{Test: []string{"CMD", "check"}, Interval: 30 * time.Second}
		}, func(c container.Config) bool { return c.Healthcheck == nil }},
		"exposed port": {func(c *container.Config) { c.ExposedPorts = network.PortSet{network.MustParsePort("80/tcp"): {}} }, func(c container.Config) bool { return len(c.ExposedPorts) == 0 }},
		"volume":       {func(c *container.Config) { c.Volumes = map[string]struct{}{"/data": {}} }, func(c container.Config) bool { return len(c.Volumes) == 0 }},
	} {
		t.Run(name, func(t *testing.T) {
			options := client.ContainerCreateOptions{Config: &container.Config{}, HostConfig: &container.HostConfig{}}
			tc.set(options.Config)
			if err := DropImageDefaults(t.Context(), oldImageDefaults(t), &options, "sha256:old"); err != nil {
				t.Fatalf("DropImageDefaults: %v", err)
			}
			if !tc.dropped(*options.Config) {
				t.Fatalf("config = %+v, want the old image's %s dropped", options.Config, name)
			}
		})
	}
}

// Docker fills in the image's Cmd only when the container's Entrypoint is
// empty, so the two are dropped together, following that rule.
func TestDropImageDefaultsFollowsDockersRuleForCmdAndEntrypoint(t *testing.T) {
	for name, tc := range map[string]struct {
		entrypoint, cmd         []string
		wantEntrypoint, wantCmd []string
	}{
		"both the image's":                      {entrypoint: []string{"/entry"}, cmd: []string{"serve"}},
		"--entrypoint with the image's CMD":     {entrypoint: []string{"/custom"}, cmd: []string{"serve"}, wantEntrypoint: []string{"/custom"}, wantCmd: []string{"serve"}},
		"an edit that changes the entrypoint":   {entrypoint: []string{"/other"}, cmd: []string{"serve"}, wantEntrypoint: []string{"/other"}, wantCmd: []string{"serve"}},
		"an edit that clears the command":       {entrypoint: []string{"/entry"}, cmd: []string{}, wantEntrypoint: []string{"/entry"}, wantCmd: []string{}},
		"the image's entrypoint, its own CMD":   {entrypoint: []string{"/entry"}, cmd: []string{"serve", "--debug"}, wantCmd: []string{"serve", "--debug"}},
		"its own entrypoint, the image's empty": {entrypoint: []string{"/custom"}, cmd: nil, wantEntrypoint: []string{"/custom"}},
	} {
		t.Run(name, func(t *testing.T) {
			options := client.ContainerCreateOptions{Config: &container.Config{Entrypoint: tc.entrypoint, Cmd: tc.cmd}, HostConfig: &container.HostConfig{}}
			if err := DropImageDefaults(t.Context(), oldImageDefaults(t), &options, "sha256:old"); err != nil {
				t.Fatalf("DropImageDefaults: %v", err)
			}
			if got := options.Config; !reflect.DeepEqual(got.Entrypoint, tc.wantEntrypoint) || !reflect.DeepEqual(got.Cmd, tc.wantCmd) {
				t.Fatalf("entrypoint %#v, cmd %#v; want %#v, %#v", got.Entrypoint, got.Cmd, tc.wantEntrypoint, tc.wantCmd)
			}
		})
	}
}

// The daemon merges the image's health check into the container's field by
// field, so it is dropped field by field: what the container set itself
// stays, and the image's test is not carried to the new image.
func TestDropImageDefaultsDropsTheHealthCheckFieldByField(t *testing.T) {
	image := container.HealthConfig{Test: []string{"CMD", "check"}, Interval: 30 * time.Second}
	for name, tc := range map[string]struct {
		set  container.HealthConfig
		want *container.HealthConfig
	}{
		"only an interval of its own": {set: container.HealthConfig{Test: image.Test, Interval: 5 * time.Second}, want: &container.HealthConfig{Interval: 5 * time.Second}},
		"a test of its own":           {set: container.HealthConfig{Test: []string{"CMD", "other"}, Interval: image.Interval, Retries: 2}, want: &container.HealthConfig{Test: []string{"CMD", "other"}, Retries: 2}},
		"NONE set for the container":  {set: container.HealthConfig{Test: []string{"NONE"}}, want: &container.HealthConfig{Test: []string{"NONE"}}},
		"the image's":                 {set: image},
	} {
		t.Run(name, func(t *testing.T) {
			set := tc.set
			options := client.ContainerCreateOptions{Config: &container.Config{Healthcheck: &set}, HostConfig: &container.HostConfig{}}
			if err := DropImageDefaults(t.Context(), oldImageDefaults(t), &options, "sha256:old"); err != nil {
				t.Fatalf("DropImageDefaults: %v", err)
			}
			if !reflect.DeepEqual(options.Config.Healthcheck, tc.want) {
				t.Fatalf("health check %+v, want %+v", options.Config.Healthcheck, tc.want)
			}
		})
	}
}

// What was set for the container differently from the old image stays, and
// so does an exposed port the container publishes.
func TestDropImageDefaultsKeepsTheContainersOwnValues(t *testing.T) {
	own := func() *container.Config {
		return &container.Config{
			Labels:       map[string]string{"org.opencontainers.image.version": "2.0", "traefik.enable": "true"},
			Env:          []string{"PATH=/custom/bin", "TOKEN=x"},
			Cmd:          []string{"serve", "--debug"},
			Entrypoint:   []string{"/other"},
			WorkingDir:   "/srv",
			User:         "root",
			StopSignal:   "SIGTERM",
			Healthcheck:  &container.HealthConfig{Test: []string{"CMD", "other"}, Interval: 10 * time.Second},
			ExposedPorts: network.PortSet{network.MustParsePort("80/tcp"): {}, network.MustParsePort("8080/tcp"): {}},
			Volumes:      map[string]struct{}{"/extra": {}},
		}
	}
	options := client.ContainerCreateOptions{
		Config:     own(),
		HostConfig: &container.HostConfig{PortBindings: network.PortMap{network.MustParsePort("80/tcp"): {{HostPort: "8080"}}}},
	}
	if err := DropImageDefaults(t.Context(), oldImageDefaults(t), &options, "sha256:old"); err != nil {
		t.Fatalf("DropImageDefaults: %v", err)
	}
	if !reflect.DeepEqual(options.Config, own()) {
		t.Fatalf("config = %+v, want %+v", options.Config, own())
	}
}

// An old image that is gone has nothing to drop; any other failure to read it
// stops the replacement before anything is changed.
func TestDropImageDefaultsWithoutTheOldImage(t *testing.T) {
	options := client.ContainerCreateOptions{Config: &container.Config{Env: []string{"PATH=/old/bin"}}, HostConfig: &container.HostConfig{}}
	if err := DropImageDefaults(t.Context(), imageConfigInspector{t: t, err: errdefs.ErrNotFound}, &options, "sha256:gone"); err != nil || len(options.Config.Env) != 1 {
		t.Fatalf("error %v, env %v; want the configuration kept as it is", err, options.Config.Env)
	}
	daemonDown := errors.New("cannot connect to the Docker daemon")
	if err := DropImageDefaults(t.Context(), imageConfigInspector{t: t, err: daemonDown}, &options, "sha256:old"); !errors.Is(err, daemonDown) {
		t.Fatalf("error = %v, want the daemon's", err)
	}
}

func TestUpdateStandaloneContainerGoesOnWhenTheOldImageIsGone(t *testing.T) {
	withTempUpdateStatusPath(t)
	before := standaloneTestInspect()
	before.Config.Labels = map[string]string{"traefik.enable": "true"}
	fake := newStandaloneUpdateFake()
	fake.imageInspectErrs = []error{nil, errdefs.ErrNotFound}
	result, err := updateStandaloneForTest(t.Context(), fake, before, "docker.io/library/nginx:latest", updateResultFor(before))
	if err != nil || !result.Updated || fake.createOptions[0].Config.Labels["traefik.enable"] != "true" {
		t.Fatalf("result %+v, error %v; want the update done with the configuration as it is", result, err)
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
	result := updateResultFor(before)

	result, err := updateStandaloneForTest(
		context.Background(), fake, before, "docker.io/library/nginx:latest", result,
	)
	if err != nil {
		t.Fatalf("updateStandaloneForTest: %v", err)
	}
	if !result.Updated || result.ContainerID != "replacement" || result.NewImageID != "sha256:new" {
		t.Fatalf("result = %+v", result)
	}
	wantCalls := []string{
		"list-containers",
		"pull:docker.io/library/nginx:latest",
		"inspect-image:docker.io/library/nginx:latest",
		"inspect-image:sha256:old",
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
	result := updateResultFor(before)

	_, err := updateStandaloneForTest(
		context.Background(), fake, before, "docker.io/library/nginx:latest", result,
	)
	if err == nil || !strings.Contains(err.Error(), "start failed") {
		t.Fatalf("updateStandaloneForTest error = %v", err)
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
			name: "old image inspection",
			configure: func(fake *fakeNativeUpdateClient, injected error) {
				fake.imageInspectErrs = []error{nil, injected}
			},
			wantCalls: []string{
				"list-containers",
				"pull:docker.io/library/nginx:latest",
				"inspect-image:docker.io/library/nginx:latest",
				"inspect-image:sha256:old",
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
				"inspect-image:sha256:old",
				"stop:old-container",
				"start:old-container",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			injected := errors.New("injected " + tc.name + " failure")
			fake := newStandaloneUpdateFake()
			tc.configure(fake, injected)

			_, err := updateStandaloneForTest(
				context.Background(), fake, standaloneTestInspect(), "docker.io/library/nginx:latest", updateResultFor(standaloneTestInspect()),
			)
			if !errors.Is(err, injected) {
				t.Fatalf("updateStandaloneForTest error = %v, want injected error", err)
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
		name      string
		configure func(*fakeNativeUpdateClient, error)
		// rolledBack: a replacement was under way and the original is back,
		// which the report, the summary and the alert say; a failure while
		// parking the original replaced nothing.
		rolledBack bool
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
			rolledBack: true,
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
			rolledBack: true,
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

			_, err := updateStandaloneForTest(
				context.Background(), fake, standaloneTestInspect(), "docker.io/library/nginx:latest", updateResultFor(standaloneTestInspect()),
			)
			if tc.name == "verification" {
				if err == nil || !strings.Contains(err.Error(), "verify replacement") {
					t.Fatalf("updateStandaloneForTest error = %v, want verification error", err)
				}
			} else if !errors.Is(err, injected) {
				t.Fatalf("updateStandaloneForTest error = %v, want injected error", err)
			}
			if len(fake.calls) < len(tc.wantSuffix) || !reflect.DeepEqual(fake.calls[len(fake.calls)-len(tc.wantSuffix):], tc.wantSuffix) {
				t.Fatalf("calls = %#v, want suffix %#v", fake.calls, tc.wantSuffix)
			}
			if errors.As(err, new(rolledBackError)) != tc.rolledBack || tc.rolledBack && !strings.Contains(err.Error(), "rolled back to the original container") {
				t.Fatalf("error = %v, want rolled back: %v", err, tc.rolledBack)
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

	_, err := updateStandaloneForTest(
		context.Background(), fake, standaloneTestInspect(), "docker.io/library/nginx:latest", updateResultFor(standaloneTestInspect()),
	)
	for _, want := range []error{primaryErr, removeErr, restoreErr} {
		if !errors.Is(err, want) {
			t.Fatalf("updateStandaloneForTest error = %v, want %v", err, want)
		}
	}
	if errors.As(err, new(rolledBackError)) {
		t.Fatalf("error = %v, want a failed rollback, not a rolled back update", err)
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

	_, err := updateStandaloneForTest(
		context.Background(), fake, standaloneTestInspect(), "docker.io/library/nginx:latest", updateResultFor(standaloneTestInspect()),
	)
	if !errors.Is(err, primaryErr) || !errors.Is(err, restoreErr) {
		t.Fatalf("updateStandaloneForTest error = %v, want primary and rollback errors", err)
	}
}

func TestUpdateStandaloneContainerReportsBackupCleanupFailure(t *testing.T) {
	withTempUpdateStatusPath(t)
	cleanupErr := errors.New("backup removal failed")
	fake := newStandaloneUpdateFake()
	fake.removeErrors["old-container"] = cleanupErr

	result, err := updateStandaloneForTest(
		context.Background(), fake, standaloneTestInspect(), "docker.io/library/nginx:latest", updateResultFor(standaloneTestInspect()),
	)
	if !errors.Is(err, cleanupErr) {
		t.Fatalf("updateStandaloneForTest error = %v, want cleanup error", err)
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

	result, err := updateStandaloneContainer(
		context.Background(),
		fake,
		before,
		"docker.io/library/nginx:latest",
		updateResultFor(before),
		nil,
		nil,
		stoppedContainerUpdatePolicy{Allow: true},
		nil,
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

	result, err := updateStandaloneContainer(
		context.Background(),
		fake,
		before,
		"docker.io/library/nginx:latest",
		updateResultFor(before),
		nil,
		nil,
		stoppedContainerUpdatePolicy{Allow: true, Revive: true},
		nil,
	)
	if err != nil || !result.Updated {
		t.Fatalf("revive stopped update result = %+v, error = %v", result, err)
	}
	calls := strings.Join(fake.calls, "|")
	if strings.Contains(calls, "stop:old-container") || !strings.Contains(calls, "start:replacement") {
		t.Fatalf("revive lifecycle calls = %v", fake.calls)
	}
}

// A replacement waits for the settle time only where it replaces a running
// container: a stopped one that a pass revives may be one-shot work that
// exits within seconds.
func TestStandaloneReplacementSettlesOnlyWhereTheOriginalRan(t *testing.T) {
	for name, tc := range map[string]struct {
		original *container.State
		policy   stoppedContainerUpdatePolicy
		want     time.Duration
	}{
		"running original":         {original: &container.State{Status: container.StateRunning, Running: true}, want: containerSettleTime},
		"stopped original revived": {original: &container.State{Status: container.StateExited}, policy: stoppedContainerUpdatePolicy{Allow: true, Revive: true}},
	} {
		t.Run(name, func(t *testing.T) {
			withTempUpdateStatusPath(t)
			synctest.Test(t, func(t *testing.T) {
				before := standaloneTestInspect()
				before.State = tc.original
				fake := newStandaloneUpdateFake()
				fake.inspectResults["replacement"] = justStarted("replacement", "sha256:new")
				began := time.Now()
				result, err := updateStandaloneContainer(t.Context(), fake, before, "docker.io/library/nginx:latest", updateResultFor(before), nil, nil, tc.policy, nil)
				if err != nil || !result.Updated || time.Since(began) != tc.want {
					t.Fatalf("updated after %v: %+v, %v; want updated after %v", time.Since(began), result, err, tc.want)
				}
			})
		})
	}
}

func TestStoppedStandaloneRollbackDoesNotStartOriginal(t *testing.T) {
	before := standaloneTestInspect()
	before.State = &container.State{Status: container.StateExited}
	fake := newStandaloneUpdateFake()
	fake.createErr = errors.New("replacement create failed")

	_, err := updateStandaloneContainer(
		context.Background(),
		fake,
		before,
		"docker.io/library/nginx:latest",
		updateResultFor(before),
		nil,
		nil,
		stoppedContainerUpdatePolicy{Allow: true},
		nil,
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
	journal := Journal{path: filepath.Join(t.TempDir(), "transaction.json")}
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

	result, err := updateStandaloneContainer(
		context.Background(),
		fake,
		before,
		"docker.io/library/nginx:latest",
		updateResultFor(before),
		&journal,
		nil,
		stoppedContainerUpdatePolicy{Allow: true},
		nil,
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

func TestStandaloneUpdateJournalClearsWhenTheOriginalIsGone(t *testing.T) {
	journal := Journal{path: filepath.Join(t.TempDir(), "transaction.json")}
	err := journal.write(standaloneUpdateTransaction{
		Phase: standaloneUpdatePrepared, OriginalID: "old-container", OriginalName: "web",
		BackupName: "web.old", ReplacementName: "web", OriginalRunning: true,
	})
	if err != nil {
		t.Fatalf("write journal: %v", err)
	}
	fake := newStandaloneUpdateFake()
	fake.inspectErrors = map[string]error{"old-container": errdefs.ErrNotFound}

	if err := recoverStandaloneUpdate(context.Background(), fake, journal); err != nil {
		t.Fatalf("recoverStandaloneUpdate = %v, want nil: a removed original leaves nothing to restore", err)
	}
	if _, err := os.Stat(journal.path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("journal still exists: %v", err)
	}
	if len(fake.calls) != 1 || fake.calls[0] != "inspect-container:old-container" {
		t.Fatalf("calls = %v, want only the original inspect", fake.calls)
	}
}

func TestStandaloneUpdateJournalRetriesVerifiedBackupCleanup(t *testing.T) {
	withTempUpdateStatusPath(t)
	journal := Journal{path: filepath.Join(t.TempDir(), "transaction.json")}
	cleanupErr := errors.New("backup removal failed")
	fake := newStandaloneUpdateFake()
	fake.removeErrors["old-container"] = cleanupErr

	result, err := updateStandaloneContainer(
		context.Background(), fake, standaloneTestInspect(), "docker.io/library/nginx:latest", updateResultFor(standaloneTestInspect()), &journal, nil, stoppedContainerUpdatePolicy{}, nil,
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

func TestStandaloneEditJournalRecoversPreparedRenamedReplacement(t *testing.T) {
	journal := Journal{path: filepath.Join(t.TempDir(), "transaction.json")}
	tx := standaloneUpdateTransaction{
		Phase:           standaloneUpdatePrepared,
		OriginalID:      "old-container",
		OriginalName:    "web",
		BackupName:      standaloneBackupName("old-container"),
		ReplacementName: "renamed-web",
		OriginalRunning: true,
	}
	if err := journal.write(tx); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	fake := newStandaloneUpdateFake()
	backup := standaloneTestInspect()
	backup.Name = "/" + tx.BackupName
	backup.State = &container.State{Status: container.StateExited}
	replacement := readyContainer("replacement", "sha256:new")
	replacement.Name = "/renamed-web"
	fake.inspectResults[tx.OriginalID] = backup
	fake.inspectResults[tx.ReplacementName] = replacement

	if err := recoverStandaloneUpdate(context.Background(), fake, journal); err != nil {
		t.Fatalf("recoverStandaloneUpdate: %v", err)
	}
	for _, call := range []string{
		"remove:replacement:true",
		"rename:old-container:web",
		"start:old-container",
	} {
		if !slices.Contains(fake.calls, call) {
			t.Errorf("calls %q do not contain %q", fake.calls, call)
		}
	}
}

func TestStandaloneUpdateJournalDoesNotSweepUnjournaledBackups(t *testing.T) {
	journal := Journal{path: filepath.Join(t.TempDir(), "transaction.json")}
	fake := newStandaloneUpdateFake()
	if err := recoverStandaloneUpdate(context.Background(), fake, journal); err != nil {
		t.Fatalf("recoverStandaloneUpdate: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("unexpected Docker calls without a journal: %v", fake.calls)
	}
}

func TestStandaloneUpdateJournalRefusesAnotherVersion(t *testing.T) {
	journal := Journal{path: filepath.Join(t.TempDir(), "transaction.json")}
	data := `{"version":2,"phase":"prepared","original_id":"old-container","original_name":"web","backup_name":"web.old","original_running":true}`
	if err := os.WriteFile(journal.path, []byte(data), 0o600); err != nil {
		t.Fatalf("write journal: %v", err)
	}
	fake := newStandaloneUpdateFake()

	err := recoverStandaloneUpdate(context.Background(), fake, journal)
	if err == nil || !strings.Contains(err.Error(), "unsupported standalone update transaction version 2") {
		t.Fatalf("recoverStandaloneUpdate error = %v, want unsupported version", err)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("unexpected Docker calls for an unsupported journal: %v", fake.calls)
	}
	if _, err := os.Stat(journal.path); err != nil {
		t.Fatalf("unsupported journal was not kept: %v", err)
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

// imageWithLabels is an inspected image whose configuration carries labels.
// It is decoded from JSON, as the Docker client does, since the configuration
// type belongs to a module this one does not require directly.
func imageWithLabels(t *testing.T, id string, labels map[string]string) image.InspectResponse {
	t.Helper()
	data, err := json.Marshal(map[string]any{"Id": id, "Config": map[string]any{"Labels": labels}})
	if err != nil {
		t.Fatal(err)
	}
	var inspect image.InspectResponse
	if err := json.Unmarshal(data, &inspect); err != nil {
		t.Fatal(err)
	}
	if inspect.Config == nil || len(inspect.Config.Labels) != len(labels) {
		t.Fatalf("image %s decoded without its labels: %+v", id, inspect)
	}
	return inspect
}

func updateResultFor(inspect container.InspectResponse) DockerContainerUpdateResult {
	return DockerContainerUpdateResult{
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

type readinessClientFunc func(context.Context) (container.InspectResponse, error)

func (f readinessClientFunc) ContainerInspect(
	ctx context.Context,
	_ string,
	_ client.ContainerInspectOptions,
) (client.ContainerInspectResult, error) {
	inspect, err := f(ctx)
	return client.ContainerInspectResult{Container: inspect}, err
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
	calls       []string
	pulledImage client.ImageInspectResult
	// images answers ImageInspect for a reference it holds; any other gets
	// pulledImage.
	images           map[string]image.InspectResponse
	pullErr          error
	pullWaitErr      error
	pullCloseErr     error
	imageInspectErr  error
	imageInspectErrs []error
	inspectErrors    map[string]error
	inspectResults   map[string]container.InspectResponse
	startErrors      map[string]error
	renameErrors     map[string]error
	removeErrors     map[string]error
	stopErr          error
	tagErr           error
	untagErr         error   // of every ImageRemove
	tagCtxErrs       []error // the state of each tag call's context
	// tagBlocks and imageInspectBlocks make those calls wait until their
	// context ends.
	tagBlocks          bool
	imageInspectBlocks bool
	createErr          error
	createOptions      []client.ContainerCreateOptions
	containerItems     []container.Summary
}

func (f *fakeNativeUpdateClient) ImageInspect(
	ctx context.Context,
	imageRef string,
	_ ...client.ImageInspectOption,
) (client.ImageInspectResult, error) {
	f.calls = append(f.calls, "inspect-image:"+imageRef)
	if f.imageInspectBlocks {
		<-ctx.Done()
		return client.ImageInspectResult{}, ctx.Err()
	}
	if inspect, ok := f.images[imageRef]; ok {
		return client.ImageInspectResult{InspectResponse: inspect}, nil
	}
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

func (f *fakeNativeUpdateClient) ImageTag(
	ctx context.Context,
	options client.ImageTagOptions,
) (client.ImageTagResult, error) {
	f.calls = append(f.calls, "tag:"+options.Source+":"+options.Target)
	f.tagCtxErrs = append(f.tagCtxErrs, ctx.Err())
	if f.tagBlocks {
		<-ctx.Done()
		return client.ImageTagResult{}, ctx.Err()
	}
	return client.ImageTagResult{}, f.tagErr
}

func (f *fakeNativeUpdateClient) ImageRemove(
	_ context.Context,
	imageRef string,
	_ client.ImageRemoveOptions,
) (client.ImageRemoveResult, error) {
	f.calls = append(f.calls, "untag:"+imageRef)
	return client.ImageRemoveResult{}, f.untagErr
}

func (f *fakeNativeUpdateClient) ContainerInspect(
	_ context.Context,
	containerID string,
	_ client.ContainerInspectOptions,
) (client.ContainerInspectResult, error) {
	f.calls = append(f.calls, "inspect-container:"+containerID)
	if err, ok := f.inspectErrors[containerID]; ok {
		return client.ContainerInspectResult{}, err
	}
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

// builtWebConfig is the effective configuration of a service "web" that
// builds the image nginx:latest, as `docker compose config` writes it.
const builtWebConfig = `{"services": {"web": {"build": {"context": "."}, "image": "nginx:latest"}}}`

// fakeComposeCLI puts a `docker` on PATH that records each command line in
// the file it returns and answers `config --format json` with config. A
// build writes buildLines numbered lines and exits with buildExit.
func fakeComposeCLI(t *testing.T, config string, buildLines, buildExit int) string {
	t.Helper()
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	script := fmt.Sprintf(`#!/bin/sh
echo "$*" >> %[1]s
case "$*" in
*"config --format json"*) echo '%[2]s' ;;
*" build --pull "*)
	echo "$BUILDX_NO_DEFAULT_ATTESTATIONS" > %[1]s.env
	echo ""
	i=1
	while [ $i -le %[3]d ]; do echo "#$i step $i"; i=$((i+1)); done
	exit %[4]d ;;
esac
`, args, config, buildLines, buildExit)
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	return args
}

// composeCommandsRun is each command line the fake Compose CLI recorded.
func composeCommandsRun(t *testing.T, args string) []string {
	t.Helper()
	data, err := os.ReadFile(args)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestRebuildRefusesAContainerComposeDoesNotManage(t *testing.T) {
	withTempUpdateStatusPath(t)
	_, err := rebuildInspectedContainer(t.Context(), nil, standaloneTestInspect(), nil)
	if !errors.Is(err, ErrNotRebuildable) || !strings.Contains(err.Error(), "Docker Compose does not manage it") {
		t.Fatalf("error = %v, want ErrNotRebuildable for a standalone container", err)
	}

	stopped := standaloneTestInspect()
	stopped.State = &container.State{Status: container.StateExited}
	if _, err := rebuildInspectedContainer(t.Context(), nil, stopped, nil); err == nil || errors.Is(err, ErrNotRebuildable) ||
		!strings.Contains(err.Error(), "must be running") {
		t.Fatalf("error = %v, want a stopped container refused for its state", err)
	}
}

func TestRebuildComposeContainerRefusesAServiceWithoutABuildSection(t *testing.T) {
	withTempUpdateStatusPath(t)
	args := fakeComposeCLI(t, `{"services": {"web": {"image": "nginx:latest"}}}`, 0, 0)
	before, fake, _ := composeUpdateFixture()

	_, err := rebuildComposeContainer(t.Context(), fake, composeRollback{}, before, composeTestTarget, "web", updateResultFor(before), nil)
	if !errors.Is(err, ErrNotRebuildable) || !strings.Contains(err.Error(), `service "web" of project "media" has no build section`) {
		t.Fatalf("error = %v, want ErrNotRebuildable for a service without a build section", err)
	}
	if commands := composeCommandsRun(t, args); len(commands) != 1 || !strings.HasSuffix(commands[0], "config --format json web") {
		t.Fatalf("commands = %q, want the configuration read alone", commands)
	}
}

func TestRebuildComposeContainerLeavesAContainerWhoseImageDidNotChange(t *testing.T) {
	withTempUpdateStatusPath(t)
	args := fakeComposeCLI(t, builtWebConfig, 2, 0)
	before, fake, _ := composeUpdateFixture()
	fake.images = map[string]image.InspectResponse{"docker.io/library/nginx:latest": {ID: before.Image}}
	var reports []string
	report := func(phase, message string) { reports = append(reports, phase+": "+message) }

	result, err := rebuildComposeContainer(t.Context(), fake, composeRollback{}, before, composeTestTarget, "web", updateResultFor(before), report)
	if err != nil {
		t.Fatalf("rebuildComposeContainer: %v", err)
	}
	if result.Updated || result.NewImageID != before.Image || result.ContainerID != before.ID {
		t.Fatalf("result = %+v, want the container left on its image", result)
	}
	commands := composeCommandsRun(t, args)
	// Reading the settings validates the project: no second `config`.
	want := []string{
		"compose --progress=json --project-name media --file compose.yml config --format json web",
		"compose --progress=plain --project-name media --file compose.yml build --pull web",
	}
	if !slices.Equal(commands, want) {
		t.Fatalf("commands = %q, want %q (no up)", commands, want)
	}
	// The build's output reaches the admin line by line, empty lines left out.
	wantReports := []string{
		"building: Building the image of Compose service web with fresh base images",
		"build_output: #1 step 1",
		"build_output: #2 step 2",
		"current: The rebuilt image is the one the container already runs",
	}
	if !slices.Equal(reports, wantReports) {
		t.Fatalf("reports = %q, want %q", reports, wantReports)
	}
	// The image the container runs is held by a tag of its own while the
	// build moves its name, and let go when the rebuild ends.
	wantCalls := []string{
		"tag:sha256:old:docker.io/library/nginx:linuxio-rebuild-previous",
		"inspect-image:docker.io/library/nginx:latest",
		"untag:docker.io/library/nginx:linuxio-rebuild-previous",
	}
	if !slices.Equal(fake.calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", fake.calls, wantCalls)
	}
	status, ok := ReadStatusSnapshot().ForContainerName("web")
	// The fake build resolves no base image.
	if !ok || status.CheckState != DockerUpdateCheckStateBuilt || status.CheckReason != builtReason("media", "web")+builtWithoutBases {
		t.Fatalf("status = %+v, %v; want built", status, ok)
	}
}

func TestRebuildComposeContainerBringsTheServiceUpOnTheNewImage(t *testing.T) {
	withTempUpdateStatusPath(t)
	args := fakeComposeCLI(t, builtWebConfig, 1, 0)
	before, fake, _ := composeUpdateFixture()
	// The tag names the rebuilt image (pulledImage, sha256:new), and the
	// service's new container runs it.
	after := readyContainer("rebuilt-web", "sha256:new")
	fake.inspectResults["web"] = after
	fake.inspectResults["rebuilt-web"] = after
	var phases []string
	report := func(phase, _ string) {
		if len(phases) == 0 || phases[len(phases)-1] != phase {
			phases = append(phases, phase)
		}
	}

	result, err := rebuildComposeContainer(t.Context(), fake, composeRollback{}, before, composeTestTarget, "web", updateResultFor(before), report)
	if err != nil {
		t.Fatalf("rebuildComposeContainer: %v", err)
	}
	if !result.Updated || result.NewImageID != "sha256:new" || result.ContainerID != "rebuilt-web" || result.PreviousImageID != before.Image {
		t.Fatalf("result = %+v, want the container replaced on the rebuilt image", result)
	}
	commands := composeCommandsRun(t, args)
	if len(commands) != 3 || commands[2] != "compose --progress=json --project-name media --file compose.yml up -d --no-deps --pull never --no-build --force-recreate web" {
		t.Fatalf("commands = %q, want the build followed by an up that pulls and builds nothing and recreates the container", commands)
	}
	if !slices.Equal(phases, []string{"building", ProgressPhaseBuildOutput, "starting", "verifying"}) {
		t.Fatalf("phases = %v", phases)
	}
	status, ok := ReadStatusSnapshot().ForContainerName("web")
	if !ok || status.CheckState != DockerUpdateCheckStateBuilt || status.ContainerID != "rebuilt-web" || status.ImageID != "sha256:new" {
		t.Fatalf("status = %+v, %v; want the new container built, not current", status, ok)
	}
}

func TestRebuildComposeContainerRollsBackAContainerThatFails(t *testing.T) {
	withTempUpdateStatusPath(t)
	args := fakeComposeCLI(t, builtWebConfig, 1, 0)
	// The new container exits on sha256:new, the image the tag names.
	before, fake, restored := composeUpdateFixture()
	commands := &composeRollbackFake{}
	rollback := commands.rollback(fake, map[string]composeServiceSettings{"web": {Image: "nginx:latest", Build: true}}, restored)

	result, err := rebuildComposeContainer(t.Context(), fake, rollback, before, composeTestTarget, "web", updateResultFor(before), nil)
	if !errors.As(err, new(rolledBackError)) || result.Updated {
		t.Fatalf("result = %+v, error = %v; want a rolled back rebuild", result, err)
	}
	wantTags := []string{
		"tag:sha256:old:docker.io/library/nginx:linuxio-rebuild-previous",
		"tag:sha256:old:docker.io/library/nginx:latest",
	}
	if tags := tagCalls(fake.calls); !slices.Equal(tags, wantTags) {
		t.Fatalf("tags = %v, want the old image held, then its tag put back on it", tags)
	}
	if last := fake.calls[len(fake.calls)-1]; last != "untag:docker.io/library/nginx:linuxio-rebuild-previous" {
		t.Fatalf("calls = %v, want the hold let go last", fake.calls)
	}
	if !reflect.DeepEqual(commands.restores, [][]string{{"web"}}) {
		t.Fatalf("restored services = %v, want web", commands.restores)
	}
	if last := composeCommandsRun(t, args); !strings.HasSuffix(last[len(last)-1], "up -d --no-deps --pull never --no-build --force-recreate web") {
		t.Fatalf("commands = %q, want the rebuild's up last", last)
	}
	if status, ok := ReadStatusSnapshot().ForContainerName("web"); ok {
		t.Fatalf("status = %+v, want none recorded for a failed rebuild", status)
	}
}

func TestRebuildComposeContainerReportsTheEndOfAFailedBuild(t *testing.T) {
	withTempUpdateStatusPath(t)
	args := fakeComposeCLI(t, builtWebConfig, 30, 1)
	before, fake, _ := composeUpdateFixture()

	_, err := rebuildComposeContainer(t.Context(), fake, composeRollback{}, before, composeTestTarget, "web", updateResultFor(before), nil)
	if err == nil || !strings.Contains(err.Error(), `build Compose project "media" service "web"`) ||
		!strings.Contains(err.Error(), "#11 step 11\n") || !strings.Contains(err.Error(), "#30 step 30") ||
		strings.Contains(err.Error(), "#10 step 10") {
		t.Fatalf("error = %v, want the build's last %d lines", err, buildOutputErrorLines)
	}
	wantCalls := []string{
		"tag:sha256:old:docker.io/library/nginx:linuxio-rebuild-previous",
		"untag:docker.io/library/nginx:linuxio-rebuild-previous",
	}
	if commands := composeCommandsRun(t, args); len(commands) != 2 || !slices.Equal(fake.calls, wantCalls) {
		t.Fatalf("commands = %q, calls = %v; want nothing after the failed build but letting go of the hold", commands, fake.calls)
	}
}

// The build runs without BuildKit's default attestations, also where the
// environment of `docker compose` is reduced to a fixed list.
func TestComposeBuildServiceBuildsWithoutDefaultAttestations(t *testing.T) {
	args := fakeComposeCLI(t, builtWebConfig, 0, 0)
	target := composeTestTarget
	target.IsolateEnvironment = true

	if err := composeBuildService(t.Context(), target, "web", nil); err != nil {
		t.Fatalf("composeBuildService: %v", err)
	}
	if commands := composeCommandsRun(t, args); len(commands) != 1 || commands[0] != "compose --progress=plain --project-name media --file compose.yml build --pull web" {
		t.Fatalf("commands = %q", commands)
	}
	env, err := os.ReadFile(args + ".env")
	if err != nil || strings.TrimSpace(string(env)) != "1" {
		t.Fatalf("BUILDX_NO_DEFAULT_ATTESTATIONS in the build = %q (%v), want 1", env, err)
	}
}

// When the files name another image than the one the container was created
// from, the build tags that one, and the rebuild reads the new image there.
func TestRebuildComposeContainerReadsTheImageTheFilesNameNow(t *testing.T) {
	withTempUpdateStatusPath(t)
	fakeComposeCLI(t, `{"services": {"web": {"build": {"context": "."}, "image": "nginx:1.29"}}}`, 0, 0)
	before, fake, _ := composeUpdateFixture()
	fake.images = map[string]image.InspectResponse{
		"docker.io/library/nginx:latest": {ID: before.Image}, // the old name still gives the old image
		"docker.io/library/nginx:1.29":   {ID: "sha256:new"},
	}
	after := readyContainer("rebuilt-web", "sha256:new")
	fake.inspectResults["web"] = after
	fake.inspectResults["rebuilt-web"] = after

	result, err := rebuildComposeContainer(t.Context(), fake, composeRollback{}, before, composeTestTarget, "web", updateResultFor(before), nil)
	if err != nil || !result.Updated || result.NewImageID != "sha256:new" {
		t.Fatalf("result = %+v, error = %v; want the service moved to the image the files name", result, err)
	}
	if !slices.Contains(fake.calls, "inspect-image:docker.io/library/nginx:1.29") ||
		!slices.Contains(fake.calls, "tag:sha256:old:docker.io/library/nginx:linuxio-rebuild-previous") {
		t.Fatalf("calls = %v, want the new name read and the old repository holding the old image", fake.calls)
	}
}

// A container whose image is already gone from the host is rebuilt without
// a hold; any other failure to hold it stops the rebuild.
func TestRebuildComposeContainerGoesOnWithoutAnImageToHold(t *testing.T) {
	for name, tc := range map[string]struct {
		tagErr  error
		wantErr string
	}{
		"image gone":    {tagErr: errors.New("Error response from daemon: No such image: sha256:old")},
		"typed gone":    {tagErr: fmt.Errorf("daemon: %w", errdefs.ErrNotFound)},
		"other failure": {tagErr: errors.New("Error response from daemon: invalid reference format"), wantErr: "invalid reference format"},
	} {
		t.Run(name, func(t *testing.T) {
			withTempUpdateStatusPath(t)
			args := fakeComposeCLI(t, builtWebConfig, 0, 0)
			before, fake, _ := composeUpdateFixture()
			fake.tagErr = tc.tagErr
			fake.images = map[string]image.InspectResponse{"docker.io/library/nginx:latest": {ID: before.Image}}

			result, err := rebuildComposeContainer(t.Context(), fake, composeRollback{}, before, composeTestTarget, "web", updateResultFor(before), nil)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) || len(composeCommandsRun(t, args)) != 1 {
					t.Fatalf("error = %v, commands = %q; want the rebuild stopped before the build", err, composeCommandsRun(t, args))
				}
				return
			}
			if err != nil || result.Updated || slices.Contains(fake.calls, "untag:docker.io/library/nginx:linuxio-rebuild-previous") {
				t.Fatalf("result = %+v, error = %v, calls = %v; want the rebuild done without a hold", result, err, fake.calls)
			}
		})
	}
}

// The hold is let go when the rebuild ends. Docker refuses to remove the
// tag while it is the image's last name and a container runs it: that keeps
// the image, and is no warning.
func TestRebuildComposeContainerLetsGoOfTheHold(t *testing.T) {
	for name, tc := range map[string]struct {
		untagErr error
		wantWarn bool
	}{
		"removed":              {},
		"kept for a container": {untagErr: errors.New("Error response from daemon: conflict: unable to remove repository reference \"nginx:linuxio-rebuild-previous\" (must force) - container 0123 is using its referenced image 4567")},
		"typed conflict":       {untagErr: fmt.Errorf("daemon: %w", errdefs.ErrConflict)},
		"other failure":        {untagErr: errors.New("Error response from daemon: unexpected EOF"), wantWarn: true},
	} {
		t.Run(name, func(t *testing.T) {
			withTempUpdateStatusPath(t)
			var logs strings.Builder
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
			t.Cleanup(func() { slog.SetDefault(previous) })
			fakeComposeCLI(t, builtWebConfig, 0, 0)
			before, fake, _ := composeUpdateFixture()
			fake.untagErr = tc.untagErr
			fake.images = map[string]image.InspectResponse{"docker.io/library/nginx:latest": {ID: before.Image}}

			if _, err := rebuildComposeContainer(t.Context(), fake, composeRollback{}, before, composeTestTarget, "web", updateResultFor(before), nil); err != nil {
				t.Fatalf("rebuildComposeContainer: %v", err)
			}
			if fake.calls[len(fake.calls)-1] != "untag:docker.io/library/nginx:linuxio-rebuild-previous" {
				t.Fatalf("calls = %v, want the hold let go last", fake.calls)
			}
			if warned := strings.Contains(logs.String(), "level=WARN"); warned != tc.wantWarn {
				t.Fatalf("logged %q; want a warning: %v", logs.String(), tc.wantWarn)
			}
		})
	}
}

// An update brings its services up on the image it pulled and builds none,
// also for a service with pull_policy: build.
func TestComposePullAndUpServicesBuildsNothing(t *testing.T) {
	args := fakeComposeCLI(t, builtWebConfig, 0, 0)

	if err := composePullAndUpServicesValidated(t.Context(), composeTestTarget, []string{"web", "worker"}, nil); err != nil {
		t.Fatalf("composePullAndUpServicesValidated: %v", err)
	}
	want := []string{
		"compose --progress=json --project-name media --file compose.yml pull web worker",
		"compose --progress=json --project-name media --file compose.yml up -d --no-deps --no-build --force-recreate web worker",
	}
	if commands := composeCommandsRun(t, args); !slices.Equal(commands, want) {
		t.Fatalf("commands = %q, want %q", commands, want)
	}
}

// A line longer than the limit is cut, and the lines after it are read.
func TestReadComposeLinesCutsAnOverlongLine(t *testing.T) {
	long := strings.Repeat("x", 200*1024)
	input := "first\n" + long + "\nafter\nlast without newline"
	var lines []string
	if err := readComposeLines(strings.NewReader(input), 100*1024, func(line string) { lines = append(lines, line) }); err != nil {
		t.Fatalf("readComposeLines: %v", err)
	}
	if len(lines) != 4 || lines[0] != "first" || lines[1] != long[:100*1024] || lines[2] != "after" || lines[3] != "last without newline" {
		t.Fatalf("got %d lines (%d bytes in the second), want first, the cut line, after, last", len(lines), len(lines[min(1, len(lines)-1)]))
	}
}
