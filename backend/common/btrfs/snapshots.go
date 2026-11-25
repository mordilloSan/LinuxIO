package btrfs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// SnapshotStrftime is snapshotTimeLayout as strftime writes it, for Samba,
// which reads the time in a snapshot's name to offer it as an earlier
// version (docs/shared-folders.md).
const SnapshotStrftime = "%Y%m%dT%H%M%SZ"

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
	// KeptBy says why the snapshot is still there (ExplainKept); empty when
	// the rules could not be read.
	KeptBy KeptReason `json:"keptBy,omitempty"`
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
	if err = ensureSnapshotDir(top, filepath.Dir(dest)); err != nil {
		return BtrfsSnapshot{}, fmt.Errorf("create snapshot directory for %q: %w", source, err)
	}
	if err = subvolOps.snapshot(top, source, dest, true); err != nil {
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

// ensureSnapshotDir makes dir, SnapshotRoot or a folder of it, and the
// folders on its way (mkdirAllBelow), and keeps them root's alone: a
// snapshot holds the files as they were, permissions included, so whoever
// could read a file then could read it there for as long as the snapshot is
// kept. They are made 0700 whatever the umask (the timer's service runs
// with systemd's 022), and one already there is narrowed (narrowSnapshotDir).
func ensureSnapshotDir(top, dir string) error {
	if first, _, _ := strings.Cut(dir, "/"); first != SnapshotRoot {
		return fmt.Errorf("%q is not a snapshot folder", dir)
	}
	return mkdirAllBelow(top, dir, 0o700, func(fd, depth int) error {
		if depth > 0 {
			return narrowSnapshotDir(fd, false)
		}
		if err := narrowSnapshotDir(fd, true); err != nil {
			return err
		}
		// Every source's folder, not only the one of this snapshot: one
		// that takes no snapshot any more would stay as open as it was.
		// One that cannot be narrowed does not keep this source from its
		// snapshot; the health pass names it at each run.
		if err := narrowSnapshotSources(fd); err != nil {
			slog.Warn("failed to narrow snapshot folders", "top", top, "error", err)
		}
		return nil
	})
}

// snapshotsOwnerUID is who SnapshotRoot and the folders in it belong to.
// A var so unit tests, which are not root, can be the owner.
var snapshotsOwnerUID uint32

// narrowSnapshotSources narrows the folder of each source in SnapshotRoot,
// open at fd (narrowSnapshotDir). It touches only what LinuxIO made: root's
// folders in a SnapshotRoot that is root's, on its filesystem and
// subvolume. In a SnapshotRoot that is someone's own (the top level of a
// disk a user mounted) nothing is opened, and what is not a folder, a link
// included, a subvolume or a mount put there, and a folder of another
// owner are left alone.
func narrowSnapshotSources(fd int) error {
	var root unix.Stat_t
	if err := unix.Fstat(fd, &root); err != nil {
		return err
	}
	if root.Uid != snapshotsOwnerUID {
		return nil
	}
	dup, err := unix.Openat(fd, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	dir := os.NewFile(uintptr(dup), SnapshotRoot)
	defer dir.Close()
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return err
	}
	var errs []error
	for _, name := range names {
		child, err := openAtBelow(fd, name)
		if err != nil {
			continue // a file, a link, or gone meanwhile
		}
		var st unix.Stat_t
		if err = unix.Fstat(child, &st); err == nil && st.Uid == snapshotsOwnerUID && st.Dev == root.Dev {
			err = narrowSnapshotDir(child, false)
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
		unix.Close(child)
	}
	return errors.Join(errs...)
}

// NarrowSnapshotDirs narrows the snapshot folders of every pool whose top
// level is mounted, so folders an earlier version, or a run under an open
// umask, left readable by everyone end root's alone without waiting for the
// next snapshot of each source. The health pass runs it.
func NarrowSnapshotDirs(ctx context.Context) error {
	mounts, err := listMounts(ctx)
	if err != nil {
		return err
	}
	return narrowSnapshotDirsOf(mounts)
}

// narrowSnapshotDirsOf narrows SnapshotRoot (narrowSnapshotDir) and the
// folders in it (narrowSnapshotSources) below each top-level mount among
// mounts, one mount for each filesystem.
func narrowSnapshotDirsOf(mounts []Mount) error {
	seen := map[string]bool{}
	var errs []error
	for _, m := range mounts {
		if m.SubvolID != topLevelSubvolID || m.SubPath != "" || seen[m.UUID] {
			continue
		}
		seen[m.UUID] = true
		fd, err := openBelow(m.Mountpoint, SnapshotRoot)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", m.Mountpoint, err))
			continue
		}
		if err = errors.Join(narrowSnapshotDir(fd, true), narrowSnapshotSources(fd)); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", m.Mountpoint, err))
		}
		unix.Close(fd)
	}
	return errors.Join(errs...)
}

// narrowSnapshotDir takes from the folder open at fd what more than root
// may do in it. SnapshotRoot keeps others' pass-through bit, which a shared
// folder's earlier versions need to reach the folder of its snapshots, and
// lets nobody but root list it. A folder of snapshots with an ACL is one a
// shared folder opened for its earlier versions (SetSnapshotsACL), and is
// left as it is: its group bits are the ACL's mask.
func narrowSnapshotDir(fd int, isRoot bool) error {
	keep := uint32(0o700)
	if isRoot {
		keep = 0o711
	} else {
		hasACL, err := hasAccessACL(fd)
		if err != nil || hasACL {
			return err
		}
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	mode := st.Mode & 0o7777
	if mode&^keep == 0 {
		return nil
	}
	return unix.Fchmod(fd, mode&keep)
}

// hasAccessACL says the folder open at fd has an access ACL beyond its mode
// bits; a filesystem that keeps no ACLs has none.
func hasAccessACL(fd int) (bool, error) {
	_, err := unix.Fgetxattr(fd, aclXattrAccess, nil)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, unix.ENODATA), errors.Is(err, unix.ENOTSUP):
		return false, nil
	}
	return false, err
}
