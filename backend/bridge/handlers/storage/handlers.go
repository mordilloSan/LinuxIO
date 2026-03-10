package storage

import (
	"context"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/go-logger/logger"
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
	logger.Debugf("Listing physical volumes")
	pvs, err := ListPhysicalVolumes()
	if err != nil {
		logger.Errorf("Failed to list PVs: %v", err)
		return err
	}
	logger.Debugf("Found %d physical volumes", len(pvs))
	return emit.Result(pvs)
}

func handleListVGs(ctx context.Context, args []string, emit ipc.Events) error {
	logger.Debugf("Listing volume groups")
	vgs, err := ListVolumeGroups()
	if err != nil {
		logger.Errorf("Failed to list VGs: %v", err)
		return err
	}
	logger.Debugf("Found %d volume groups", len(vgs))
	return emit.Result(vgs)
}

func handleListLVs(ctx context.Context, args []string, emit ipc.Events) error {
	logger.Debugf("Listing logical volumes")
	lvs, err := ListLogicalVolumes()
	if err != nil {
		logger.Errorf("Failed to list LVs: %v", err)
		return err
	}
	logger.Debugf("Found %d logical volumes", len(lvs))
	return emit.Result(lvs)
}

func handleCreateLV(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 3 {
		logger.Errorf("create_lv: insufficient arguments (need vgName, lvName, size)")
		return ipc.ErrInvalidArgs
	}
	logger.Infof("Creating logical volume: vg=%s, lv=%s, size=%s", args[0], args[1], args[2])
	result, err := CreateLogicalVolume(args[0], args[1], args[2])
	if err != nil {
		logger.Errorf("Failed to create LV %s/%s: %v", args[0], args[1], err)
		return err
	}
	logger.Infof("Successfully created logical volume %s/%s", args[0], args[1])
	return emit.Result(result)
}

func handleDeleteLV(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 2 {
		logger.Errorf("delete_lv: insufficient arguments (need vgName, lvName)")
		return ipc.ErrInvalidArgs
	}
	logger.Infof("Deleting logical volume: vg=%s, lv=%s", args[0], args[1])
	result, err := DeleteLogicalVolume(args[0], args[1])
	if err != nil {
		logger.Errorf("Failed to delete LV %s/%s: %v", args[0], args[1], err)
		return err
	}
	logger.Infof("Successfully deleted logical volume %s/%s", args[0], args[1])
	return emit.Result(result)
}

func handleResizeLV(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 3 {
		logger.Errorf("resize_lv: insufficient arguments (need vgName, lvName, newSize)")
		return ipc.ErrInvalidArgs
	}
	logger.Infof("Resizing logical volume: vg=%s, lv=%s, newSize=%s", args[0], args[1], args[2])
	result, err := ResizeLogicalVolume(args[0], args[1], args[2])
	if err != nil {
		logger.Errorf("Failed to resize LV %s/%s: %v", args[0], args[1], err)
		return err
	}
	logger.Infof("Successfully resized logical volume %s/%s to %s", args[0], args[1], args[2])
	return emit.Result(result)
}

func handleListNFSMounts(ctx context.Context, args []string, emit ipc.Events) error {
	logger.Debugf("Listing NFS mounts")
	mounts, err := ListNFSMounts()
	if err != nil {
		logger.Errorf("Failed to list NFS mounts: %v", err)
		return err
	}
	logger.Debugf("Found %d NFS mounts", len(mounts))
	return emit.Result(mounts)
}

func handleListNFSExports(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		logger.Errorf("list_nfs_exports: missing server argument")
		return ipc.ErrInvalidArgs
	}
	logger.Debugf("Listing NFS exports from server: %s", args[0])
	exports, err := ListNFSExports(args[0])
	if err != nil {
		logger.Errorf("Failed to list NFS exports from %s: %v", args[0], err)
		return err
	}
	logger.Debugf("Found %d NFS exports from %s", len(exports), args[0])
	return emit.Result(exports)
}

func handleMountNFS(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 4 {
		logger.Errorf("mount_nfs: insufficient arguments (need server, exportPath, mountpoint, options)")
		return ipc.ErrInvalidArgs
	}
	persist := len(args) > 4 && (args[4] == "true" || args[4] == "1")
	options := args[3]
	logger.Infof("Mounting NFS share: %s:%s -> %s (persist=%v)", args[0], args[1], args[2], persist)
	result, err := MountNFS(args[0], args[1], args[2], options, persist)
	if err != nil {
		logger.Errorf("Failed to mount NFS %s:%s: %v", args[0], args[1], err)
		return err
	}
	logger.Infof("Successfully mounted NFS share %s:%s at %s", args[0], args[1], args[2])
	return emit.Result(result)
}

func handleUnmountNFS(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		logger.Errorf("unmount_nfs: missing mountpoint argument")
		return ipc.ErrInvalidArgs
	}
	removeFstab := len(args) > 1 && (args[1] == "true" || args[1] == "1")
	logger.Infof("Unmounting NFS: %s (removeFstab=%v)", args[0], removeFstab)
	result, err := UnmountNFS(args[0], removeFstab)
	if err != nil {
		logger.Errorf("Failed to unmount NFS %s: %v", args[0], err)
		return err
	}
	logger.Infof("Successfully unmounted NFS from %s", args[0])
	return emit.Result(result)
}

func handleRemountNFS(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 2 {
		logger.Errorf("remount_nfs: insufficient arguments (need mountpoint, options)")
		return ipc.ErrInvalidArgs
	}
	updateFstab := len(args) > 2 && (args[2] == "true" || args[2] == "1")
	logger.Infof("Remounting NFS: %s with options=%s (updateFstab=%v)", args[0], args[1], updateFstab)
	result, err := RemountNFS(args[0], args[1], updateFstab)
	if err != nil {
		logger.Errorf("Failed to remount NFS %s: %v", args[0], err)
		return err
	}
	logger.Infof("Successfully remounted NFS at %s", args[0])
	return emit.Result(result)
}

func handleUnmountFilesystem(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 1 {
		logger.Errorf("unmount_filesystem: missing mountpoint argument")
		return ipc.ErrInvalidArgs
	}
	logger.Infof("Unmounting filesystem at %s", args[0])
	result, err := UnmountFilesystem(args[0])
	if err != nil {
		logger.Errorf("Failed to unmount filesystem %s: %v", args[0], err)
		return err
	}
	logger.Infof("Successfully unmounted filesystem at %s", args[0])
	return emit.Result(result)
}

func handleCreateBtrfsSubvolume(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) < 2 {
		logger.Errorf("create_btrfs_subvolume: insufficient arguments (need mountpoint, name)")
		return ipc.ErrInvalidArgs
	}
	logger.Infof("Creating btrfs subvolume %s under %s", args[1], args[0])
	result, err := CreateBtrfsSubvolume(args[0], args[1])
	if err != nil {
		logger.Errorf("Failed to create btrfs subvolume %s under %s: %v", args[1], args[0], err)
		return err
	}
	logger.Infof("Successfully created btrfs subvolume %s under %s", args[1], args[0])
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
	logger.Infof("SMART test initiated: device=%s type=%s", device, testType)
	return emit.Result(result)
}
