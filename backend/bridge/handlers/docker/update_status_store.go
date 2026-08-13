package docker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	updateStatusVersion = 2
)

var (
	updateStatusPath = filepath.Join(version.DataDir, "docker-update-status.json")
	updateStatusMu   sync.Mutex
)

type imageUpdateStatus struct {
	ContainerID     string                           `json:"container_id,omitempty"`
	ContainerName   string                           `json:"container_name,omitempty"`
	CheckReason     string                           `json:"check_reason,omitempty"`
	CheckState      apischema.DockerUpdateCheckState `json:"check_state"`
	ImageID         string                           `json:"image_id,omitempty"`
	ImageRef        string                           `json:"image_ref,omitempty"`
	UpdateAvailable bool                             `json:"update_available"`
	LocalDigest     string                           `json:"local_digest,omitempty"`
	RemoteDigest    string                           `json:"remote_digest,omitempty"`
	CheckedAt       time.Time                        `json:"checked_at"`
	Err             string                           `json:"error,omitempty"`
}

type updateStatusDocument struct {
	Version  int                 `json:"version"`
	Statuses []imageUpdateStatus `json:"statuses"`
}

type updateStatusSnapshot struct {
	byContainerID   map[string]imageUpdateStatus
	byContainerName map[string]imageUpdateStatus
	byImageID       map[string]imageUpdateStatus
	byImageRef      map[string]imageUpdateStatus
}

func readUpdateStatusSnapshot() updateStatusSnapshot {
	return newUpdateStatusSnapshot(readUpdateStatusFile())
}

func newUpdateStatusSnapshot(statuses []imageUpdateStatus) updateStatusSnapshot {
	snap := updateStatusSnapshot{
		byContainerID:   make(map[string]imageUpdateStatus, len(statuses)),
		byContainerName: make(map[string]imageUpdateStatus, len(statuses)),
		byImageID:       make(map[string]imageUpdateStatus, len(statuses)),
		byImageRef:      make(map[string]imageUpdateStatus, len(statuses)),
	}
	for _, status := range statuses {
		if status.ContainerID != "" {
			snap.byContainerID[status.ContainerID] = status
		}
		if status.ContainerName != "" {
			snap.byContainerName[status.ContainerName] = status
		}
		if status.ImageID != "" {
			existing, ok := snap.byImageID[status.ImageID]
			if !ok || status.UpdateAvailable || (!existing.UpdateAvailable && existing.Err != "") {
				snap.byImageID[status.ImageID] = status
			}
		}
		if status.ImageRef != "" {
			existing, ok := snap.byImageRef[status.ImageRef]
			if !ok || status.UpdateAvailable || (!existing.UpdateAvailable && existing.Err != "") {
				snap.byImageRef[status.ImageRef] = status
			}
		}
	}
	return snap
}

func (s updateStatusSnapshot) forContainer(containerID string) (imageUpdateStatus, bool) {
	status, ok := s.byContainerID[containerID]
	return status, ok
}

func (s updateStatusSnapshot) forContainerName(containerName string) (imageUpdateStatus, bool) {
	status, ok := s.byContainerName[containerName]
	return status, ok
}

func (s updateStatusSnapshot) forImage(img image.Summary) (imageUpdateStatus, bool) {
	if status, ok := s.byImageID[img.ID]; ok {
		return status, true
	}
	for _, ref := range img.RepoTags {
		if status, ok := s.byImageRef[ref]; ok {
			return status, true
		}
	}
	for _, ref := range img.RepoDigests {
		if status, ok := s.byImageRef[ref]; ok {
			return status, true
		}
	}
	return imageUpdateStatus{}, false
}

func writeUpdateStatuses(ctx context.Context, statuses []imageUpdateStatus) error {
	return withUpdateStatusWriteLock(ctx, func() error {
		return writeUpdateStatusFile(statuses)
	})
}

