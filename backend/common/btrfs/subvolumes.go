package btrfs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// dockerSubvolumes is where Docker's btrfs driver keeps its layer subvolumes.
const dockerSubvolumes = "var/lib/docker/btrfs/subvolumes/"

// BtrfsSubvolume is one subvolume of a btrfs filesystem. The size fields stay
// nil until quota data is available.
type BtrfsSubvolume struct {
	ID              int        `json:"id"`
	ParentID        int        `json:"parentId"`
	Path            string     `json:"path"`
	UUID            string     `json:"uuid"`
	ParentUUID      string     `json:"parentUuid"`
	ReceivedUUID    string     `json:"receivedUuid"`
	ReadOnly        bool       `json:"readOnly"`
	IsDefault       bool       `json:"isDefault"`
	IsSnapshot      bool       `json:"isSnapshot"`
	CreatedAt       *time.Time `json:"createdAt"`
	Mountpoint      *string    `json:"mountpoint"`
	ReferencedBytes *uint64    `json:"referencedBytes"`
	ExclusiveBytes  *uint64    `json:"exclusiveBytes"`
	// MaxReferencedBytes is the qgroup size limit; nil when unlimited or
	// quota data is unavailable.
	MaxReferencedBytes *uint64 `json:"maxReferencedBytes,omitempty"`
}

// subvolumeRow is one parsed line of `btrfs subvolume list`. Fields the
// chosen list flags do not print stay zero.
type subvolumeRow struct {
	ID, ParentID                   int
	UUID, ParentUUID, ReceivedUUID string
	OTime, Path                    string
}

var (
	subvolumeLine = regexp.MustCompile(`^ID (\d+) gen \d+(?: cgen \d+)?(?: parent (\d+))? top level \d+` +
		`(?: parent_uuid (\S+) received_uuid (\S+) uuid (\S+))?` +
		`(?: otime (\d{4}-\d\d-\d\d \d\d:\d\d:\d\d|-))? path (.+)$`)
	defaultSubvolumeLine = regexp.MustCompile(`^ID (\d+)`)
)

// parseSubvolumeList parses `btrfs subvolume list -a` output of any flag
// combination, stripping the <FS_TREE>/ prefix and mapping "-" to "".
func parseSubvolumeList(out string) ([]subvolumeRow, error) {
	unset := func(s string) string {
		if s == "-" {
			return ""
		}
		return s
	}
	var rows []subvolumeRow
	for line := range strings.SplitSeq(out, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		m := subvolumeLine.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("parse subvolume list: unexpected line %q", line)
		}
		id, _ := strconv.Atoi(m[1])
		parent, _ := strconv.Atoi(m[2])
		rows = append(rows, subvolumeRow{
			ID: id, ParentID: parent,
			ParentUUID: unset(m[3]), ReceivedUUID: unset(m[4]), UUID: unset(m[5]),
			OTime: m[6], Path: strings.TrimPrefix(m[7], "<FS_TREE>/"),
		})
	}
	return rows, nil
}

// ValidateSubvolumePath checks a subvolume path relative to the top level.
func ValidateSubvolumePath(p string) error {
	if p == "" {
		return errors.New("subvolume path is required")
	}
	if strings.HasPrefix(p, "/") {
		return fmt.Errorf("subvolume path %q must be relative", p)
	}
	// A comma or whitespace would break the subvol= mount option and fstab.
	if strings.ContainsAny(p, "\x00,") || strings.ContainsFunc(p, unicode.IsSpace) {
		return fmt.Errorf("invalid subvolume path %q", p)
	}
	for seg := range strings.SplitSeq(p, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return fmt.Errorf("invalid subvolume path %q", p)
		}
	}
	if first, _, _ := strings.Cut(p, "/"); first == SnapshotRoot {
		return errors.New("path is reserved for snapshots")
	}
	return nil
}

func isSnapshotPath(p string) bool {
	return strings.HasPrefix(p, SnapshotRoot+"/")
}

// listRows runs `btrfs subvolume list -a <flags> <top>` and parses it.
func listRows(ctx context.Context, top, what string, flags ...string) ([]subvolumeRow, error) {
	args := append([]string{"subvolume", "list", "-a"}, flags...)
	out, err := runBtrfs(ctx, append(args, top)...)
	if err != nil {
		return nil, fmt.Errorf("list subvolumes (%s): %w", what, err)
	}
	return parseSubvolumeList(out)
}

