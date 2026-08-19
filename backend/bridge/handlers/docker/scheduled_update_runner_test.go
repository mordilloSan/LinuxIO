package docker

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

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

func TestRunScheduledPassChecksAllRunningContainersWithoutSelection(t *testing.T) {
	checkErr := errors.New("check failed")
	var checked []container.Summary
	updateCalled := false
	err := runScheduledPass(context.Background(), apischema.DockerContainerAutoUpdateOptions{
		Mode: "check_only",
	}, scheduledPassOperations{
		list: func(context.Context) ([]container.Summary, error) {
			return []container.Summary{
				{ID: "web-id", Names: []string{"/web"}, State: container.StateRunning},
				{ID: "stopped-id", Names: []string{"/stopped"}, State: container.StateExited},
				{ID: "dead-id", Names: []string{"/dead"}, State: container.StateDead},
			}, nil
		},
		check: func(_ context.Context, summaries []container.Summary) error {
			checked = summaries
			return checkErr
		},
		update: func(context.Context, []container.Summary, []container.Summary, apischema.DockerContainerAutoUpdateOptions) error {
			updateCalled = true
			return nil
		},
	})
	if !errors.Is(err, checkErr) {
		t.Fatalf("runScheduledPass error = %v", err)
	}
	if len(checked) != 1 || checked[0].ID != "web-id" || updateCalled {
		t.Fatalf("dispatch = checked %#v, update called %v", checked, updateCalled)
	}
}

func TestRunScheduledPassCanCheckStoppedContainers(t *testing.T) {
	var checked []container.Summary
	err := runScheduledPass(context.Background(), apischema.DockerContainerAutoUpdateOptions{
		IncludeStopped: true,
		Mode:           "check_only",
	}, scheduledPassOperations{
		list: func(context.Context) ([]container.Summary, error) {
			return []container.Summary{
				{ID: "web-id", Names: []string{"/web"}, State: container.StateRunning},
				{ID: "stopped-id", Names: []string{"/stopped"}, State: container.StateExited},
				{ID: "dead-id", Names: []string{"/dead"}, State: container.StateDead},
			}, nil
		},
		check: func(_ context.Context, summaries []container.Summary) error {
			checked = summaries
			return nil
		},
		update: func(context.Context, []container.Summary, []container.Summary, apischema.DockerContainerAutoUpdateOptions) error {
			return errors.New("unexpected update")
		},
	})
	if err != nil {
		t.Fatalf("runScheduledPass: %v", err)
	}
	if len(checked) != 2 || checked[1].ID != "stopped-id" {
		t.Fatalf("checked summaries = %#v", checked)
	}
}

func TestRunScheduledPassDispatchesUpdateCleanupOption(t *testing.T) {
	called := false
	err := runScheduledPass(context.Background(), apischema.DockerContainerAutoUpdateOptions{
		Mode:           "update",
		Cleanup:        true,
		ContainerNames: []string{"web", "missing"},
	}, scheduledPassOperations{
		list: func(context.Context) ([]container.Summary, error) {
			return []container.Summary{
				{ID: "web-id", Names: []string{"/web"}, State: container.StateRunning},
				{ID: "stopped-id", Names: []string{"/stopped"}, State: container.StateExited},
			}, nil
		},
		check: func(_ context.Context, summaries []container.Summary) error {
			if len(summaries) != 1 || summaries[0].ID != "web-id" {
				t.Fatalf("checked summaries = %#v", summaries)
			}
			return nil
		},
		update: func(_ context.Context, summaries, all []container.Summary, opts apischema.DockerContainerAutoUpdateOptions) error {
			called = len(summaries) == 1 && summaries[0].ID == "web-id" &&
				len(all) == 2 && all[0].ID == "web-id" && opts.Cleanup
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "missing") || !called {
		t.Fatalf("runScheduledPass = %v, called %v", err, called)
	}
}

func TestScheduledComposeGroupsContinueAfterIndependentFailures(t *testing.T) {
	withTempUpdateStatusPath(t)
	state := newScheduledUpdateState(context.Background(), nil)
	calls := 0
	composeErr := errors.New("compose failed")
	state.composeUpdate = func(context.Context, composeProjectTarget, []string, composeLineEmitter) error {
		calls++
		return composeErr
	}
	state.addComposeCandidate(composeProjectTarget{Name: "one", ConfigFiles: []string{"one.yml"}}, "web", readyContainer("one", "sha256:old"))
	state.addComposeCandidate(composeProjectTarget{Name: "two", ConfigFiles: []string{"two.yml"}}, "api", readyContainer("two", "sha256:old"))

	if err := state.applyComposeGroups(); err != nil {
		t.Fatalf("applyComposeGroups: %v", err)
	}
	if calls != 2 || len(state.errs) != 2 {
		t.Fatalf("compose calls = %d, errors = %v", calls, state.errs)
	}
}

func TestScheduledComposeCandidatesBatchByProject(t *testing.T) {
	state := newScheduledUpdateState(context.Background(), nil)
	target := composeProjectTarget{
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
	state := newScheduledUpdateState(context.Background(), nil)
	composeErr := errors.New("compose failed")
	state.composeUpdate = func(context.Context, composeProjectTarget, []string, composeLineEmitter) error {
		return composeErr
	}
	target := composeProjectTarget{Name: "media", ConfigFiles: []string{"compose.yml"}}
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
	snapshot := readUpdateStatusSnapshot()
	for _, before := range group.before {
		status, ok := snapshot.forContainer(before.ID)
		if !ok || status.CheckState != apischema.DockerUpdateCheckStateError ||
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
