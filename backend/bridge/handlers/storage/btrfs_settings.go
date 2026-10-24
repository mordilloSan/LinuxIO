package storage

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// btrfsSetCompression and btrfsListMounts are variables so tests can skip the
// real remount and mount discovery.
var (
	btrfsSetCompression = btrfs.SetCompression
	btrfsListMounts     = btrfs.ListMounts
)

// GetBtrfsSettings is btrfs.GetSettings with FstabPersisted filled from
// /etc/fstab: true when the filesystem has fstab entries and all of them carry
// its compression.
func GetBtrfsSettings(ctx context.Context, uuid string) (btrfs.BtrfsSettings, error) {
	settings, err := btrfs.GetSettings(ctx, uuid)
	if err != nil {
		return btrfs.BtrfsSettings{}, err
	}
	mounts, err := btrfsListMounts(ctx)
	if err != nil {
		return btrfs.BtrfsSettings{}, fmt.Errorf("list btrfs mounts: %w", err)
	}
	settings.Compression.FstabPersisted = compressionPersisted(btrfsFstabEntries(uuid, mounts), settings.Compression)
	return settings, nil
}

// compressionPersisted reports whether there are fstab entries and all of
// them carry compression c.
func compressionPersisted(entries map[string]fstabEntry, c btrfs.BtrfsCompression) bool {
	for _, entry := range entries {
		if !fstabHasCompression(entry, c) {
			return false
		}
	}
	return len(entries) > 0
}

// btrfsFstabEntries returns the btrfs fstab entries of filesystem uuid keyed
// by mountpoint: those whose mountpoint is a mount of it and those whose
// source is UUID=<uuid>.
func btrfsFstabEntries(uuid string, mounts []btrfs.Mount) map[string]fstabEntry {
	entries := map[string]fstabEntry{}
	for mountpoint, entry := range getFstabEntries() {
		if entry.fstype != "btrfs" {
			continue
		}
		mounted := slices.ContainsFunc(mounts, func(m btrfs.Mount) bool { return m.UUID == uuid && m.Mountpoint == mountpoint })
		if mounted || strings.EqualFold(entry.source, "UUID="+uuid) {
			entries[mountpoint] = entry
		}
	}
	return entries
}

// fstabHasCompression reports whether the fstab entry mounts with the same
// compression as c. A missing level matches any: the kernel reports the
// default level the fstab line may omit.
func fstabHasCompression(entry fstabEntry, c btrfs.BtrfsCompression) bool {
	found := false
	for opt := range strings.SplitSeq(entry.options, ",") {
		alg, level, force, ok := btrfs.ParseCompressOption(opt)
		if !ok {
			continue
		}
		if alg == "none" {
			continue
		}
		found = true
		if alg != c.Algorithm || force != c.Force || (level != nil && c.Level != nil && *level != *c.Level) {
			return false
		}
	}
	return found == (c.Algorithm != "none")
}

// SetBtrfsQuotas enables or disables quotas and returns the updated settings.
func SetBtrfsQuotas(ctx context.Context, req apischema.BtrfsQuotaRequest) (btrfs.BtrfsSettings, error) {
	slog.Info("setting btrfs quotas", "uuid", req.UUID, "enabled", req.Enabled)
	if err := btrfs.SetQuotas(ctx, req.UUID, req.Enabled); err != nil {
		return btrfs.BtrfsSettings{}, fmt.Errorf("set btrfs quotas of %s: %w", req.UUID, err)
	}
	return GetBtrfsSettings(ctx, req.UUID)
}

// LimitBtrfsQgroup sets the referenced-size limit of a subvolume.
func LimitBtrfsQgroup(ctx context.Context, req apischema.BtrfsQgroupLimitRequest) (btrfs.BtrfsSubvolume, error) {
	slog.Info("setting btrfs qgroup limit", "uuid", req.UUID, "path", req.Path)
	sv, err := btrfs.SetQgroupLimit(ctx, req.UUID, req.Path, req.MaxReferencedBytes)
	if err != nil {
		return sv, fmt.Errorf("set btrfs qgroup limit of %s in %s: %w", req.Path, req.UUID, err)
	}
	return sv, nil
}

// ApplyBtrfsCompression sets the filesystem compression and returns the
// updated settings.
func ApplyBtrfsCompression(ctx context.Context, req apischema.BtrfsCompressionRequest) (btrfs.BtrfsSettings, error) {
	slog.Info("setting btrfs compression", "uuid", req.UUID, "algorithm", req.Algorithm)
	if err := setBtrfsCompression(ctx, req); err != nil {
		return btrfs.BtrfsSettings{}, fmt.Errorf("set btrfs compression of %s: %w", req.UUID, err)
	}
	return GetBtrfsSettings(ctx, req.UUID)
}

// SetBtrfsSubvolumeCompression sets the compression property of a subvolume.
func SetBtrfsSubvolumeCompression(ctx context.Context, req apischema.BtrfsSubvolumeCompressionRequest) (btrfs.BtrfsSubvolume, error) {
	slog.Info("setting btrfs subvolume compression", "uuid", req.UUID, "path", req.Path, "algorithm", req.Algorithm)
	sv, err := btrfs.SetSubvolumeCompression(ctx, req.UUID, req.Path, req.Algorithm)
	if err != nil {
		return sv, fmt.Errorf("set btrfs compression of subvolume %s in %s: %w", req.Path, req.UUID, err)
	}
	return sv, nil
}

// setBtrfsCompression remounts the filesystem with the compression option and
// rewrites that option in every fstab entry of the filesystem, leaving every
// other option alone.
func setBtrfsCompression(ctx context.Context, req apischema.BtrfsCompressionRequest) error {
	option, err := btrfsSetCompression(ctx, req.UUID, req.Algorithm, req.Level, req.Force)
	if err != nil {
		return err
	}
	mounts, err := btrfsListMounts(ctx)
	if err != nil {
		return fmt.Errorf("remounted with %s but could not list btrfs mounts: %w", option, err)
	}
	replacement := option
	if option == "compress=no" {
		replacement = "" // persisted as absence of the option
	}
	entries := btrfsFstabEntries(req.UUID, mounts)
	for _, mountpoint := range slices.Sorted(maps.Keys(entries)) {
		entry := entries[mountpoint]
		oldOptions := strings.Split(entry.options, ",")
		// The bare compress and compress-force options mean zlib and must go too.
		options := slices.DeleteFunc(slices.Clone(oldOptions), func(o string) bool { return o == "compress" || o == "compress-force" })
		options = replaceFstabOption(options, "compress-force=", "")
		options = replaceFstabOption(options, "compress=", replacement)
		if slices.Equal(oldOptions, options) {
			continue
		}
		if err := updateFstabEntry(mountpoint, entry.source, entry.fstype, options); err != nil {
			return fmt.Errorf("remounted with %s but could not update %s in /etc/fstab: %w", option, mountpoint, err)
		}
	}
	return nil
}
