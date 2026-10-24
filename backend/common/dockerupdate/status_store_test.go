package dockerupdate

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/image"
	"golang.org/x/sys/unix"
)

func TestUpdateStatusStoreRoundTrip(t *testing.T) {
	withTempUpdateStatusPath(t)
	checkedAt := time.Date(2026, 6, 24, 12, 30, 0, 0, time.UTC)

	if err := writeUpdateStatuses(context.Background(), []ImageUpdateStatus{
		{
			ContainerID:     "container-1",
			ContainerName:   "nginx",
			ImageID:         "sha256:image-1",
			ImageRef:        "nginx:latest",
			UpdateAvailable: true,
			CheckState:      DockerUpdateCheckStateAvailable,
			LocalDigest:     "sha256:local",
			RemoteDigest:    "sha256:remote",
			CheckedAt:       checkedAt,
		},
	}); err != nil {
		t.Fatalf("writeUpdateStatuses: %v", err)
	}

	snap := ReadStatusSnapshot()
	status, ok := snap.ForContainer("container-1")
	if !ok {
		t.Fatal("ForContainer(container-1) missed")
	}
	if !status.UpdateAvailable || !status.CheckedAt.Equal(checkedAt) {
		t.Fatalf("container status = %+v", status)
	}
	if status.LocalDigest != "sha256:local" || status.RemoteDigest != "sha256:remote" {
		t.Fatalf("container digests = %+v", status)
	}
	imageStatus, ok := snap.ForImage(image.Summary{ID: "sha256:image-1"})
	if !ok || !imageStatus.UpdateAvailable {
		t.Fatalf("ForImage = %+v, %v", imageStatus, ok)
	}
}

