package storage

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers all storage handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterJobRoutes(router)

	bridgeipc.RegisterRoutes(router, "storage", []bridgeipc.Command{
		{Name: "list_pvs", Mode: bridgeipc.ModeQuery, Handler: handleListPVs},
		{Name: "list_vgs", Mode: bridgeipc.ModeQuery, Handler: handleListVGs},
		{Name: "list_lvs", Mode: bridgeipc.ModeQuery, Handler: handleListLVs},
		{Name: "create_lv", Mode: bridgeipc.ModeJob, Handler: handleCreateLV},
		{Name: "delete_lv", Mode: bridgeipc.ModeJob, Handler: handleDeleteLV},
		{Name: "resize_lv", Mode: bridgeipc.ModeJob, Handler: handleResizeLV},
		{Name: "list_nfs_mounts", Mode: bridgeipc.ModeQuery, Handler: handleListNFSMounts},
		{Name: "list_nfs_exports", Mode: bridgeipc.ModeQuery, Handler: handleListNFSExports},
		{Name: "mount_nfs", Mode: bridgeipc.ModeJob, Handler: handleMountNFS},
		{Name: "unmount_nfs", Mode: bridgeipc.ModeJob, Handler: handleUnmountNFS},
		{Name: "remount_nfs", Mode: bridgeipc.ModeJob, Handler: handleRemountNFS},
		{Name: "unmount_filesystem", Mode: bridgeipc.ModeJob, Handler: handleUnmountFilesystem},
		{Name: "create_btrfs_subvolume", Mode: bridgeipc.ModeJob, Handler: handleCreateBtrfsSubvolume},
		{Name: "get_drive_info", Mode: bridgeipc.ModeQuery, Handler: handleGetDriveInfo},
	})
}

func handleListPVs(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Debug("Listing physical volumes")
	pvs, err := ListPhysicalVolumes(ctx)
	if err != nil {
		slog.Error("failed to list physical volumes", "error", err)
		return err
	}
	slog.Debug("listed physical volumes", "count", len(pvs))
	return bridgeipc.EmitResult(emit, pvs, nil)
}

func handleListVGs(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Debug("Listing volume groups")
	vgs, err := ListVolumeGroups(ctx)
	if err != nil {
		slog.Error("failed to list volume groups", "error", err)
		return err
	}
	slog.Debug("listed volume groups", "count", len(vgs))
	return bridgeipc.EmitResult(emit, vgs, nil)
}

func handleListLVs(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Debug("Listing logical volumes")
	lvs, err := ListLogicalVolumes(ctx)
	if err != nil {
		slog.Error("failed to list logical volumes", "error", err)
		return err
	}
	slog.Debug("listed logical volumes", "count", len(lvs))
	return bridgeipc.EmitResult(emit, lvs, nil)
}

func handleCreateLV(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 3); err != nil {
		slog.Error("create_lv: insufficient arguments (need vgName, lvName, size)")
		return err
	}
	slog.Info("creating logical volume", "volume_group", args[0], "name", args[1], "size", args[2])
	result, err := CreateLogicalVolume(ctx, args[0], args[1], args[2])
	if err != nil {
		slog.Error("failed to create logical volume", "volume_group", args[0], "name", args[1], "error", err)
		return err
	}
	slog.Info("logical volume created", "volume_group", args[0], "name", args[1])
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleDeleteLV(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 2); err != nil {
		slog.Error("delete_lv: insufficient arguments (need vgName, lvName)")
		return err
	}
	slog.Info("deleting logical volume", "volume_group", args[0], "name", args[1])
	result, err := DeleteLogicalVolume(ctx, args[0], args[1])
	if err != nil {
		slog.Error("failed to delete logical volume", "volume_group", args[0], "name", args[1], "error", err)
		return err
	}
	slog.Info("logical volume deleted", "volume_group", args[0], "name", args[1])
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleResizeLV(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 3); err != nil {
		slog.Error("resize_lv: insufficient arguments (need vgName, lvName, newSize)")
		return err
	}
	slog.Info("resizing logical volume", "volume_group", args[0], "name", args[1], "size", args[2])
	result, err := ResizeLogicalVolume(ctx, args[0], args[1], args[2])
	if err != nil {
		slog.Error("failed to resize logical volume", "volume_group", args[0], "name", args[1], "error", err)
		return err
	}
	slog.Info("logical volume resized", "volume_group", args[0], "name", args[1], "size", args[2])
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleListNFSMounts(ctx context.Context, args []string, emit bridgeipc.Events) error {
	slog.Debug("Listing NFS mounts")
	mounts, err := ListNFSMounts()
	if err != nil {
		slog.Error("failed to list NFS mounts", "error", err)
		return err
	}
	slog.Debug("listed NFS mounts", "count", len(mounts))
	return bridgeipc.EmitResult(emit, mounts, nil)
}

