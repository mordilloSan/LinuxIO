package docker

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

func TestScheduledStandaloneCleanupFailurePreservesCurrentStatus(t *testing.T) {
	withTempUpdateStatusPath(t)
	ctx := context.Background()
	before := standaloneTestInspect()
	after := readyContainer("replacement", "sha256:new")
	markContainerCurrent(ctx, before.ID, after)

	updated := apischemaUpdateResult(before)
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

	snapshot := readUpdateStatusSnapshot()
	status, ok := snapshot.forContainerName("web")
	if !ok {
		t.Fatal("replacement status was not retained")
	}
	if status.ContainerID != after.ID || status.ImageID != after.Image || status.UpdateAvailable || status.Err != "" {
		t.Fatalf("replacement status = %+v, want current container without an update error", status)
	}
	if _, ok := snapshot.forContainer(before.ID); ok {
		t.Fatal("old container status was retained")
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

	status, ok := readUpdateStatusSnapshot().forContainer(candidate.inspect.ID)
	if !ok {
		t.Fatal("uncheckable status was not persisted")
	}
	if status.CheckState != apischema.DockerUpdateCheckStateUncheckable || status.CheckReason != reason || status.Err != "" {
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
	status, ok := readUpdateStatusSnapshot().forContainer(inspect.ID)
	if !ok {
		t.Fatal("deferred update status was not persisted")
	}
	if status.CheckState != apischema.DockerUpdateCheckStateAvailable ||
		!status.UpdateAvailable || status.Err != "" ||
		!strings.Contains(status.CheckReason, "container is stopped") {
		t.Fatalf("status = %+v, want non-error deferred update", status)
	}
}
