package dockerupdate

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/client"
	digest "github.com/opencontainers/go-digest"
	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/scheduledrun"
)

func TestRegistryLookupCacheReusesOnlySuccessfulLookups(t *testing.T) {
	const imageRef = "lscr.io/linuxserver/speedtest-tracker:latest"
	localDigest := digest.FromString("local")
	remoteDigest := digest.FromString("remote")
	calls := 0
	fake := &fakeImageUpdateCheckClient{
		images: map[string]client.ImageInspectResult{
			"image-id": {InspectResponse: image.InspectResponse{
				RepoDigests: []string{"lscr.io/linuxserver/speedtest-tracker@" + localDigest.String()},
			}},
		},
		distributionHook: func(context.Context, string, client.DistributionInspectOptions) (client.DistributionInspectResult, error) {
			calls++
			if calls == 1 {
				return client.DistributionInspectResult{}, errdefs.ErrNotFound
			}
			return distributionInspectResult(remoteDigest), nil
		},
	}
	registry := newRegistryLookupCache(fake)

	statuses, _, err := checkContainerImageUpdates(t.Context(), registry, nil, []containerImageUpdateTarget{{
		ContainerID: "container-id", ContainerName: "speedtest-tracker", ImageID: "image-id", ImageRef: imageRef,
	}}, time.Now())
	if err != nil || len(statuses) != 1 || statuses[0].Err == "" {
		t.Fatalf("check pass = %+v, %v; want one failed lookup", statuses, err)
	}
	// The update pass asks again after a failed check, then reuses the success.
	for range 2 {
		observation, err := inspectImageUpdate(t.Context(), registry, "image-id", imageRef)
		if err != nil || observation.err != nil || observation.remoteDigest != remoteDigest.String() {
			t.Fatalf("update pass observation = %+v, %v", observation, err)
		}
	}
	if calls != 2 {
		t.Fatalf("registry lookups = %d, want 2", calls)
	}
}

func TestAcquireDockerUpdateLockAtExcludesConcurrentUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker-update.lock")
	release, err := acquireDockerUpdateLockAt(context.Background(), path, time.Second, time.Millisecond)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}

	_, contentionErr := acquireDockerUpdateLockAt(context.Background(), path, 20*time.Millisecond, time.Millisecond)
	if contentionErr == nil || !strings.Contains(contentionErr.Error(), "already in progress") {
		t.Fatalf("contended lock error = %v", contentionErr)
	}
	release()

	release, err = acquireDockerUpdateLockAt(context.Background(), path, time.Second, time.Millisecond)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	release()
}

