package btrfs

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// BtrfsSettings is the quota state and compression of a filesystem.
type BtrfsSettings struct {
	UUID          string           `json:"uuid"`
	QuotasEnabled bool             `json:"quotasEnabled"`
	Compression   BtrfsCompression `json:"compression"`
}

// BtrfsCompression is the compression the filesystem runs with: btrfs applies
// the compress option filesystem-wide, so it is read from the first user
// mount. The caller that knows /etc/fstab fills FstabPersisted.
type BtrfsCompression struct {
	Algorithm      string `json:"algorithm"` // none|zstd|lzo|zlib
	Level          *int   `json:"level,omitzero"`
	Force          bool   `json:"force"`
	FstabPersisted bool   `json:"fstabPersisted"`
}

// QgroupUsage is the space accounting of one subvolume's level-0 qgroup.
type QgroupUsage struct {
	Referenced    uint64
	Exclusive     uint64
	MaxReferenced *uint64
}

// errQuotasDisabled is returned by qgroupShow when quotas are off.
var errQuotasDisabled = errors.New("quotas not enabled")

// compressionLevels maps each algorithm to its valid level range; levelless
// algorithms have a zero range.
var compressionLevels = map[string][2]int{"none": {}, "lzo": {}, "zstd": {1, 15}, "zlib": {1, 9}}

// ParseCompressOption parses a compress=<alg>[:<level>] or
// compress-force=<alg>[:<level>] mount option; the bare compress and
// compress-force options mean zlib. ok is false for any other option.
func ParseCompressOption(opt string) (algorithm string, level *int, force, ok bool) {
	key, val, found := strings.Cut(opt, "=")
	if key != "compress" && key != "compress-force" {
		return "", nil, false, false
	}
	if !found {
		return "zlib", nil, key == "compress-force", true
	}
	algorithm, lvl, hasLevel := strings.Cut(val, ":")
	if algorithm == "no" {
		algorithm = "none"
	}
	if hasLevel {
		if n, err := strconv.Atoi(lvl); err == nil {
			level = &n
		}
	}
	return algorithm, level, key == "compress-force", true
}

// GetSettings reports whether quotas are enabled and the compression option
// of the filesystem's first user mount.
func GetSettings(ctx context.Context, uuid string) (BtrfsSettings, error) {
	top, err := TopLevel(ctx, uuid)
	if err != nil {
		return BtrfsSettings{}, err
	}
	settings := BtrfsSettings{UUID: uuid, Compression: BtrfsCompression{Algorithm: "none"}}
	_, err = readQgroups(top)
	if err != nil && !errors.Is(err, errQuotasDisabled) {
		return BtrfsSettings{}, err
	}
	settings.QuotasEnabled = err == nil
	mounts, err := listMounts(ctx)
	if err != nil {
		return BtrfsSettings{}, fmt.Errorf("list btrfs mounts: %w", err)
	}
	if m, ok := firstUserMount(mounts, uuid); ok {
		for _, opt := range m.Options {
			if alg, level, force, ok := ParseCompressOption(opt); ok {
				settings.Compression = BtrfsCompression{Algorithm: alg, Level: level, Force: force}
			}
		}
	}
	return settings, nil
}

// firstUserMount returns the first mount of filesystem uuid other than the
// one TopLevel makes under TopLevelDir.
func firstUserMount(mounts []Mount, uuid string) (Mount, bool) {
	for _, m := range mounts {
		if m.UUID == uuid && m.Mountpoint != filepath.Join(TopLevelDir, uuid) {
			return m, true
		}
	}
	return Mount{}, false
}

// SetQuotas enables or disables quotas (qgroups) on the filesystem.
func SetQuotas(ctx context.Context, uuid string, enabled bool) error {
	top, err := TopLevel(ctx, uuid)
	if err != nil {
		return err
	}
	if err := subvolOps.quota(top, enabled); err != nil {
		if enabled {
			return fmt.Errorf("enable quotas: %w", err)
		}
		return fmt.Errorf("disable quotas: %w", err)
	}
	return nil
}

// SetQgroupLimit sets the referenced-size limit of the subvolume at path, or
// removes it when max is nil, and returns the subvolume.
func SetQgroupLimit(ctx context.Context, uuid, path string, max *uint64) (BtrfsSubvolume, error) {
	if max != nil && *max == 0 {
		return BtrfsSubvolume{}, errors.New("limit must be greater than zero; omit it to remove the limit")
	}
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
	if err := subvolOps.limit(filepath.Join(top, path), max); err != nil {
		return BtrfsSubvolume{}, fmt.Errorf("set size limit on %q: %w", path, err)
	}
	return requireSubvolume(ctx, uuid, path)
}

// SetCompression remounts the filesystem's first user mount with the
// compression option, which btrfs applies to every mount of the filesystem,
// and returns the option so the caller can persist it. "none" yields
// compress=no.
func SetCompression(ctx context.Context, uuid, algorithm string, level *int, force bool) (string, error) {
	bounds, ok := compressionLevels[algorithm]
	if !ok {
		return "", fmt.Errorf("unsupported compression algorithm %s", algorithm)
	}
	if level != nil && (bounds == [2]int{} || *level < bounds[0] || *level > bounds[1]) {
		return "", fmt.Errorf("invalid compression level %d for %s", *level, algorithm)
	}
	mounts, err := listMounts(ctx)
	if err != nil {
		return "", fmt.Errorf("list btrfs mounts: %w", err)
	}
	mount, ok := firstUserMount(mounts, uuid)
	if !ok {
		return "", fmt.Errorf("filesystem %s is not mounted", uuid)
	}
	option := "compress=no"
	if algorithm != "none" {
		option = "compress=" + algorithm
		if force {
			option = "compress-force=" + algorithm
		}
		if level != nil {
			option += ":" + strconv.Itoa(*level)
		}
	}
	// A remount resets the mount flags it is not given, so the current ones
	// are repeated; the data string only needs the option that changes.
	flags, _ := splitMountOptions(mount.Options)
	if err := mountFS("", mount.Mountpoint, "", unix.MS_REMOUNT|flags, option); err != nil {
		return "", fmt.Errorf("remount %s with %s: %w", mount.Mountpoint, option, err)
	}
	return option, nil
}

// SetSubvolumeCompression sets the compression property of the subvolume at
// path ("none" clears it) and returns the subvolume.
func SetSubvolumeCompression(ctx context.Context, uuid, path, algorithm string) (BtrfsSubvolume, error) {
	if _, ok := compressionLevels[algorithm]; !ok {
		return BtrfsSubvolume{}, fmt.Errorf("unsupported compression algorithm %s", algorithm)
	}
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
	value := algorithm
	if algorithm == "none" {
		value = ""
	}
	if err := subvolOps.compression(filepath.Join(top, path), value); err != nil {
		return BtrfsSubvolume{}, fmt.Errorf("set compression on %q: %w", path, err)
	}
	return requireSubvolume(ctx, uuid, path)
}