// defaultSubvolumeID returns the id of the filesystem's default subvolume.
func defaultSubvolumeID(ctx context.Context, top string) (int, error) {
	out, err := runBtrfs(ctx, "subvolume", "get-default", top)
	if err != nil {
		return 0, fmt.Errorf("get default subvolume: %w", err)
	}
	m := defaultSubvolumeLine.FindStringSubmatch(out)
	if m == nil {
		return 0, fmt.Errorf("get default subvolume: unexpected output %q", out)
	}
	return strconv.Atoi(m[1])
}

// ListSubvolumes returns the subvolumes of the filesystem sorted by path,
// excluding Docker's btrfs-driver layers.
func ListSubvolumes(ctx context.Context, uuid string) ([]BtrfsSubvolume, error) {
	top, err := TopLevel(ctx, uuid)
	if err != nil {
		return nil, err
	}
	rows, err := listRows(ctx, top, "details", "-p", "-u", "-q", "-R")
	if err != nil {
		return nil, err
	}
	roRows, err := listRows(ctx, top, "read-only", "-r")
	if err != nil {
		return nil, err
	}
	snapRows, err := listRows(ctx, top, "snapshots", "-s")
	if err != nil {
		return nil, err
	}
	defaultID, err := defaultSubvolumeID(ctx, top)
	if err != nil {
		return nil, err
	}
	mounts, err := listMounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("list btrfs mounts: %w", err)
	}

	readOnly := map[int]bool{}
	for _, r := range roRows {
		readOnly[r.ID] = true
	}
	created := map[int]*time.Time{}
	for _, r := range snapRows {
		if t, err := time.ParseInLocation(time.DateTime, r.OTime, time.Local); err == nil {
			created[r.ID] = &t
		}
	}

	subvols := make([]BtrfsSubvolume, 0, len(rows))
	for _, r := range rows {
		if strings.Contains(r.Path, dockerSubvolumes) {
			continue
		}
		subvols = append(subvols, BtrfsSubvolume{
			ID: r.ID, ParentID: r.ParentID, Path: r.Path,
			UUID: r.UUID, ParentUUID: r.ParentUUID, ReceivedUUID: r.ReceivedUUID,
			ReadOnly: readOnly[r.ID], IsDefault: r.ID == defaultID,
			IsSnapshot: isSnapshotPath(r.Path), CreatedAt: created[r.ID],
			Mountpoint: mountpointOf(mounts, uuid, r.Path),
		})
	}
	// One qgroup show serves both cases: "quotas not enabled" just means no sizes.
	switch usage, err := qgroupShow(ctx, top); {
	case err == nil:
		for i := range subvols {
			if u, ok := usage[subvols[i].ID]; ok {
				subvols[i].ReferencedBytes, subvols[i].ExclusiveBytes = &u.Referenced, &u.Exclusive
				subvols[i].MaxReferencedBytes = u.MaxReferenced
			}
		}
	case !errors.Is(err, errQuotasDisabled):
		slog.Warn("btrfs qgroup sizes unavailable", "uuid", uuid, "error", err)
	}
	slices.SortFunc(subvols, func(a, b BtrfsSubvolume) int { return strings.Compare(a.Path, b.Path) })
	return subvols, nil
}

// mountpointOf returns the mountpoint of exactly the subvolume at path, or nil.
func mountpointOf(mounts []Mount, uuid, path string) *string {
	for _, m := range mounts {
		if m.UUID == uuid && m.Subvol == path {
			return &m.Mountpoint
		}
	}
	return nil
}

// findSubvolume lists the filesystem and returns the subvolume at path; ok is
// false when it does not exist.
func findSubvolume(ctx context.Context, uuid, path string) (sv BtrfsSubvolume, ok bool, err error) {
	subvols, err := ListSubvolumes(ctx, uuid)
	if err != nil {
		return BtrfsSubvolume{}, false, err
	}
	for _, s := range subvols {
		if s.Path == path {
			return s, true, nil
		}
	}
	return BtrfsSubvolume{}, false, nil
}