// A job's update (lock wait 0) queues behind the holder until its context
// ends, while a bounded wait still gives up.
func TestAcquireDockerUpdateLockAtZeroWaitBlocksUntilContextEnds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "docker-update.lock")
	release, err := acquireDockerUpdateLockAt(context.Background(), path, time.Second, time.Millisecond)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}
	defer release()

	if _, err := acquireDockerUpdateLockAt(context.Background(), path, 20*time.Millisecond, time.Millisecond); err == nil || !strings.Contains(err.Error(), "already in progress") {
		t.Fatalf("bounded wait error = %v, want already in progress", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := acquireDockerUpdateLockAt(ctx, path, 0, time.Millisecond)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("zero wait returned %v while the lock is held, want it to block", err)
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("zero wait error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("zero wait did not return after the context was canceled")
	}
}

func TestRunScheduledPassChecksAllRunningContainersWithoutSelection(t *testing.T) {
	checkErr := errors.New("check failed")
	var checked []container.Summary
	updateCalled := false
	_, err := runScheduledPass(context.Background(), DockerContainerAutoUpdateOptions{Mode: "check_only"}, alerts.NotificationSettings{}, scheduledPassOperations{
		list: func(context.Context) ([]container.Summary, error) {
			return []container.Summary{
				{ID: "web-id", Names: []string{"/web"}, State: container.StateRunning},
				{ID: "stopped-id", Names: []string{"/stopped"}, State: container.StateExited},
				{ID: "dead-id", Names: []string{"/dead"}, State: container.StateDead},
			}, nil
		},
		check: func(_ context.Context, summaries []container.Summary) (DockerUpdateCheckResult, error) {
			checked = summaries
			return DockerUpdateCheckResult{}, checkErr
		},
		update: func(context.Context, []container.Summary, []container.Summary, DockerContainerAutoUpdateOptions, func(string)) ([]ContainerOutcome, error) {
			updateCalled = true
			return nil, nil
		},
	}, nil)
	if !errors.Is(err, checkErr) {
		t.Fatalf("runScheduledPass error = %v", err)
	}
	if len(checked) != 1 || checked[0].ID != "web-id" || updateCalled {
		t.Fatalf("dispatch = checked %#v, update called %v", checked, updateCalled)
	}
}

func TestRunScheduledPassCanCheckStoppedContainers(t *testing.T) {
	var checked []container.Summary
	_, err := runScheduledPass(context.Background(), DockerContainerAutoUpdateOptions{IncludeStopped: true, Mode: "check_only"}, alerts.NotificationSettings{}, scheduledPassOperations{
		list: func(context.Context) ([]container.Summary, error) {
			return []container.Summary{
				{ID: "web-id", Names: []string{"/web"}, State: container.StateRunning},
				{ID: "stopped-id", Names: []string{"/stopped"}, State: container.StateExited},
				{ID: "dead-id", Names: []string{"/dead"}, State: container.StateDead},
			}, nil
		},
		check: func(_ context.Context, summaries []container.Summary) (DockerUpdateCheckResult, error) {
			checked = summaries
			return DockerUpdateCheckResult{}, nil
		},
		update: func(context.Context, []container.Summary, []container.Summary, DockerContainerAutoUpdateOptions, func(string)) ([]ContainerOutcome, error) {
			return nil, errors.New("unexpected update")
		},
	}, nil)
	if err != nil {
		t.Fatalf("runScheduledPass: %v", err)
	}
	if len(checked) != 2 || checked[1].ID != "stopped-id" {
		t.Fatalf("checked summaries = %#v", checked)
	}
}

func TestRunScheduledPassDispatchesUpdateCleanupOption(t *testing.T) {
	called := false
	_, err := runScheduledPass(context.Background(), DockerContainerAutoUpdateOptions{
		Mode:           "update",
		Cleanup:        true,
		ContainerNames: []string{"web", "missing"}}, alerts.NotificationSettings{}, scheduledPassOperations{
		list: func(context.Context) ([]container.Summary, error) {
			return []container.Summary{
				{ID: "web-id", Names: []string{"/web"}, State: container.StateRunning},
				{ID: "stopped-id", Names: []string{"/stopped"}, State: container.StateExited},
			}, nil
		},
		check: func(_ context.Context, summaries []container.Summary) (DockerUpdateCheckResult, error) {
			if len(summaries) != 1 || summaries[0].ID != "web-id" {
				t.Fatalf("checked summaries = %#v", summaries)
			}
			return DockerUpdateCheckResult{}, nil
		},
		update: func(_ context.Context, summaries, all []container.Summary, opts DockerContainerAutoUpdateOptions, _ func(string)) ([]ContainerOutcome, error) {
			called = len(summaries) == 1 && summaries[0].ID == "web-id" &&
				len(all) == 2 && all[0].ID == "web-id" && opts.Cleanup
			return nil, nil
		},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "missing") || !called {
		t.Fatalf("runScheduledPass = %v, called %v", err, called)
	}
}

// The free-space minimum is asked only when a pass in update mode has
// containers to update; too little room fails the pass before any update,
// with no container alert, so the run's own alert reports it.
func TestRunScheduledPassRefusesUpdatesWithTooLittleFreeSpace(t *testing.T) {
	lowSpace := errors.New("no container was updated: the filesystem of /var/lib/docker has 1.0 GiB free")
	for name, tc := range map[string]struct {
		opts       DockerContainerAutoUpdateOptions
		names      []string
		spaceErr   error
		wantAsked  int
		wantUpdate bool
		wantErr    error
	}{
		"no minimum":        {opts: DockerContainerAutoUpdateOptions{Mode: "update", UpdateAll: true}, wantUpdate: true},
		"room enough":       {opts: DockerContainerAutoUpdateOptions{Mode: "update", UpdateAll: true, MinFreeGiB: 5}, wantAsked: 5, wantUpdate: true},
		"too little room":   {opts: DockerContainerAutoUpdateOptions{Mode: "update", UpdateAll: true, MinFreeGiB: 5}, spaceErr: lowSpace, wantAsked: 5, wantErr: lowSpace},
		"check only":        {opts: DockerContainerAutoUpdateOptions{Mode: "check_only", UpdateAll: true, MinFreeGiB: 5}, spaceErr: lowSpace},
		"nothing to update": {opts: DockerContainerAutoUpdateOptions{Mode: "update", MinFreeGiB: 5}, spaceErr: lowSpace},
	} {
		t.Run(name, func(t *testing.T) {
			asked, updated := 0, false
			pass, err := runScheduledPass(t.Context(), tc.opts, alerts.NotificationSettings{}, scheduledPassOperations{
				list: func(context.Context) ([]container.Summary, error) {
					return []container.Summary{{ID: "web-id", Names: []string{"/web"}, State: container.StateRunning}}, nil
				},
				check: func(context.Context, []container.Summary) (DockerUpdateCheckResult, error) {
					return DockerUpdateCheckResult{Checked: 1, Updates: 1}, nil
				},
				update: func(context.Context, []container.Summary, []container.Summary, DockerContainerAutoUpdateOptions, func(string)) ([]ContainerOutcome, error) {
					updated = true
					return nil, nil
				},
				freeSpace: func(_ context.Context, minGiB int) error {
					asked = minGiB
					return tc.spaceErr
				},
			}, nil)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("runScheduledPass error = %v, want %v", err, tc.wantErr)
			}
			if asked != tc.wantAsked || updated != tc.wantUpdate {
				t.Fatalf("asked for %d GiB, updated %v; want %d, %v", asked, updated, tc.wantAsked, tc.wantUpdate)
			}
			if tc.wantErr != nil && scheduledrun.Unreported(err) == nil {
				t.Fatal("a refusal for free space is marked reported; the run would not fail")
			}
			if pass.Checked.Updates != 1 {
				t.Fatalf("report = %+v, want the check's result kept", pass)
			}
		})
	}
}

type fakeDockerInfo struct {
	root string
	err  error
}

func (f fakeDockerInfo) Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error) {
	var result client.SystemInfoResult
	result.Info.DockerRootDir = f.root
	return result, f.err
}