func TestUpdateStatusStoreMissingAndCorruptReadAsEmpty(t *testing.T) {
	path := withTempUpdateStatusPath(t)
	if _, ok := ReadStatusSnapshot().ForContainer("missing"); ok {
		t.Fatal("missing file returned a status")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, ok := ReadStatusSnapshot().ForContainer("missing"); ok {
		t.Fatal("corrupt file returned a status")
	}
}

func TestMergeUpdateStatusesOverlaysEntry(t *testing.T) {
	withTempUpdateStatusPath(t)
	oldTime := time.Date(2026, 6, 24, 1, 0, 0, 0, time.UTC)
	newTime := oldTime.Add(time.Hour)
	if err := writeUpdateStatuses(context.Background(), []ImageUpdateStatus{
		{ContainerID: "container-1", ContainerName: "nginx", CheckState: DockerUpdateCheckStateCurrent, ImageID: "sha256:image-1", CheckedAt: oldTime},
		{ContainerID: "container-2", ContainerName: "redis", CheckState: DockerUpdateCheckStateCurrent, ImageID: "sha256:image-2", CheckedAt: oldTime},
	}); err != nil {
		t.Fatalf("writeUpdateStatuses: %v", err)
	}

	if err := mergeUpdateStatuses(context.Background(), []ImageUpdateStatus{
		{
			ContainerID:     "container-1",
			ContainerName:   "nginx",
			ImageID:         "sha256:image-1",
			UpdateAvailable: true,
			CheckState:      DockerUpdateCheckStateAvailable,
			CheckedAt:       newTime,
		},
	}); err != nil {
		t.Fatalf("mergeUpdateStatuses: %v", err)
	}

	snap := ReadStatusSnapshot()
	status, ok := snap.ForContainer("container-1")
	if !ok || !status.UpdateAvailable || !status.CheckedAt.Equal(newTime) {
		t.Fatalf("container-1 status = %+v, %v", status, ok)
	}
	status, ok = snap.ForContainer("container-2")
	if !ok || status.UpdateAvailable || !status.CheckedAt.Equal(oldTime) {
		t.Fatalf("container-2 status = %+v, %v", status, ok)
	}
}

func TestMergeUpdateStatusesRemovesOldContainerID(t *testing.T) {
	withTempUpdateStatusPath(t)
	if err := writeUpdateStatuses(context.Background(), []ImageUpdateStatus{
		{ContainerID: "old-container", ContainerName: "nginx", CheckState: DockerUpdateCheckStateAvailable, ImageID: "sha256:old", UpdateAvailable: true, CheckedAt: time.Now()},
	}); err != nil {
		t.Fatalf("writeUpdateStatuses: %v", err)
	}

	if err := mergeUpdateStatuses(context.Background(), []ImageUpdateStatus{
		{ContainerID: "new-container", ContainerName: "nginx", CheckState: DockerUpdateCheckStateCurrent, ImageID: "sha256:new", CheckedAt: time.Now()},
	}, "old-container"); err != nil {
		t.Fatalf("mergeUpdateStatuses: %v", err)
	}

	snap := ReadStatusSnapshot()
	if _, ok := snap.ForContainer("old-container"); ok {
		t.Fatal("old container status was not removed")
	}
	if _, ok := snap.ForContainer("new-container"); !ok {
		t.Fatal("new container status was not written")
	}
}

func TestUpdateStatusSnapshotPrefersAvailableImageStatus(t *testing.T) {
	snap := NewStatusSnapshot([]ImageUpdateStatus{
		{ContainerID: "container-1", CheckState: DockerUpdateCheckStateCurrent, ImageID: "sha256:shared", CheckedAt: time.Now()},
		{ContainerID: "container-2", CheckState: DockerUpdateCheckStateAvailable, ImageID: "sha256:shared", UpdateAvailable: true, CheckedAt: time.Now()},
	})

	status, ok := snap.ForImage(image.Summary{ID: "sha256:shared"})
	if !ok || !status.UpdateAvailable {
		t.Fatalf("ForImage = %+v, %v", status, ok)
	}
}

func TestUpdateStatusSnapshotMatchesImageRef(t *testing.T) {
	snap := NewStatusSnapshot([]ImageUpdateStatus{
		{ContainerName: "nginx", CheckState: DockerUpdateCheckStateAvailable, ImageRef: "nginx:latest", UpdateAvailable: true, CheckedAt: time.Now()},
	})

	status, ok := snap.ForImage(image.Summary{RepoTags: []string{"nginx:latest"}})
	if !ok || !status.UpdateAvailable {
		t.Fatalf("ForImage = %+v, %v", status, ok)
	}
}

func TestMergeUpdateStatusesWaitsForFileLock(t *testing.T) {
	withTempUpdateStatusPath(t)
	lockPath := updateStatusLockPath()
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	defer f.Close()
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		t.Fatalf("Flock: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- mergeUpdateStatuses(context.Background(), []ImageUpdateStatus{
			{ContainerID: "container-1", ContainerName: "nginx", CheckState: DockerUpdateCheckStateCurrent, ImageID: "sha256:image-1", CheckedAt: time.Now()},
		})
	}()

	select {
	case err := <-done:
		t.Fatalf("mergeUpdateStatuses finished while lock was held: %v", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		t.Fatalf("unlock: %v", err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("mergeUpdateStatuses: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("mergeUpdateStatuses did not finish after lock release")
	}
}

func TestConcurrentMergeUpdateStatusesKeepAllEntries(t *testing.T) {
	withTempUpdateStatusPath(t)

	var wg sync.WaitGroup
	for _, id := range []string{"container-1", "container-2", "container-3"} {
		wg.Go(func() {
			if err := mergeUpdateStatuses(context.Background(), []ImageUpdateStatus{
				{ContainerID: id, ContainerName: id, CheckState: DockerUpdateCheckStateCurrent, ImageID: "sha256:" + id, CheckedAt: time.Now()},
			}); err != nil {
				t.Errorf("mergeUpdateStatuses(%s): %v", id, err)
			}
		})
	}
	wg.Wait()

	snap := ReadStatusSnapshot()
	for _, id := range []string{"container-1", "container-2", "container-3"} {
		if _, ok := snap.ForContainer(id); !ok {
			t.Fatalf("missing status for %s", id)
		}
	}
}

func withTempUpdateStatusPath(t *testing.T) string {
	t.Helper()
	oldPath := updateStatusPath
	path := filepath.Join(t.TempDir(), "docker-update-status.json")
	updateStatusPath = path
	oldJournal := defaultStandaloneUpdateJournal
	defaultStandaloneUpdateJournal.path = filepath.Join(filepath.Dir(path), "docker-update-transaction.json")
	t.Cleanup(func() {
		updateStatusPath = oldPath
		defaultStandaloneUpdateJournal = oldJournal
	})
	return path
}
