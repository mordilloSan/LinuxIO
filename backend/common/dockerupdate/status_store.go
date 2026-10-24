package dockerupdate

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
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

	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	updateStatusVersion = 2
)

var (
	updateStatusPath = filepath.Join(version.DataDir, "docker-update-status.json")
	updateStatusMu   sync.Mutex
)

// ImageUpdateStatus is the persisted result of the last update check for one
// container and its image.
type ImageUpdateStatus struct {
	ContainerID     string                 `json:"container_id,omitempty"`
	ContainerName   string                 `json:"container_name,omitempty"`
	CheckReason     string                 `json:"check_reason,omitempty"`
	CheckState      DockerUpdateCheckState `json:"check_state"`
	ImageID         string                 `json:"image_id,omitempty"`
	ImageRef        string                 `json:"image_ref,omitempty"`
	UpdateAvailable bool                   `json:"update_available"`
	LocalDigest     string                 `json:"local_digest,omitempty"`
	RemoteDigest    string                 `json:"remote_digest,omitempty"`
	CheckedAt       time.Time              `json:"checked_at"`
	Err             string                 `json:"error,omitempty"`
}

type updateStatusDocument struct {
	Version  int                 `json:"version"`
	Statuses []ImageUpdateStatus `json:"statuses"`
}

// StatusSnapshot indexes the persisted statuses for lookup by container or image.
type StatusSnapshot struct {
	byContainerID   map[string]ImageUpdateStatus
	byContainerName map[string]ImageUpdateStatus
	byImageID       map[string]ImageUpdateStatus
	byImageRef      map[string]ImageUpdateStatus
}

// ReadStatusSnapshot loads the persisted update statuses; a missing or invalid
// file reads as empty.
func ReadStatusSnapshot() StatusSnapshot {
	return NewStatusSnapshot(readUpdateStatusFile())
}

