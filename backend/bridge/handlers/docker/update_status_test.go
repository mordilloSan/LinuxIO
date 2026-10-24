package docker

import (
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/dockerupdate"
)

func TestApplyContainerUpdateStatusFallsBackToContainerName(t *testing.T) {
	checkedAt := time.Date(2026, 6, 24, 12, 30, 0, 0, time.UTC)
	snap := dockerupdate.NewStatusSnapshot([]dockerupdate.ImageUpdateStatus{
		{ContainerName: "nginx", CheckState: apischema.DockerUpdateCheckStateAvailable, ImageRef: "nginx:latest", UpdateAvailable: true, CheckedAt: checkedAt},
	})
	info := apischema.ContainerInfo{
		ID:    "container-1",
		Names: []string{"/nginx"},
	}

	applyContainerUpdateStatus(&info, snap)

	if info.UpdateAvailable == nil || !*info.UpdateAvailable {
		t.Fatalf("UpdateAvailable = %v, want true", info.UpdateAvailable)
	}
	if info.UpdateCheckedAt == nil || *info.UpdateCheckedAt != checkedAt.UnixMilli() {
		t.Fatalf("UpdateCheckedAt = %v, want %d", info.UpdateCheckedAt, checkedAt.UnixMilli())
	}
	if info.UpdateCheckState == nil || *info.UpdateCheckState != apischema.DockerUpdateCheckStateAvailable {
		t.Fatalf("UpdateCheckState = %v, want available", info.UpdateCheckState)
	}
}

func TestApplyUncheckableUpdateStatusLeavesAvailabilityUnset(t *testing.T) {
	checkedAt := time.Date(2026, 8, 13, 12, 30, 0, 0, time.UTC)
	const reason = "local image has no repository digest"
	snap := dockerupdate.NewStatusSnapshot([]dockerupdate.ImageUpdateStatus{{
		ContainerID: "container-1",
		CheckReason: reason,
		CheckState:  apischema.DockerUpdateCheckStateUncheckable,
		CheckedAt:   checkedAt,
	}})
	info := apischema.ContainerInfo{ID: "container-1", UpdateAvailable: new(true)}

	applyContainerUpdateStatus(&info, snap)

	if info.UpdateAvailable != nil {
		t.Fatalf("UpdateAvailable = %v, want nil", info.UpdateAvailable)
	}
	if info.UpdateCheckState == nil || *info.UpdateCheckState != apischema.DockerUpdateCheckStateUncheckable {
		t.Fatalf("UpdateCheckState = %v, want uncheckable", info.UpdateCheckState)
	}
	if info.UpdateCheckReason == nil || *info.UpdateCheckReason != reason {
		t.Fatalf("UpdateCheckReason = %v, want %q", info.UpdateCheckReason, reason)
	}
	if info.UpdateError != nil {
		t.Fatalf("UpdateError = %v, want nil", info.UpdateError)
	}

	img := apischema.DockerImage{UpdateAvailable: new(true)}
	applyImageUpdateStatus(&img, dockerupdate.ImageUpdateStatus{
		CheckReason: reason,
		CheckState:  apischema.DockerUpdateCheckStateUncheckable,
	})
	if img.UpdateAvailable != nil || img.UpdateCheckState == nil ||
		*img.UpdateCheckState != apischema.DockerUpdateCheckStateUncheckable {
		t.Fatalf("DockerImage update status = %+v, want uncheckable with nil availability", img)
	}
	if img.UpdateCheckReason == nil || *img.UpdateCheckReason != reason {
		t.Fatalf("DockerImage UpdateCheckReason = %v, want %q", img.UpdateCheckReason, reason)
	}
}
