package btrfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

// SnapshotRoot is the top-level directory reserved for LinuxIO snapshots.
const SnapshotRoot = "linuxio-snapshots"

// snapshotTimeLayout is the UTC timestamp format of a snapshot's leaf name.
const snapshotTimeLayout = "20060102T150405Z"

// SnapshotKind records why a snapshot was taken.
type SnapshotKind string

const (
	KindManual      SnapshotKind = "manual"
	KindTimeline    SnapshotKind = "timeline"
	KindPreUpdate   SnapshotKind = "pre-update"
	KindPreRollback SnapshotKind = "pre-rollback"
)

var snapshotKinds = []SnapshotKind{KindManual, KindTimeline, KindPreUpdate, KindPreRollback}

// BtrfsSnapshot is a read-only snapshot under SnapshotRoot. CreatedAt comes
// from the path timestamp, not the subvolume's otime.
type BtrfsSnapshot struct {
	Source          string       `json:"source"`
	Path            string       `json:"path"`
	Kind            SnapshotKind `json:"kind"`
	CreatedAt       time.Time    `json:"createdAt"`
	ReadOnly        bool         `json:"readOnly"`
	ID              int          `json:"id"`
	UUID            string       `json:"uuid"`
	ReceivedUUID    string       `json:"receivedUuid"`
	ReferencedBytes *uint64      `json:"referencedBytes,omitzero"`
	ExclusiveBytes  *uint64      `json:"exclusiveBytes,omitzero"`
	// MaxReferencedBytes is the qgroup size limit, nil when unlimited.
	MaxReferencedBytes *uint64 `json:"maxReferencedBytes,omitzero"`
}

var (
	escapeReplacer   = strings.NewReplacer("%", "%25", "/", "%2F")
	unescapeReplacer = strings.NewReplacer("%2F", "/", "%25", "%")
)

// EscapeSource turns a subvolume path into a single path segment.
func EscapeSource(path string) string { return escapeReplacer.Replace(path) }

// UnescapeSource reverses EscapeSource.
func UnescapeSource(segment string) string { return unescapeReplacer.Replace(segment) }

// SnapshotPath returns the top-level-relative path of a snapshot.
func SnapshotPath(source string, at time.Time, kind SnapshotKind) string {
	return SnapshotRoot + "/" + EscapeSource(source) + "/" + at.UTC().Format(snapshotTimeLayout) + "-" + string(kind)
}

// ParseSnapshotPath is the strict inverse of SnapshotPath; ok is false for
// any path LinuxIO did not create.
func ParseSnapshotPath(p string) (source string, at time.Time, kind SnapshotKind, ok bool) {
	parts := strings.Split(p, "/")
	if len(parts) != 3 || parts[0] != SnapshotRoot || parts[1] == "" {
		return "", time.Time{}, "", false
	}
	stamp, k, found := strings.Cut(parts[2], "-")
	if !found || !slices.Contains(snapshotKinds, SnapshotKind(k)) {
		return "", time.Time{}, "", false
	}
	at, err := time.Parse(snapshotTimeLayout, stamp)
	if err != nil {
		return "", time.Time{}, "", false
	}
	return UnescapeSource(parts[1]), at, SnapshotKind(k), true
}

// snapshotFrom builds a BtrfsSnapshot from a subvolume row whose path parses.
func snapshotFrom(sv BtrfsSubvolume) (BtrfsSnapshot, bool) {
	source, at, kind, ok := ParseSnapshotPath(sv.Path)
	if !ok {
		return BtrfsSnapshot{}, false
	}
	return BtrfsSnapshot{
		Source: source, Path: sv.Path, Kind: kind, CreatedAt: at,
		ReadOnly: sv.ReadOnly, ID: sv.ID, UUID: sv.UUID, ReceivedUUID: sv.ReceivedUUID,
		ReferencedBytes: sv.ReferencedBytes, ExclusiveBytes: sv.ExclusiveBytes,
		MaxReferencedBytes: sv.MaxReferencedBytes,
	}, true
}

// CreateSnapshot takes a read-only snapshot of the subvolume at source.
func CreateSnapshot(ctx context.Context, uuid, source string, kind SnapshotKind, now time.Time) (BtrfsSnapshot, error) {
	if err := ValidateSubvolumePath(source); err != nil {
		return BtrfsSnapshot{}, err
	}
	if !slices.Contains(snapshotKinds, kind) {
		return BtrfsSnapshot{}, fmt.Errorf("invalid snapshot kind %q", kind)
	}
	if _, err := requireSubvolume(ctx, uuid, source); err != nil {
		return BtrfsSnapshot{}, err
	}
	top, err := TopLevel(ctx, uuid)
	if err != nil {
		return BtrfsSnapshot{}, err
	}
	dest := SnapshotPath(source, now, kind)
	if err = os.MkdirAll(filepath.Join(top, SnapshotRoot, EscapeSource(source)), 0o755); err != nil {
		return BtrfsSnapshot{}, fmt.Errorf("create snapshot directory for %q: %w", source, err)
	}
	if err = subvolOps.snapshot(filepath.Join(top, source), filepath.Join(top, dest), true); err != nil {
		return BtrfsSnapshot{}, fmt.Errorf("snapshot subvolume %q: %w", source, err)
	}
	created, err := requireSubvolume(ctx, uuid, dest)
	if err != nil {
		return BtrfsSnapshot{}, err
	}
	snap, _ := snapshotFrom(created)
	return snap, nil
}

// ListSnapshots returns LinuxIO snapshots newest first, optionally only those
// of one source. Foreign subvolumes under SnapshotRoot are ignored.
func ListSnapshots(ctx context.Context, uuid string, source *string) ([]BtrfsSnapshot, error) {
	subvols, err := ListSubvolumes(ctx, uuid)
	if err != nil {
		return nil, err
	}
	snaps := []BtrfsSnapshot{}
	for _, sv := range subvols {
		if snap, ok := snapshotFrom(sv); ok && (source == nil || snap.Source == *source) {
			snaps = append(snaps, snap)
		}
	}
	slices.SortStableFunc(snaps, func(a, b BtrfsSnapshot) int { return b.CreatedAt.Compare(a.CreatedAt) })
	return snaps, nil
}

// DeleteSnapshot deletes a LinuxIO snapshot. It refuses mounted snapshots and
// succeeds when the snapshot is already gone.
func DeleteSnapshot(ctx context.Context, uuid, snapshotPath string) error {
	if _, _, _, ok := ParseSnapshotPath(snapshotPath); !ok {
		return errors.New("not a linuxio snapshot path")
	}
	if err := deleteUnmounted(ctx, uuid, snapshotPath); err != nil && !errors.Is(err, unix.ENOENT) {
		return err
	}
	return nil
}