// Both Docker's root folder and containerd's are measured; a containerd
// folder that does not exist is passed over.
func TestCheckDockerFreeSpaceNamesTheFolderShortOfRoom(t *testing.T) {
	for name, tc := range map[string]struct {
		free    map[string]uint64
		wantErr string
	}{
		"room in both":         {free: map[string]uint64{"/srv/docker": 20 << 30, containerdRoot: 20 << 30}},
		"docker short":         {free: map[string]uint64{"/srv/docker": 3 << 30, containerdRoot: 20 << 30}, wantErr: "/srv/docker has 3.0 GiB free, less than the 5 GiB"},
		"containerd short":     {free: map[string]uint64{"/srv/docker": 20 << 30, containerdRoot: 1 << 29}, wantErr: containerdRoot + " has 0.5 GiB free"},
		"no containerd folder": {free: map[string]uint64{"/srv/docker": 20 << 30}},
		"exactly the minimum":  {free: map[string]uint64{"/srv/docker": 5 << 30, containerdRoot: 5 << 30}},
	} {
		t.Run(name, func(t *testing.T) {
			old := freeBytesAt
			t.Cleanup(func() { freeBytesAt = old })
			freeBytesAt = func(path string) (uint64, error) {
				free, ok := tc.free[path]
				if !ok {
					return 0, unix.ENOENT
				}
				return free, nil
			}
			err := checkDockerFreeSpace(t.Context(), fakeDockerInfo{root: "/srv/docker"}, 5)
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("checkDockerFreeSpace error = %v, want %q", err, tc.wantErr)
			}
		})
	}
	// Docker's own folder must be readable: its absence is an error.
	old := freeBytesAt
	t.Cleanup(func() { freeBytesAt = old })
	freeBytesAt = func(string) (uint64, error) { return 0, unix.ENOENT }
	if err := checkDockerFreeSpace(t.Context(), fakeDockerInfo{root: "/srv/docker"}, 5); err == nil {
		t.Fatal("a Docker root folder that cannot be read passed the check")
	}
	if err := checkDockerFreeSpace(t.Context(), fakeDockerInfo{err: errors.New("daemon down")}, 5); err == nil {
		t.Fatal("a daemon that does not answer passed the check")
	}
}