func requireSubvolume(ctx context.Context, uuid, path string) (BtrfsSubvolume, error) {
	sv, ok, err := findSubvolume(ctx, uuid, path)
	if err != nil {
		return BtrfsSubvolume{}, err
	}
	if !ok {
		return BtrfsSubvolume{}, fmt.Errorf("subvolume %q not found", path)
	}
	return sv, nil
}

// CreateSubvolume creates an empty subvolume at path and returns it.
func CreateSubvolume(ctx context.Context, uuid, path string) (BtrfsSubvolume, error) {
	if err := ValidateSubvolumePath(path); err != nil {
		return BtrfsSubvolume{}, err
	}
	top, err := TopLevel(ctx, uuid)
	if err != nil {
		return BtrfsSubvolume{}, err
	}
	if _, err := runBtrfs(ctx, "subvolume", "create", filepath.Join(top, path)); err != nil {
		return BtrfsSubvolume{}, fmt.Errorf("create subvolume %q: %w", path, err)
	}
	return requireSubvolume(ctx, uuid, path)
}

// DeleteSubvolume deletes the subvolume at path. It refuses the default
// subvolume, mounted subvolumes and snapshots, and succeeds when the
// subvolume is already gone.
func DeleteSubvolume(ctx context.Context, uuid, path string) error {
	if path == SnapshotRoot || isSnapshotPath(path) {
		return errors.New("use the snapshot routes to delete snapshots")
	}
	if err := ValidateSubvolumePath(path); err != nil {
		return err
	}
	sv, ok, err := findSubvolume(ctx, uuid, path)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	if sv.IsDefault {
		return fmt.Errorf("subvolume %q is the default subvolume; set another default first", path)
	}
	return deleteUnmounted(ctx, uuid, path)
}

// deleteUnmounted deletes the subvolume at path unless it, or a subvolume
// below it, is mounted.
func deleteUnmounted(ctx context.Context, uuid, path string) error {
	mounts, err := listMounts(ctx)
	if err != nil {
		return fmt.Errorf("list btrfs mounts: %w", err)
	}
	for _, m := range mounts {
		if m.UUID == uuid && (m.Subvol == path || strings.HasPrefix(m.Subvol, path+"/")) {
			return fmt.Errorf("subvolume %q is mounted at %s; unmount it first", m.Subvol, m.Mountpoint)
		}
	}
	top, err := TopLevel(ctx, uuid)
	if err != nil {
		return err
	}
	if _, err := runBtrfs(ctx, "subvolume", "delete", filepath.Join(top, path)); err != nil {
		return fmt.Errorf("delete subvolume %q: %w", path, err)
	}
	return nil
}

// SetSubvolumeReadOnly toggles the read-only property and returns the subvolume.
func SetSubvolumeReadOnly(ctx context.Context, uuid, path string, ro bool) (BtrfsSubvolume, error) {
	if err := ValidateSubvolumePath(path); err != nil {
		return BtrfsSubvolume{}, err
	}
	if _, err := requireSubvolume(ctx, uuid, path); err != nil {
		return BtrfsSubvolume{}, err
	}
	top, err := TopLevel(ctx, uuid)
	if err != nil {
		return BtrfsSubvolume{}, err
	}
	if _, err := runBtrfs(ctx, "property", "set", "-ts", filepath.Join(top, path), "ro", strconv.FormatBool(ro)); err != nil {
		return BtrfsSubvolume{}, fmt.Errorf("set read-only on %q: %w", path, err)
	}
	return requireSubvolume(ctx, uuid, path)
}

// SetDefaultSubvolume makes the subvolume at path the filesystem default.
func SetDefaultSubvolume(ctx context.Context, uuid, path string) error {
	if err := ValidateSubvolumePath(path); err != nil {
		return err
	}
	sv, err := requireSubvolume(ctx, uuid, path)
	if err != nil {
		return err
	}
	top, err := TopLevel(ctx, uuid)
	if err != nil {
		return err
	}
	if _, err := runBtrfs(ctx, "subvolume", "set-default", strconv.Itoa(sv.ID), top); err != nil {
		return fmt.Errorf("set default subvolume %q: %w", path, err)
	}
	return nil
}
