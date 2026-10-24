package storage

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

// ListBtrfsFilesystems lists the btrfs filesystems, raises their health alerts
// and makes sure the health timer exists.
func ListBtrfsFilesystems(ctx context.Context) ([]btrfs.BtrfsFilesystem, error) {
	filesystems, err := btrfs.ListFilesystems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list btrfs filesystems: %w", err)
	}
	for _, fs := range filesystems {
		btrfs.RaiseAlerts(ctx, fs)
	}
	ensureBtrfsHealthTimerOnce(ctx)
	return filesystems, nil
}

// CreateBtrfsSubvolume creates a subvolume.
func CreateBtrfsSubvolume(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (btrfs.BtrfsSubvolume, error) {
	sv, err := btrfs.CreateSubvolume(ctx, req.UUID, req.Path)
	if err != nil {
		return sv, fmt.Errorf("create btrfs subvolume %s in %s: %w", req.Path, req.UUID, err)
	}
	slog.Info("btrfs subvolume created", "uuid", req.UUID, "path", req.Path)
	return sv, nil
}

// DeleteBtrfsSubvolume deletes a subvolume.
func DeleteBtrfsSubvolume(ctx context.Context, req apischema.BtrfsSubvolumeRequest) error {
	if err := btrfs.DeleteSubvolume(ctx, req.UUID, req.Path); err != nil {
		return fmt.Errorf("delete btrfs subvolume %s in %s: %w", req.Path, req.UUID, err)
	}
	slog.Info("btrfs subvolume deleted", "uuid", req.UUID, "path", req.Path)
	return nil
}

// CreateBtrfsSnapshot takes a manual snapshot of a subvolume.
func CreateBtrfsSnapshot(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (btrfs.BtrfsSnapshot, error) {
	snap, err := btrfs.CreateSnapshot(ctx, req.UUID, req.Path, btrfs.KindManual, time.Now())
	if err != nil {
		return snap, fmt.Errorf("create btrfs snapshot of %s in %s: %w", req.Path, req.UUID, err)
	}
	slog.Info("btrfs snapshot created", "uuid", req.UUID, "source", req.Path)
	return snap, nil
}

// DeleteBtrfsSnapshot deletes a snapshot.
func DeleteBtrfsSnapshot(ctx context.Context, req apischema.BtrfsSnapshotRequest) error {
	if err := btrfs.DeleteSnapshot(ctx, req.UUID, req.SnapshotPath); err != nil {
		return fmt.Errorf("delete btrfs snapshot %s in %s: %w", req.SnapshotPath, req.UUID, err)
	}
	slog.Info("btrfs snapshot deleted", "uuid", req.UUID, "path", req.SnapshotPath)
	return nil
}

// SetBtrfsSubvolumeReadOnly toggles the read-only property of a subvolume.
func SetBtrfsSubvolumeReadOnly(ctx context.Context, req apischema.BtrfsSubvolumeReadOnlyRequest) (btrfs.BtrfsSubvolume, error) {
	sv, err := btrfs.SetSubvolumeReadOnly(ctx, req.UUID, req.Path, req.ReadOnly)
	if err != nil {
		return sv, fmt.Errorf("set btrfs subvolume %s read-only=%t in %s: %w", req.Path, req.ReadOnly, req.UUID, err)
	}
	slog.Info("btrfs subvolume read-only changed", "uuid", req.UUID, "path", req.Path, "readOnly", req.ReadOnly)
	return sv, nil
}

// SetBtrfsDefaultSubvolume makes a subvolume the filesystem default.
func SetBtrfsDefaultSubvolume(ctx context.Context, req apischema.BtrfsSubvolumeRequest) error {
	if err := btrfs.SetDefaultSubvolume(ctx, req.UUID, req.Path); err != nil {
		return fmt.Errorf("set default btrfs subvolume %s in %s: %w", req.Path, req.UUID, err)
	}
	slog.Info("default btrfs subvolume set", "uuid", req.UUID, "path", req.Path)
	return nil
}
