package storage

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type storageRegistration struct {
	command string
	handler ipc.HandlerFunc
}

// RegisterHandlers registers all storage handlers with the global registry
func RegisterHandlers() {
	registerStorageHandlers([]storageRegistration{
		{command: "list_pvs", handler: handleListPVs},
		{command: "list_vgs", handler: handleListVGs},
		{command: "list_lvs", handler: handleListLVs},
		{command: "create_lv", handler: handleCreateLV},
		{command: "delete_lv", handler: handleDeleteLV},
		{command: "resize_lv", handler: handleResizeLV},
		{command: "list_nfs_mounts", handler: handleListNFSMounts},
		{command: "list_nfs_exports", handler: handleListNFSExports},
		{command: "mount_nfs", handler: handleMountNFS},
		{command: "unmount_nfs", handler: handleUnmountNFS},
		{command: "remount_nfs", handler: handleRemountNFS},
		{command: "unmount_filesystem", handler: handleUnmountFilesystem},
		{command: "create_btrfs_subvolume", handler: handleCreateBtrfsSubvolume},
		{command: "get_drive_info", handler: handleGetDriveInfo},
		{command: "run_smart_test", handler: handleRunSMARTTest},
	})
}

func registerStorageHandlers(registrations []storageRegistration) {
	for _, registration := range registrations {
		ipc.RegisterFunc("storage", registration.command, registration.handler)
	}
}

func handleListPVs(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Debug("Listing physical volumes")
	pvs, err := ListPhysicalVolumes()
	if err != nil {
		slog.Error("failed to list physical volumes", "error", err)
		return err
	}
	slog.Debug("listed physical volumes", "count", len(pvs))
	return emit.Result(pvs)
}

func handleListVGs(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Debug("Listing volume groups")
	vgs, err := ListVolumeGroups()
	if err != nil {
		slog.Error("failed to list volume groups", "error", err)
		return err
	}
	slog.Debug("listed volume groups", "count", len(vgs))
	return emit.Result(vgs)
}

func handleListLVs(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Debug("Listing logical volumes")
	lvs, err := ListLogicalVolumes()
	if err != nil {
		slog.Error("failed to list logical volumes", "error", err)
		return err
	}
	slog.Debug("listed logical volumes", "count", len(lvs))
	return emit.Result(lvs)
}

func handleCreateLV(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 3 {
		slog.Error("create_lv: insufficient arguments (need vgName, lvName, size)")
		return ipc.ErrInvalidArgs
	}
	slog.Info("creating logical volume", "volume_group", args[0], "name", args[1], "size", args[2])
	result, err := CreateLogicalVolume(args[0], args[1], args[2])
	if err != nil {
		slog.Error("failed to create logical volume", "volume_group", args[0], "name", args[1], "error", err)
		return err
	}
	slog.Info("logical volume created", "volume_group", args[0], "name", args[1])
	return emit.Result(result)
}

func handleDeleteLV(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 2 {
		slog.Error("delete_lv: insufficient arguments (need vgName, lvName)")
		return ipc.ErrInvalidArgs
	}
	slog.Info("deleting logical volume", "volume_group", args[0], "name", args[1])
	result, err := DeleteLogicalVolume(args[0], args[1])
	if err != nil {
		slog.Error("failed to delete logical volume", "volume_group", args[0], "name", args[1], "error", err)
		return err
	}
	slog.Info("logical volume deleted", "volume_group", args[0], "name", args[1])
	return emit.Result(result)
}

func handleResizeLV(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 3 {
		slog.Error("resize_lv: insufficient arguments (need vgName, lvName, newSize)")
		return ipc.ErrInvalidArgs
	}
	slog.Info("resizing logical volume", "volume_group", args[0], "name", args[1], "size", args[2])
	result, err := ResizeLogicalVolume(args[0], args[1], args[2])
	if err != nil {
		slog.Error("failed to resize logical volume", "volume_group", args[0], "name", args[1], "error", err)
		return err
	}
	slog.Info("logical volume resized", "volume_group", args[0], "name", args[1], "size", args[2])
	return emit.Result(result)
}