func TestRunScheduledPassUpdateAllSkipsIneligibleContainers(t *testing.T) {
	compose := map[string]string{"com.docker.compose.project": "p", "com.docker.compose.service": "s"}
	var updated []string
	_, err := runScheduledPass(context.Background(), DockerContainerAutoUpdateOptions{Mode: "update", UpdateAll: true}, alerts.NotificationSettings{}, scheduledPassOperations{
		list: func(context.Context) ([]container.Summary, error) {
			return []container.Summary{
				{ID: "web-id", Names: []string{"/web"}, State: container.StateRunning},
				{ID: "stopped-id", Names: []string{"/stopped"}, State: container.StateExited},
				{ID: "rep1", Names: []string{"/p-s-1"}, State: container.StateRunning, Labels: compose},
				{ID: "rep2", Names: []string{"/p-s-2"}, State: container.StateRunning, Labels: compose},
			}, nil
		},
		check: func(context.Context, []container.Summary) (DockerUpdateCheckResult, error) {
			return DockerUpdateCheckResult{}, nil
		},
		update: func(_ context.Context, summaries, _ []container.Summary, _ DockerContainerAutoUpdateOptions, _ func(string)) ([]ContainerOutcome, error) {
			for _, summary := range summaries {
				updated = append(updated, summary.ID)
			}
			return nil, nil
		},
	}, nil)
	if err != nil || !slices.Equal(updated, []string{"web-id"}) {
		t.Fatalf("runScheduledPass = %v, updated %v", err, updated)
	}
}

func TestScheduledComposeGroupsContinueAfterIndependentFailures(t *testing.T) {
	withTempUpdateStatusPath(t)
	useTempAlertStore(t)
	state := newScheduledUpdateState(context.Background(), nil)
	calls := 0
	composeErr := errors.New("compose failed")
	state.composeUpdate = func(context.Context, ComposeTarget, []string, ComposeLineEmitter) error {
		calls++
		return composeErr
	}
	state.addComposeCandidate(ComposeTarget{Name: "one", ConfigFiles: []string{"one.yml"}}, "web", readyContainer("one", "sha256:old"))
	state.addComposeCandidate(ComposeTarget{Name: "two", ConfigFiles: []string{"two.yml"}}, "api", readyContainer("two", "sha256:old"))

	if err := state.applyComposeGroups(); err != nil {
		t.Fatalf("applyComposeGroups: %v", err)
	}
	if calls != 2 || len(state.errs) != 2 {
		t.Fatalf("compose calls = %d, errors = %v", calls, state.errs)
	}
}

func TestScheduledComposeCandidatesBatchByProject(t *testing.T) {
	state := newScheduledUpdateState(context.Background(), nil)
	target := ComposeTarget{
		Name:        "media",
		ConfigFiles: []string{"compose.yml", "compose.prod.yml"},
		WorkingDir:  "/srv/media",
	}
	web := readyContainer("web", "sha256:web")
	worker := readyContainer("worker", "sha256:worker")
	worker.Name = "/worker"

	state.addComposeCandidate(target, "web", web)
	state.addComposeCandidate(target, "worker", worker)

	if len(state.composeGroups) != 1 {
		t.Fatalf("Compose groups = %d, want one project batch", len(state.composeGroups))
	}
	group := state.composeGroups[composeScheduleKey(target)]
	if group == nil {
		t.Fatal("Compose project batch is missing")
	}
	if !slices.Equal(group.services, []string{"web", "worker"}) {
		t.Fatalf("batched services = %v", group.services)
	}
	if len(group.before) != 2 || group.before[0].ID != web.ID || group.before[1].ID != worker.ID {
		t.Fatalf("batched containers = %+v", group.before)
	}
}

