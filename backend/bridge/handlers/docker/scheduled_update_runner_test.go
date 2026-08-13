package docker

import (
	"context"
	"errors"
	"path/filepath"
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

func TestRunScheduledPassAggregatesMissingTargetsAndDispatchesAvailableOnes(t *testing.T) {
	checkErr := errors.New("check failed")
	var checked []container.Summary
	updateCalled := false
	err := runScheduledPass(context.Background(), apischema.DockerContainerAutoUpdateOptions{
		Mode:           "check_only",
		ContainerNames: []string{"web", "missing"},
	}, scheduledPassOperations{
		list: func(context.Context) ([]container.Summary, error) {
			return []container.Summary{{ID: "web-id", Names: []string{"/web"}}}, nil
		},
		check: func(_ context.Context, summaries []container.Summary) error {
			checked = summaries
			return checkErr
		},
		update: func(context.Context, []container.Summary, bool) error {
			updateCalled = true
			return nil
		},
	})
	if !errors.Is(err, checkErr) || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("runScheduledPass error = %v", err)
	}
	if len(checked) != 1 || checked[0].ID != "web-id" || updateCalled {
		t.Fatalf("dispatch = checked %#v, update called %v", checked, updateCalled)
	}
}

func TestRunScheduledPassDispatchesUpdateCleanupOption(t *testing.T) {
	called := false
	err := runScheduledPass(context.Background(), apischema.DockerContainerAutoUpdateOptions{
		Mode:           "update",
		Cleanup:        true,
		ContainerNames: []string{"web"},
	}, scheduledPassOperations{
		list: func(context.Context) ([]container.Summary, error) {
			return []container.Summary{{ID: "web-id", Names: []string{"/web"}}}, nil
		},
		check: func(context.Context, []container.Summary) error {
			t.Fatal("check-only operation called in update mode")
			return nil
		},
		update: func(_ context.Context, summaries []container.Summary, cleanup bool) error {
			called = len(summaries) == 1 && summaries[0].ID == "web-id" && cleanup
			return nil
		},
	})
	if err != nil || !called {
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
