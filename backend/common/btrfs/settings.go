package btrfs

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
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
	Level          *int   `json:"level,omitempty"`
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

// qgroupShow runs `btrfs qgroup show` on the top-level mount and returns
// errQuotasDisabled when quotas are off.
func qgroupShow(ctx context.Context, top string) (map[int]QgroupUsage, error) {
	out, err := runBtrfs(ctx, "qgroup", "show", "--raw", "-r", top)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "quotas not enabled") {
			return nil, errQuotasDisabled
		}
		return nil, fmt.Errorf("show qgroups: %w", err)
	}
	return parseQgroupShow(out)
}

// parseQgroupShow parses `btrfs qgroup show --raw -r`, keyed by subvolume ID.
// Higher-level qgroups (1/…) are skipped.
func parseQgroupShow(out string) (map[int]QgroupUsage, error) {
	usage := map[int]QgroupUsage{}
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !strings.HasPrefix(fields[0], "0/") {
			continue
		}
		id, err := strconv.Atoi(strings.TrimPrefix(fields[0], "0/"))
		if err != nil {
			return nil, fmt.Errorf("parse qgroup show: invalid qgroupid %q", fields[0])
		}
		var u QgroupUsage
		if u.Referenced, err = strconv.ParseUint(fields[1], 10, 64); err != nil {
			return nil, fmt.Errorf("parse qgroup show: invalid size in %q", line)
		}
		if u.Exclusive, err = strconv.ParseUint(fields[2], 10, 64); err != nil {
			return nil, fmt.Errorf("parse qgroup show: invalid size in %q", line)
		}
		if len(fields) > 3 && fields[3] != "none" {
			limit, err := strconv.ParseUint(fields[3], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse qgroup show: invalid limit in %q", line)
			}
			u.MaxReferenced = &limit
		}
		usage[id] = u
	}
	return usage, nil
}

// GetSettings reports whether quotas are enabled and the compression option
// of the filesystem's first user mount.
func GetSettings(ctx context.Context, uuid string) (BtrfsSettings, error) {
	top, err := TopLevel(ctx, uuid)
	if err != nil {
		return BtrfsSettings{}, err
	}
	settings := BtrfsSettings{UUID: uuid, Compression: BtrfsCompression{Algorithm: "none"}}
	_, err = qgroupShow(ctx, top)
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
	verb := "disable"
	if enabled {
		verb = "enable"
	}
	if _, err := runBtrfs(ctx, "quota", verb, top); err != nil {
		return fmt.Errorf("%s quotas: %w", verb, err)
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
	limit := "none"
	if max != nil {
		limit = strconv.FormatUint(*max, 10)
	}
	if _, err := runBtrfs(ctx, "qgroup", "limit", limit, filepath.Join(top, path)); err != nil {
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
	if _, err := runCommand(ctx, "mount", "-o", "remount,"+option, mount.Mountpoint); err != nil {
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
	if _, err := runBtrfs(ctx, "property", "set", filepath.Join(top, path), "compression", value); err != nil {
		return BtrfsSubvolume{}, fmt.Errorf("set compression on %q: %w", path, err)
	}
	return requireSubvolume(ctx, uuid, path)
}