func mergeUpdateStatuses(ctx context.Context, statuses []imageUpdateStatus, removeContainerIDs ...string) error {
	return withUpdateStatusWriteLock(ctx, func() error {
		remove := make(map[string]struct{}, len(removeContainerIDs))
		for _, id := range removeContainerIDs {
			if id != "" {
				remove[id] = struct{}{}
			}
		}

		merged := make(map[string]imageUpdateStatus)
		for _, status := range readUpdateStatusFile() {
			if _, ok := remove[status.ContainerID]; ok {
				continue
			}
			if key := updateStatusStorageKey(status); key != "" {
				merged[key] = status
			}
		}
		for _, status := range statuses {
			if key := updateStatusStorageKey(status); key != "" {
				merged[key] = status
			}
		}

		out := make([]imageUpdateStatus, 0, len(merged))
		for _, status := range merged {
			out = append(out, status)
		}
		return writeUpdateStatusFile(out)
	})
}

func readUpdateStatusFile() []imageUpdateStatus {
	data, err := os.ReadFile(updateStatusPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Debug("failed to read Docker update status file", "component", "docker", "path", updateStatusPath, "error", err)
		}
		return nil
	}

	var doc updateStatusDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		slog.Debug("failed to parse Docker update status file", "component", "docker", "path", updateStatusPath, "error", err)
		return nil
	}
	if doc.Version != updateStatusVersion {
		slog.Debug("ignoring unsupported Docker update status file version", "component", "docker", "path", updateStatusPath, "version", doc.Version)
		return nil
	}
	for _, status := range doc.Statuses {
		if !validDockerUpdateCheckState(status.CheckState) {
			slog.Debug("ignoring Docker update status file with an invalid check state", "component", "docker", "path", updateStatusPath, "state", status.CheckState)
			return nil
		}
	}
	return doc.Statuses
}

func writeUpdateStatusFile(statuses []imageUpdateStatus) error {
	for _, status := range statuses {
		if !validDockerUpdateCheckState(status.CheckState) {
			return fmt.Errorf("write Docker update status: invalid check state %q", status.CheckState)
		}
	}
	doc := updateStatusDocument{
		Version:  updateStatusVersion,
		Statuses: compactUpdateStatuses(statuses),
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal Docker update status: %w", err)
	}
	data = append(data, '\n')
	if err := utils.WriteFileAtomic(updateStatusPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", updateStatusPath, err)
	}
	return nil
}

func validDockerUpdateCheckState(state apischema.DockerUpdateCheckState) bool {
	switch state {
	case apischema.DockerUpdateCheckStateCurrent,
		apischema.DockerUpdateCheckStateAvailable,
		apischema.DockerUpdateCheckStateUncheckable,
		apischema.DockerUpdateCheckStateError:
		return true
	default:
		return false
	}
}

func compactUpdateStatuses(statuses []imageUpdateStatus) []imageUpdateStatus {
	byKey := make(map[string]imageUpdateStatus, len(statuses))
	for _, status := range statuses {
		if key := updateStatusStorageKey(status); key != "" {
			byKey[key] = status
		}
	}

	out := make([]imageUpdateStatus, 0, len(byKey))
	for _, status := range byKey {
		out = append(out, status)
	}
	sort.Slice(out, func(i, j int) bool {
		return updateStatusSortKey(out[i]) < updateStatusSortKey(out[j])
	})
	return out
}

func updateStatusStorageKey(status imageUpdateStatus) string {
	if status.ContainerName != "" {
		return "container-name:" + status.ContainerName
	}
	if status.ContainerID != "" {
		return "container:" + status.ContainerID
	}
	if status.ImageID != "" {
		return "image:" + status.ImageID
	}
	if status.ImageRef != "" {
		return "image-ref:" + status.ImageRef
	}
	return ""
}

func updateStatusSortKey(status imageUpdateStatus) string {
	return strings.Join([]string{
		status.ContainerName,
		status.ContainerID,
		status.ImageID,
		status.ImageRef,
	}, "\x00")
}

func withUpdateStatusWriteLock(ctx context.Context, fn func() error) error {
	updateStatusMu.Lock()
	defer updateStatusMu.Unlock()

	return filelock.RunExclusive(
		ctx,
		updateStatusLockPath(),
		fn,
		filelock.WithTimeout(dockerUpdateLockWait),
		filelock.WithRetryDelay(dockerUpdateLockPoll),
	)
}

