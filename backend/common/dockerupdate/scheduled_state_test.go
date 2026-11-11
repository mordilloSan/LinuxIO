package dockerupdate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
)

func TestScheduledStandaloneCleanupFailurePreservesCurrentStatus(t *testing.T) {
	withTempUpdateStatusPath(t)
	ctx := context.Background()
	before := standaloneTestInspect()
	after := readyContainer("replacement", "sha256:new")
	markContainerCurrent(ctx, before.ID, after)

	updated := updateResultFor(before)
	updated.ContainerID = after.ID
	updated.NewImageID = after.Image
	updated.Updated = true
	cleanupErr := errors.New("backup removal failed")
	state := newScheduledUpdateState(ctx, nil)

	if err := state.recordStandaloneUpdateOutcome(before, updated, cleanupErr); err != nil {
		t.Fatalf("recordStandaloneUpdateOutcome: %v", err)
	}
	if len(state.errs) != 1 || !errors.Is(state.errs[0], cleanupErr) {
		t.Fatalf("scheduled errors = %v, want cleanup error", state.errs)
	}
	if len(state.oldImageIDs) != 1 || state.oldImageIDs[0] != before.Image {
		t.Fatalf("old image IDs = %v, want [%s]", state.oldImageIDs, before.Image)
	}
	// The replacement counts even though its cleanup failed: every_run's
	// "nothing to update" depends on this count.
	if state.updated != 1 {
		t.Fatalf("replaced containers = %d, want 1", state.updated)
	}

	snapshot := ReadStatusSnapshot()
	status, ok := snapshot.ForContainerName("web")
	if !ok {
		t.Fatal("replacement status was not retained")
	}
	if status.ContainerID != after.ID || status.ImageID != after.Image || status.UpdateAvailable || status.Err != "" {
		t.Fatalf("replacement status = %+v, want current container without an update error", status)
	}
	if _, ok := snapshot.ForContainer(before.ID); ok {
		t.Fatal("old container status was retained")
	}
}

func TestScheduledComposeVerifyCountsReplacedContainers(t *testing.T) {
	withTempUpdateStatusPath(t)
	useTempAlertStore(t)
	state := newScheduledUpdateState(t.Context(), nil)
	state.alertReport = alerts.ReportChanges
	web := readyContainer("web-old", "sha256:web-old")
	worker := readyContainer("worker", "sha256:worker")
	worker.Name = "/worker"
	newWeb := readyContainer("web-new", "sha256:web-new")
	// Each container is inspected by name, then by ID until it is ready.
	state.readiness = &sequenceReadinessClient{results: []container.InspectResponse{newWeb, newWeb, worker, worker}}
	group := &scheduledComposeTarget{
		target:   ComposeTarget{Name: "media", ConfigFiles: []string{"compose.yml"}},
		services: []string{"web", "worker"},
		before:   []container.InspectResponse{web, worker},
	}

	if err := state.verifyComposeGroup(group); err != nil {
		t.Fatalf("verifyComposeGroup: %v", err)
	}
	// Only web got a new image; worker kept its own.
	if state.updated != 1 || len(state.oldImageIDs) != 1 || state.oldImageIDs[0] != web.Image || len(state.errs) != 0 {
		t.Fatalf("replaced = %d, old images %v, errors %v; want 1, [%s], none", state.updated, state.oldImageIDs, state.errs, web.Image)
	}
	if ids := alertIDs(t); len(ids) != 1 || ids[0] != "docker-update/updated-web" {
		t.Fatalf("alerts = %v, want the one updated notice", ids)
	}
}

func TestScheduledLocalImageIsUncheckableWithoutRunError(t *testing.T) {
	withTempUpdateStatusPath(t)
	ctx := context.Background()
	candidate := scheduledUpdateCandidate{inspect: standaloneTestInspect()}
	const reason = "local image has no repository digest"

	result, err := applyContainerImageObservation(ctx, candidate, "docker.io/library/local:test", imageUpdateObservation{
		uncheckableReason: reason,
	})
	if err != nil {
		t.Fatalf("applyContainerImageObservation: %v", err)
	}
	if result.needsUpdate {
		t.Fatal("local image was marked as needing an update")
	}

	status, ok := ReadStatusSnapshot().ForContainer(candidate.inspect.ID)
	if !ok {
		t.Fatal("uncheckable status was not persisted")
	}
	if status.CheckState != DockerUpdateCheckStateUncheckable || status.CheckReason != reason || status.Err != "" {
		t.Fatalf("status = %+v, want non-error uncheckable state", status)
	}
}

func TestScheduledStoppedContainerIsDeferredWithoutRunError(t *testing.T) {
	withTempUpdateStatusPath(t)
	inspect := standaloneTestInspect()
	inspect.State = &container.State{Status: container.StateExited}

	skipped, err := skipStoppedScheduledContainer(context.Background(), inspect)
	if err != nil || !skipped {
		t.Fatalf("skipStoppedScheduledContainer = %v, %v", skipped, err)
	}
	status, ok := ReadStatusSnapshot().ForContainer(inspect.ID)
	if !ok {
		t.Fatal("deferred update status was not persisted")
	}
	if status.CheckState != DockerUpdateCheckStateAvailable ||
		!status.UpdateAvailable || status.Err != "" ||
		!strings.Contains(status.CheckReason, "container is stopped") {
		t.Fatalf("status = %+v, want non-error deferred update", status)
	}
}
