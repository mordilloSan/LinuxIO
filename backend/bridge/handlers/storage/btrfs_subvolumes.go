package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/linksafe"
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
		return sv, fmt.Errorf("create btrfs subvolume %s in %s: %w", req.Path, req.UUID, pathStatus(err))
	}
	slog.Info("btrfs subvolume created", "uuid", req.UUID, "path", req.Path)
	return sv, nil
}

// pathStatus gives the error of an operation on a top-level path its client
// status: a path that is or goes through a symbolic link is refused, and one
// that leads onto another filesystem is not a path of this pool. Any other
// error is returned as it is.
func pathStatus(err error) error {
	switch {
	case errors.Is(err, btrfs.ErrLinkOnPath):
		return fmt.Errorf("%w: %w", bridgeipc.ErrForbidden, err)
	case errors.Is(err, btrfs.ErrNotOnFilesystem):
		return fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	return err
}

func btrfsSubvolumeFolderBindings(linkRule linksafe.Owner) apischema.BindingSet {
	return apischema.Bindings(
		apischema.Mutation("storage.btrfs_subvolume_create_in", func(ctx context.Context, req apischema.BtrfsSubvolumeCreateInRequest) (btrfs.BtrfsSubvolume, error) {
			return CreateBtrfsSubvolumeIn(ctx, req, linkRule)
		}, apischema.Privileged()),
	)
}

// CreateBtrfsSubvolumeIn creates a subvolume in a host folder that must
// already exist. A root bridge reaches the folder for linkRule, the session
// user it acts for, without following a link that user could have planted
// (see linksafe.OpenNearestDirNoUserLinks), and creates through its handle.
func CreateBtrfsSubvolumeIn(ctx context.Context, req apischema.BtrfsSubvolumeCreateInRequest, linkRule linksafe.Owner) (btrfs.BtrfsSubvolume, error) {
	if !filepath.IsAbs(req.Folder) {
		return btrfs.BtrfsSubvolume{}, fmt.Errorf("%w: folder %q", bridgeipc.ErrInvalidArgs, req.Folder)
	}
	root, err := fsroot.Open()
	if err != nil {
		return btrfs.BtrfsSubvolume{}, fmt.Errorf("open filesystem root: %w", err)
	}
	defer root.Close()
	dir, missing, err := linksafe.OpenNearestDirNoUserLinks(root, req.Folder, linkRule)
	if errors.Is(err, unix.ENOTDIR) {
		return btrfs.BtrfsSubvolume{}, fmt.Errorf("%w: folder %s: %w", bridgeipc.ErrInvalidArgs, req.Folder, err)
	}
	if err != nil {
		return btrfs.BtrfsSubvolume{}, fmt.Errorf("open folder %s: %w", req.Folder, err)
	}
	defer dir.Close()
	if len(missing) > 0 {
		return btrfs.BtrfsSubvolume{}, fmt.Errorf("%w: folder %s", bridgeipc.ErrNotFound, req.Folder)
	}
	sv, err := btrfs.CreateSubvolumeIn(ctx, req.UUID, dir, req.Name)
	switch {
	case errors.Is(err, btrfs.ErrNotOnFilesystem), errors.Is(err, btrfs.ErrInvalidSubvolume):
		return sv, fmt.Errorf("%w: folder %s: %w", bridgeipc.ErrInvalidArgs, req.Folder, err)
	case errors.Is(err, unix.EEXIST):
		return sv, fmt.Errorf("%w: %s in folder %s: %w", bridgeipc.ErrConflict, req.Name, req.Folder, err)
	case errors.Is(err, unix.EROFS):
		return sv, fmt.Errorf("%w: folder %s is read-only: %w", bridgeipc.ErrConflict, req.Folder, err)
	case err != nil:
		return sv, fmt.Errorf("create btrfs subvolume %s in %s: %w", req.Name, req.Folder, err)
	}
	slog.Info("btrfs subvolume created", "uuid", req.UUID, "path", sv.Path, "folder", req.Folder)
	return sv, nil
}

// DeleteBtrfsSubvolume deletes a subvolume.
func DeleteBtrfsSubvolume(ctx context.Context, req apischema.BtrfsSubvolumeRequest) error {
	if err := btrfs.DeleteSubvolume(ctx, req.UUID, req.Path); err != nil {
		return fmt.Errorf("delete btrfs subvolume %s in %s: %w", req.Path, req.UUID, pathStatus(err))
	}
	slog.Info("btrfs subvolume deleted", "uuid", req.UUID, "path", req.Path)
	return nil
}

// CreateBtrfsSnapshot takes a manual snapshot of a subvolume.
func CreateBtrfsSnapshot(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (btrfs.BtrfsSnapshot, error) {
	snap, err := btrfs.CreateSnapshot(ctx, req.UUID, req.Path, btrfs.KindManual, time.Now())
	if err != nil {
		return snap, fmt.Errorf("create btrfs snapshot of %s in %s: %w", req.Path, req.UUID, pathStatus(err))
	}
	slog.Info("btrfs snapshot created", "uuid", req.UUID, "source", req.Path)
	return snap, nil
}

// DeleteBtrfsSnapshot deletes a snapshot.
func DeleteBtrfsSnapshot(ctx context.Context, req apischema.BtrfsSnapshotRequest) error {
	if err := btrfs.DeleteSnapshot(ctx, req.UUID, req.SnapshotPath); err != nil {
		return fmt.Errorf("delete btrfs snapshot %s in %s: %w", req.SnapshotPath, req.UUID, pathStatus(err))
	}
	slog.Info("btrfs snapshot deleted", "uuid", req.UUID, "path", req.SnapshotPath)
	return nil
}

// SetBtrfsSubvolumeReadOnly toggles the read-only property of a subvolume.
func SetBtrfsSubvolumeReadOnly(ctx context.Context, req apischema.BtrfsSubvolumeReadOnlyRequest) (btrfs.BtrfsSubvolume, error) {
	sv, err := btrfs.SetSubvolumeReadOnly(ctx, req.UUID, req.Path, req.ReadOnly)
	if err != nil {
		return sv, fmt.Errorf("set btrfs subvolume %s read-only=%t in %s: %w", req.Path, req.ReadOnly, req.UUID, pathStatus(err))
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