func updateStatusLockPath() string {
	ext := filepath.Ext(updateStatusPath)
	if ext == "" {
		return updateStatusPath + ".lock"
	}
	return strings.TrimSuffix(updateStatusPath, ext) + ".lock"
}

func applyContainerUpdateStatus(info *apischema.ContainerInfo, snap updateStatusSnapshot) {
	if info == nil {
		return
	}
	if status, ok := snap.forContainer(info.ID); ok {
		setContainerUpdateStatus(info, status)
		return
	}
	if status, ok := snap.forContainerName(containerInfoPrimaryName(*info)); ok {
		setContainerUpdateStatus(info, status)
	}
}

func setContainerUpdateStatus(info *apischema.ContainerInfo, status imageUpdateStatus) {
	checkState := status.CheckState
	info.UpdateCheckState = new(checkState)
	info.UpdateCheckReason = utils.OptionalString(status.CheckReason)
	info.UpdateAvailable = nil
	if checkState != apischema.DockerUpdateCheckStateUncheckable {
		info.UpdateAvailable = new(status.UpdateAvailable)
	}
	info.UpdateCheckedAt = new(status.CheckedAt.UnixMilli())
	info.UpdateError = utils.OptionalString(status.Err)
}

func containerInfoPrimaryName(info apischema.ContainerInfo) string {
	if len(info.Names) == 0 {
		return ""
	}
	return strings.TrimPrefix(info.Names[0], "/")
}

// markContainerCurrent records a freshly updated container (which may have a
// new ID after recreation) as having no pending update.
func markContainerCurrent(ctx context.Context, oldContainerID string, inspect container.InspectResponse) {
	status := imageUpdateStatus{
		ContainerID:   inspect.ID,
		ContainerName: strings.TrimPrefix(inspect.Name, "/"),
		CheckState:    apischema.DockerUpdateCheckStateCurrent,
		ImageID:       inspect.Image,
		CheckedAt:     time.Now(),
	}
	if inspect.Config != nil {
		status.ImageRef = inspect.Config.Image
	}

	if err := mergeUpdateStatuses(ctx, []imageUpdateStatus{status}, oldContainerID); err != nil {
		slog.Warn("failed to mark Docker container image current", "component", "docker", "container", inspect.ID, "error", err)
	}
}

func markContainerUncheckable(ctx context.Context, inspect container.InspectResponse, reason string) error {
	status := imageUpdateStatus{
		ContainerID:   inspect.ID,
		ContainerName: strings.TrimPrefix(inspect.Name, "/"),
		CheckReason:   reason,
		CheckState:    apischema.DockerUpdateCheckStateUncheckable,
		ImageID:       inspect.Image,
		CheckedAt:     time.Now(),
	}
	if inspect.Config != nil {
		status.ImageRef = inspect.Config.Image
	}
	if err := mergeUpdateStatuses(ctx, []imageUpdateStatus{status}); err != nil {
		return fmt.Errorf("mark Docker container image uncheckable: %w", err)
	}
	return nil
}

func markContainerUpdateDeferred(ctx context.Context, inspect container.InspectResponse, reason string) error {
	status := imageUpdateStatus{
		ContainerID:     inspect.ID,
		ContainerName:   strings.TrimPrefix(inspect.Name, "/"),
		CheckReason:     reason,
		CheckState:      apischema.DockerUpdateCheckStateAvailable,
		ImageID:         inspect.Image,
		UpdateAvailable: true,
		CheckedAt:       time.Now(),
	}
	if inspect.Config != nil {
		status.ImageRef = inspect.Config.Image
	}
	if err := mergeUpdateStatuses(ctx, []imageUpdateStatus{status}); err != nil {
		return fmt.Errorf("mark Docker container update deferred: %w", err)
	}
	return nil
}

func primaryContainerName(ctr container.Summary) string {
	if len(ctr.Names) == 0 {
		return ""
	}
	return strings.TrimPrefix(ctr.Names[0], "/")
}