// NewStatusSnapshot indexes statuses by container ID, container name, image ID
// and image reference.
func NewStatusSnapshot(statuses []ImageUpdateStatus) StatusSnapshot {
	snap := StatusSnapshot{
		byContainerID:   make(map[string]ImageUpdateStatus, len(statuses)),
		byContainerName: make(map[string]ImageUpdateStatus, len(statuses)),
		byImageID:       make(map[string]ImageUpdateStatus, len(statuses)),
		byImageRef:      make(map[string]ImageUpdateStatus, len(statuses)),
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

func (s StatusSnapshot) ForContainer(containerID string) (ImageUpdateStatus, bool) {
	status, ok := s.byContainerID[containerID]
	return status, ok
}

func (s StatusSnapshot) ForContainerName(containerName string) (ImageUpdateStatus, bool) {
	status, ok := s.byContainerName[containerName]
	return status, ok
}

// ForImage matches an image by ID first, then by any of its tags or digests.
func (s StatusSnapshot) ForImage(img image.Summary) (ImageUpdateStatus, bool) {
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
	return ImageUpdateStatus{}, false
}

func writeUpdateStatuses(ctx context.Context, statuses []ImageUpdateStatus) error {
	return withUpdateStatusWriteLock(ctx, func() error {
		return writeUpdateStatusFile(statuses)
	})
}

func mergeUpdateStatuses(ctx context.Context, statuses []ImageUpdateStatus, removeContainerIDs ...string) error {
	return withUpdateStatusWriteLock(ctx, func() error {
		remove := make(map[string]struct{}, len(removeContainerIDs))
		for _, id := range removeContainerIDs {
			if id != "" {
				remove[id] = struct{}{}
			}
		}

		merged := make(map[string]ImageUpdateStatus)
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

		out := make([]ImageUpdateStatus, 0, len(merged))
		for _, status := range merged {
			out = append(out, status)
		}
		return writeUpdateStatusFile(out)
	})
}

func readUpdateStatusFile() []ImageUpdateStatus {
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

func writeUpdateStatusFile(statuses []ImageUpdateStatus) error {
	for _, status := range statuses {
		if !validDockerUpdateCheckState(status.CheckState) {
			return fmt.Errorf("write Docker update status: invalid check state %q", status.CheckState)
		}
	}
	doc := updateStatusDocument{
		Version:  updateStatusVersion,
		Statuses: compactUpdateStatuses(statuses),
	}
	data, err := json.Marshal(doc, jsontext.WithIndent("  "), json.Deterministic(true))
	if err != nil {
		return fmt.Errorf("marshal Docker update status: %w", err)
	}
	data = append(data, '\n')
	if err := atomicfile.Write(updateStatusPath, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", updateStatusPath, err)
	}
	return nil
}

func validDockerUpdateCheckState(state DockerUpdateCheckState) bool {
	switch state {
	case DockerUpdateCheckStateCurrent,
		DockerUpdateCheckStateAvailable,
		DockerUpdateCheckStateUncheckable,
		DockerUpdateCheckStateError:
		return true
	default:
		return false
	}
}

func compactUpdateStatuses(statuses []ImageUpdateStatus) []ImageUpdateStatus {
	byKey := make(map[string]ImageUpdateStatus, len(statuses))
	for _, status := range statuses {
		if key := updateStatusStorageKey(status); key != "" {
			byKey[key] = status
		}
	}

	out := make([]ImageUpdateStatus, 0, len(byKey))
	for _, status := range byKey {
		out = append(out, status)
	}
	sort.Slice(out, func(i, j int) bool {
		return updateStatusSortKey(out[i]) < updateStatusSortKey(out[j])
	})
	return out
}

func updateStatusStorageKey(status ImageUpdateStatus) string {
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

func updateStatusSortKey(status ImageUpdateStatus) string {
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

// markContainerCurrent records a freshly updated container (which may have a
// new ID after recreation) as having no pending update.
func markContainerCurrent(ctx context.Context, oldContainerID string, inspect container.InspectResponse) {
	status := ImageUpdateStatus{
		ContainerID:   inspect.ID,
		ContainerName: strings.TrimPrefix(inspect.Name, "/"),
		CheckState:    DockerUpdateCheckStateCurrent,
		ImageID:       inspect.Image,
		CheckedAt:     time.Now(),
	}
	if inspect.Config != nil {
		status.ImageRef = inspect.Config.Image
	}

	if err := mergeUpdateStatuses(ctx, []ImageUpdateStatus{status}, oldContainerID); err != nil {
		slog.Warn("failed to mark Docker container image current", "component", "docker", "container", inspect.ID, "error", err)
		return
	}
	resolveDockerUpdateAlert(ctx, inspect.Name)
}

func markContainerUncheckable(ctx context.Context, inspect container.InspectResponse, reason string) error {
	status := ImageUpdateStatus{
		ContainerID:   inspect.ID,
		ContainerName: strings.TrimPrefix(inspect.Name, "/"),
		CheckReason:   reason,
		CheckState:    DockerUpdateCheckStateUncheckable,
		ImageID:       inspect.Image,
		CheckedAt:     time.Now(),
	}
	if inspect.Config != nil {
		status.ImageRef = inspect.Config.Image
	}
	if err := mergeUpdateStatuses(ctx, []ImageUpdateStatus{status}); err != nil {
		return fmt.Errorf("mark Docker container image uncheckable: %w", err)
	}
	return nil
}

func markContainerUpdateDeferred(ctx context.Context, inspect container.InspectResponse, reason string) error {
	status := ImageUpdateStatus{
		ContainerID:     inspect.ID,
		ContainerName:   strings.TrimPrefix(inspect.Name, "/"),
		CheckReason:     reason,
		CheckState:      DockerUpdateCheckStateAvailable,
		ImageID:         inspect.Image,
		UpdateAvailable: true,
		CheckedAt:       time.Now(),
	}
	if inspect.Config != nil {
		status.ImageRef = inspect.Config.Image
	}
	if err := mergeUpdateStatuses(ctx, []ImageUpdateStatus{status}); err != nil {
		return fmt.Errorf("mark Docker container update deferred: %w", err)
	}
	return nil
}

// PrimaryContainerName returns the first container name without its leading slash.
func PrimaryContainerName(ctr container.Summary) string {
	if len(ctr.Names) == 0 {
		return ""
	}
	return strings.TrimPrefix(ctr.Names[0], "/")
}