func handleListNFSMounts(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Debug("Listing NFS mounts")
	mounts, err := ListNFSMounts()
	if err != nil {
		slog.Error("failed to list NFS mounts", "error", err)
		return err
	}
	slog.Debug("listed NFS mounts", "count", len(mounts))
	return emit.Result(mounts)
}

func handleListNFSExports(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		slog.Error("list_nfs_exports: missing server argument")
		return ipc.ErrInvalidArgs
	}
	slog.Debug("listing NFS exports", "server", args[0])
	exports, err := ListNFSExports(ctx, args[0])
	if err != nil {
		slog.Error("failed to list NFS exports", "server", args[0], "error", err)
		return err
	}
	slog.Debug("listed NFS exports", "server", args[0], "count", len(exports))
	return emit.Result(exports)
}

func handleMountNFS(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 4 {
		slog.Error("mount_nfs: insufficient arguments (need server, exportPath, mountpoint, options)")
		return ipc.ErrInvalidArgs
	}
	persist := len(args) > 4 && (args[4] == "true" || args[4] == "1")
	options := args[3]
	slog.Info("mounting NFS share",
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
	slog.Info("NFS share mounted", "server", args[0], "path", args[1], "mountpoint", args[2])
	return emit.Result(result)
}

func handleUnmountNFS(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		slog.Error("unmount_nfs: missing mountpoint argument")
		return ipc.ErrInvalidArgs
	}
	removeFstab := len(args) > 1 && (args[1] == "true" || args[1] == "1")
	slog.Info("unmounting NFS share", "mountpoint", args[0], "remove_fstab", removeFstab)
	result, err := UnmountNFS(ctx, args[0], removeFstab)
	if err != nil {
		slog.Error("failed to unmount NFS share", "mountpoint", args[0], "error", err)
		return err
	}
	slog.Info("NFS share unmounted", "mountpoint", args[0])
	return emit.Result(result)
}

func handleRemountNFS(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 2 {
		slog.Error("remount_nfs: insufficient arguments (need mountpoint, options)")
		return ipc.ErrInvalidArgs
	}
	updateFstab := len(args) > 2 && (args[2] == "true" || args[2] == "1")
	slog.Info("remounting NFS share", "mountpoint", args[0], "options", args[1], "update_fstab", updateFstab)
	result, err := RemountNFS(ctx, args[0], args[1], updateFstab)
	if err != nil {
		slog.Error("failed to remount NFS share", "mountpoint", args[0], "error", err)
		return err
	}
	slog.Info("NFS share remounted", "mountpoint", args[0])
	return emit.Result(result)
}

func handleUnmountFilesystem(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		slog.Error("unmount_filesystem: missing mountpoint argument")
		return ipc.ErrInvalidArgs
	}
	slog.Info("unmounting filesystem", "mountpoint", args[0])
	result, err := UnmountFilesystem(ctx, args[0])
	if err != nil {
		slog.Error("failed to unmount filesystem", "mountpoint", args[0], "error", err)
		return err
	}
	slog.Info("filesystem unmounted", "mountpoint", args[0])
	return emit.Result(result)
}

func handleCreateBtrfsSubvolume(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 2 {
		slog.Error("create_btrfs_subvolume: insufficient arguments (need mountpoint, name)")
		return ipc.ErrInvalidArgs
	}
	slog.Info("creating btrfs subvolume", "mountpoint", args[0], "name", args[1])
	result, err := CreateBtrfsSubvolume(args[0], args[1])
	if err != nil {
		slog.Error("failed to create btrfs subvolume", "mountpoint", args[0], "name", args[1], "error", err)
		return err
	}
	slog.Info("btrfs subvolume created", "mountpoint", args[0], "name", args[1])
	return emit.Result(result)
}

func handleGetDriveInfo(ctx context.Context, args []string, emit ipc.Events) error {
	driveInfo, err := FetchDriveInfo()
	if err != nil {
		return err
	}
	return emit.Result(driveInfo)
}

func handleRunSMARTTest(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 2 {
		return fmt.Errorf("run_smart_test requires device name and test type (short/long)")
	}
	device := args[0]
	testType := args[1]
	result, err := RunSmartTest(device, testType)
	if err != nil {
		return err
	}
	slog.Info("SMART test initiated", "device", device, "type", testType)
	return emit.Result(result)
}