func TestScheduledComposeGroupErrorRecordsEveryCandidate(t *testing.T) {
	withTempUpdateStatusPath(t)
	useTempAlertStore(t)
	state := newScheduledUpdateState(context.Background(), nil)
	composeErr := errors.New("compose failed")
	state.composeUpdate = func(context.Context, ComposeTarget, []string, ComposeLineEmitter) error {
		return composeErr
	}
	target := ComposeTarget{Name: "media", ConfigFiles: []string{"compose.yml"}}
	web := readyContainer("web", "sha256:web")
	worker := readyContainer("worker", "sha256:worker")
	worker.Name = "/worker"
	group := &scheduledComposeTarget{
		target:   target,
		services: []string{"web", "worker"},
		before:   []container.InspectResponse{web, worker},
	}

	if err := state.applyComposeGroup(group); err != nil {
		t.Fatalf("applyComposeGroup: %v", err)
	}
	if len(state.errs) != 1 || !errors.Is(state.errs[0], composeErr) {
		t.Fatalf("scheduled errors = %v", state.errs)
	}
	snapshot := ReadStatusSnapshot()
	for _, before := range group.before {
		status, ok := snapshot.ForContainer(before.ID)
		if !ok || status.CheckState != DockerUpdateCheckStateError ||
			!strings.Contains(status.Err, "compose failed") {
			t.Fatalf("status for %s = %+v, %v", before.ID, status, ok)
		}
	}
}

type fakeScheduledCleanupClient struct {
	containers []container.Summary
	removed    []string
	removeErr  map[string]error
}

func (f *fakeScheduledCleanupClient) ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error) {
	return client.ContainerListResult{Items: f.containers}, nil
}

func (f *fakeScheduledCleanupClient) ImageRemove(_ context.Context, imageID string, _ client.ImageRemoveOptions) (client.ImageRemoveResult, error) {
	f.removed = append(f.removed, imageID)
	return client.ImageRemoveResult{}, f.removeErr[imageID]
}

func TestCleanupUnusedUpdateImagesDeduplicatesAndSkipsInUseImages(t *testing.T) {
	fake := &fakeScheduledCleanupClient{
		containers: []container.Summary{{ImageID: "sha256:in-use"}},
		removeErr:  map[string]error{},
	}
	if err := cleanupUnusedUpdateImages(context.Background(), fake, []string{"", "sha256:old", "sha256:old", "sha256:in-use"}); err != nil {
		t.Fatalf("cleanupUnusedUpdateImages: %v", err)
	}
	if len(fake.removed) != 1 || fake.removed[0] != "sha256:old" {
		t.Fatalf("removed images = %v", fake.removed)
	}
}

func TestScheduledPassReportsItsSteps(t *testing.T) {
	var messages []string
	_, err := runScheduledPass(context.Background(), DockerContainerAutoUpdateOptions{Mode: "update", ContainerNames: []string{"web", "db"}}, alerts.NotificationSettings{}, scheduledPassOperations{
		list: func(context.Context) ([]container.Summary, error) {
			return []container.Summary{
				{ID: "web-id", Names: []string{"/web"}, State: container.StateRunning},
				{ID: "db-id", Names: []string{"/db"}, State: container.StateRunning},
			}, nil
		},
		check: func(context.Context, []container.Summary) (DockerUpdateCheckResult, error) {
			return DockerUpdateCheckResult{}, nil
		},
		update: func(_ context.Context, summaries, _ []container.Summary, _ DockerContainerAutoUpdateOptions, announce func(string)) ([]ContainerOutcome, error) {
			for _, summary := range summaries {
				announce(PrimaryContainerName(summary))
			}
			return nil, nil
		},
	}, func(message string) { messages = append(messages, message) })
	if err != nil {
		t.Fatalf("runScheduledPass: %v", err)
	}
	want := []string{"Checking for updates", "Updating web", "Updating db"}
	if !slices.Equal(messages, want) {
		t.Fatalf("messages = %q, want %q", messages, want)
	}
}