func handleListNFSExports(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 1); err != nil {
		slog.Error("list_nfs_exports: missing server argument")
		return err
	}
	slog.Debug("listing NFS exports", "server", args[0])
	exports, err := ListNFSExports(ctx, args[0])
	if err != nil {
		slog.Error("failed to list NFS exports", "server", args[0], "error", err)
		return err
	}
	slog.Debug("listed NFS exports", "server", args[0], "count", len(exports))
	return bridgeipc.EmitResult(emit, exports, nil)
}

func handleMountNFS(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 4); err != nil {
		slog.Error("mount_nfs: insufficient arguments (need server, exportPath, mountpoint, options)")
		return err
	}
	persist := len(args) > 4 && (args[4] == "true" || args[4] == "1")
	options := args[3]
	slog.Debug("mount_nfs request",
		"server", args[0],
		"path", args[1],
		"mountpoint", args[2],
		"options", options,
		"persistent", persist)
	result, err := MountNFS(ctx, args[0], args[1], args[2], options, persist)
	if err != nil {
		slog.Error("failed to mount NFS share",
			"server", args[0],
			"path", args[1],
			"mountpoint", args[2],
			"error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleUnmountNFS(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 1); err != nil {
		slog.Error("unmount_nfs: missing mountpoint argument")
		return err
	}
	removeFstab := len(args) > 1 && (args[1] == "true" || args[1] == "1")
	slog.Debug("unmount_nfs request", "mountpoint", args[0], "remove_fstab", removeFstab)
	result, err := UnmountNFS(ctx, args[0], removeFstab)
	if err != nil {
		slog.Error("failed to unmount NFS share", "mountpoint", args[0], "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleRemountNFS(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 2); err != nil {
		slog.Error("remount_nfs: insufficient arguments (need mountpoint, options)")
		return err
	}
	updateFstab := len(args) > 2 && (args[2] == "true" || args[2] == "1")
	slog.Debug("remount_nfs request", "mountpoint", args[0], "options", args[1], "update_fstab", updateFstab)
	result, err := RemountNFS(ctx, args[0], args[1], updateFstab)
	if err != nil {
		slog.Error("failed to remount NFS share", "mountpoint", args[0], "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleUnmountFilesystem(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 1); err != nil {
		slog.Error("unmount_filesystem: missing mountpoint argument")
		return err
	}
	slog.Info("unmounting filesystem", "mountpoint", args[0])
	result, err := UnmountFilesystem(ctx, args[0])
	if err != nil {
		slog.Error("failed to unmount filesystem", "mountpoint", args[0], "error", err)
		return err
	}
	slog.Info("filesystem unmounted", "mountpoint", args[0])
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleCreateBtrfsSubvolume(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 2); err != nil {
		slog.Error("create_btrfs_subvolume: insufficient arguments (need mountpoint, name)")
		return err
	}
	slog.Info("creating btrfs subvolume", "mountpoint", args[0], "name", args[1])
	result, err := CreateBtrfsSubvolume(ctx, args[0], args[1])
	if err != nil {
		slog.Error("failed to create btrfs subvolume", "mountpoint", args[0], "name", args[1], "error", err)
		return err
	}
	slog.Info("btrfs subvolume created", "mountpoint", args[0], "name", args[1])
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleGetDriveInfo(ctx context.Context, args []string, emit bridgeipc.Events) error {
	driveInfo, err := FetchDriveInfo(ctx)
	if err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, driveInfo, nil)
}
