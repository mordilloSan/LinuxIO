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
	"slices"
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
// container and its image. A container in state built has UpdateAvailable
// when a base image of its image has a newer version, and RemoteDigest then
// stands for the digests of those base images (builtCheck).
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
	// BuiltImages holds the base images of each image a rebuild by LinuxIO
	// built (recordBuiltImage). They are kept beside the statuses, under the
	// same lock, so the check that replaces every status also drops the
	// records of images no longer on the host (writeUpdateStatuses).
	BuiltImages []builtImageRecord `json:"built_images,omitempty"`
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

// writeUpdateStatuses replaces every status with statuses. When images, the
// daemon's image list, is not nil, it also drops the base images recorded for
// an image no longer on the host (pruneBuiltImageRecords).
func writeUpdateStatuses(ctx context.Context, statuses []ImageUpdateStatus, images *hostImages) error {
	return withUpdateStatusWriteLock(ctx, func() error {
		doc := readUpdateStatusDocument()
		doc.Statuses = statuses
		if images != nil {
			doc.BuiltImages = pruneBuiltImageRecords(doc.BuiltImages, *images)
		}
		return writeUpdateStatusDocument(doc)
	})
}

// hostImages is the ID of every image the daemon holds (listHostImages), and
// when that list was asked for.
type hostImages struct {
	ids      map[string]bool
	listedAt time.Time
}

// mergeUpdateStatuses replaces the statuses of the containers statuses name,
// and removes those of removeContainerIDs. The recorded base images stay.
func mergeUpdateStatuses(ctx context.Context, statuses []ImageUpdateStatus, removeContainerIDs ...string) error {
	return withUpdateStatusWriteLock(ctx, func() error {
		remove := make(map[string]struct{}, len(removeContainerIDs))
		for _, id := range removeContainerIDs {
			if id != "" {
				remove[id] = struct{}{}
			}
		}

		doc := readUpdateStatusDocument()
		merged := make(map[string]ImageUpdateStatus)
		for _, status := range doc.Statuses {
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

		doc.Statuses = make([]ImageUpdateStatus, 0, len(merged))
		for _, status := range merged {
			doc.Statuses = append(doc.Statuses, status)
		}
		return writeUpdateStatusDocument(doc)
	})
}

// recordBuiltImage keeps record, replacing what was recorded for its image.
func recordBuiltImage(ctx context.Context, record builtImageRecord) error {
	return withUpdateStatusWriteLock(ctx, func() error {
		doc := readUpdateStatusDocument()
		doc.BuiltImages = slices.DeleteFunc(doc.BuiltImages, func(existing builtImageRecord) bool {
			return existing.ImageID == record.ImageID
		})
		doc.BuiltImages = append(doc.BuiltImages, record)
		return writeUpdateStatusDocument(doc)
	})
}

// readBuiltImageRecords is each recorded image's base images, by image ID.
func readBuiltImageRecords() map[string]builtImageRecord {
	records := make(map[string]builtImageRecord)
	for _, record := range readUpdateStatusDocument().BuiltImages {
		records[record.ImageID] = record
	}
	return records
}

// pruneBuiltImageRecords drops the record of an image that images does not
// hold. A record is written once its image exists, so one written after the
// list was asked for may name an image the list could not show yet: it is
// kept until a later list.
func pruneBuiltImageRecords(records []builtImageRecord, images hostImages) []builtImageRecord {
	return slices.DeleteFunc(records, func(record builtImageRecord) bool {
		return !images.ids[record.ImageID] && record.RecordedAt.Before(images.listedAt)
	})
}

func readUpdateStatusFile() []ImageUpdateStatus {
	return readUpdateStatusDocument().Statuses
}

// readUpdateStatusDocument reads the status file; a missing or invalid file
// reads as empty.
func readUpdateStatusDocument() updateStatusDocument {
	data, err := os.ReadFile(updateStatusPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Debug("failed to read Docker update status file", "component", "docker", "path", updateStatusPath, "error", err)
		}
		return updateStatusDocument{}
	}

	var doc updateStatusDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		slog.Debug("failed to parse Docker update status file", "component", "docker", "path", updateStatusPath, "error", err)
		return updateStatusDocument{}
	}
	if doc.Version != updateStatusVersion {
		slog.Debug("ignoring unsupported Docker update status file version", "component", "docker", "path", updateStatusPath, "version", doc.Version)
		return updateStatusDocument{}
	}
	for _, status := range doc.Statuses {
		if !validDockerUpdateCheckState(status.CheckState) {
			slog.Debug("ignoring Docker update status file with an invalid check state", "component", "docker", "path", updateStatusPath, "state", status.CheckState)
			return updateStatusDocument{}
		}
	}
	return doc
}

func writeUpdateStatusDocument(doc updateStatusDocument) error {
	for _, status := range doc.Statuses {
		if !validDockerUpdateCheckState(status.CheckState) {
			return fmt.Errorf("write Docker update status: invalid check state %q", status.CheckState)
		}
	}
	doc.Version = updateStatusVersion
	doc.Statuses = compactUpdateStatuses(doc.Statuses)
	slices.SortFunc(doc.BuiltImages, func(a, b builtImageRecord) int { return strings.Compare(a.ImageID, b.ImageID) })
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
		DockerUpdateCheckStateBuilt,
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
	markContainerChecked(ctx, oldContainerID, inspect, DockerUpdateCheckStateCurrent, builtCheck{})
}

// markContainerBuilt records a container (which may have a new ID after a
// rebuild recreated it) as running the image its Compose service builds, as
// check found it. A base image with a newer version keeps the container's
// update-available alert, which the check raised.
func markContainerBuilt(ctx context.Context, oldContainerID string, inspect container.InspectResponse, check builtCheck) {
	markContainerChecked(ctx, oldContainerID, inspect, DockerUpdateCheckStateBuilt, check)
}

func markContainerChecked(ctx context.Context, oldContainerID string, inspect container.InspectResponse, state DockerUpdateCheckState, check builtCheck) {
	status := ImageUpdateStatus{
		ContainerID:     inspect.ID,
		ContainerName:   strings.TrimPrefix(inspect.Name, "/"),
		CheckReason:     check.reason,
		CheckState:      state,
		ImageID:         inspect.Image,
		UpdateAvailable: check.baseMoved,
		RemoteDigest:    check.remoteDigest,
		CheckedAt:       time.Now(),
	}
	if inspect.Config != nil {
		status.ImageRef = inspect.Config.Image
	}

	if err := mergeUpdateStatuses(ctx, []ImageUpdateStatus{status}, oldContainerID); err != nil {
		slog.Warn("failed to record the update status of Docker container "+status.ContainerName, "component", "docker", "container", status.ContainerName, "container_id", inspect.ID, "state", state, "error", err)
		return
	}
	if check.baseMoved {
		resolveDockerUpdateFailureAlerts(ctx, inspect.Name)
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
