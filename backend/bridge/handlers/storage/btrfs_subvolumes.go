package storage

import (
	"context"
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
		slog.Error("failed to list btrfs filesystems", "error", err)
		return nil, err
	}
	for _, fs := range filesystems {
		btrfs.RaiseAlerts(ctx, fs)
	}
	ensureBtrfsHealthTimerOnce(ctx)
	return filesystems, nil
}

// CreateBtrfsSubvolume creates a subvolume.
func CreateBtrfsSubvolume(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (btrfs.BtrfsSubvolume, error) {
	slog.Info("creating btrfs subvolume", "uuid", req.UUID, "path", req.Path)
	sv, err := btrfs.CreateSubvolume(ctx, req.UUID, req.Path)
	if err != nil {
		slog.Error("failed to create btrfs subvolume", "uuid", req.UUID, "path", req.Path, "error", err)
	}
	return sv, err
}

// DeleteBtrfsSubvolume deletes a subvolume.
func DeleteBtrfsSubvolume(ctx context.Context, req apischema.BtrfsSubvolumeRequest) error {
	slog.Info("deleting btrfs subvolume", "uuid", req.UUID, "path", req.Path)
	if err := btrfs.DeleteSubvolume(ctx, req.UUID, req.Path); err != nil {
		slog.Error("failed to delete btrfs subvolume", "uuid", req.UUID, "path", req.Path, "error", err)
		return err
	}
	return nil
}

// CreateBtrfsSnapshot takes a manual snapshot of a subvolume.
func CreateBtrfsSnapshot(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (btrfs.BtrfsSnapshot, error) {
	slog.Info("creating btrfs snapshot", "uuid", req.UUID, "source", req.Path)
	snap, err := btrfs.CreateSnapshot(ctx, req.UUID, req.Path, btrfs.KindManual, time.Now())
	if err != nil {
		slog.Error("failed to create btrfs snapshot", "uuid", req.UUID, "source", req.Path, "error", err)
	}
	return snap, err
}

// DeleteBtrfsSnapshot deletes a snapshot.
func DeleteBtrfsSnapshot(ctx context.Context, req apischema.BtrfsSnapshotRequest) error {
	slog.Info("deleting btrfs snapshot", "uuid", req.UUID, "path", req.SnapshotPath)
	if err := btrfs.DeleteSnapshot(ctx, req.UUID, req.SnapshotPath); err != nil {
		slog.Error("failed to delete btrfs snapshot", "uuid", req.UUID, "path", req.SnapshotPath, "error", err)
		return err
	}
	return nil
}

// SetBtrfsSubvolumeReadOnly toggles the read-only property of a subvolume.
func SetBtrfsSubvolumeReadOnly(ctx context.Context, req apischema.BtrfsSubvolumeReadOnlyRequest) (btrfs.BtrfsSubvolume, error) {
	slog.Info("setting btrfs subvolume read-only", "uuid", req.UUID, "path", req.Path, "readOnly", req.ReadOnly)
	sv, err := btrfs.SetSubvolumeReadOnly(ctx, req.UUID, req.Path, req.ReadOnly)
	if err != nil {
		slog.Error("failed to set btrfs subvolume read-only", "uuid", req.UUID, "path", req.Path, "error", err)
	}
	return sv, err
}

// SetBtrfsDefaultSubvolume makes a subvolume the filesystem default.
func SetBtrfsDefaultSubvolume(ctx context.Context, req apischema.BtrfsSubvolumeRequest) error {
	slog.Info("setting default btrfs subvolume", "uuid", req.UUID, "path", req.Path)
	if err := btrfs.SetDefaultSubvolume(ctx, req.UUID, req.Path); err != nil {
		slog.Error("failed to set default btrfs subvolume", "uuid", req.UUID, "path", req.Path, "error", err)
		return err
	}
	return nil
}
