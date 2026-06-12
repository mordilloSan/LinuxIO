package docker

import (
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
)

type imageUpdateStatus struct {
	ContainerID     string
	ContainerName   string
	ImageID         string
	ImageRef        string
	UpdateAvailable bool
	CheckedAt       time.Time
	Err             string
}

var imageUpdateCache = struct {
	sync.RWMutex
	byContainerID map[string]imageUpdateStatus
	byImageID     map[string]imageUpdateStatus
}{
	byContainerID: map[string]imageUpdateStatus{},
	byImageID:     map[string]imageUpdateStatus{},
}

func replaceImageUpdateCache(statuses []imageUpdateStatus) {
	byContainerID := make(map[string]imageUpdateStatus, len(statuses))
	byImageID := make(map[string]imageUpdateStatus, len(statuses))
	for _, status := range statuses {
		if status.ContainerID != "" {
			byContainerID[status.ContainerID] = status
		}
		if status.ImageID != "" {
			existing, ok := byImageID[status.ImageID]
			if !ok || status.UpdateAvailable || (!existing.UpdateAvailable && existing.Err != "") {
				byImageID[status.ImageID] = status
			}
		}
	}

	imageUpdateCache.Lock()
	imageUpdateCache.byContainerID = byContainerID
	imageUpdateCache.byImageID = byImageID
	imageUpdateCache.Unlock()
}

func imageUpdateStatusForContainer(containerID string) (imageUpdateStatus, bool) {
	imageUpdateCache.RLock()
	defer imageUpdateCache.RUnlock()
	status, ok := imageUpdateCache.byContainerID[containerID]
	return status, ok
}

func imageUpdateStatusForImage(img image.Summary) (imageUpdateStatus, bool) {
	imageUpdateCache.RLock()
	defer imageUpdateCache.RUnlock()
	status, ok := imageUpdateCache.byImageID[img.ID]
	return status, ok
}

// markContainerCurrent records a freshly updated container (which may have a
// new ID after recreation) as having no pending update.
func markContainerCurrent(oldContainerID string, inspect container.InspectResponse) {
	status := imageUpdateStatus{
		ContainerID:   inspect.ID,
		ContainerName: strings.TrimPrefix(inspect.Name, "/"),
		ImageID:       inspect.Image,
		CheckedAt:     time.Now(),
	}
	if inspect.Config != nil {
		status.ImageRef = inspect.Config.Image
	}

	imageUpdateCache.Lock()
	delete(imageUpdateCache.byContainerID, oldContainerID)
	imageUpdateCache.byContainerID[inspect.ID] = status
	imageUpdateCache.byImageID[inspect.Image] = status
	imageUpdateCache.Unlock()
}

func primaryContainerName(ctr container.Summary) string {
	if len(ctr.Names) == 0 {
		return ""
	}
	return strings.TrimPrefix(ctr.Names[0], "/")
}
