package storage

import (
	"context"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/go-logger/logger"
)

// RegisterHandlers registers all storage handlers with the global registry
func RegisterHandlers() {
	// LVM Read Operations
	ipc.RegisterFunc("storage", "list_pvs", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Debugf("Listing physical volumes")
		pvs, err := ListPhysicalVolumes()
		if err != nil {
			logger.Errorf("Failed to list PVs: %v", err)
			return err
		}
		logger.Debugf("Found %d physical volumes", len(pvs))
		return emit.Result(pvs)
	})

	ipc.RegisterFunc("storage", "list_vgs", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Debugf("Listing volume groups")
		vgs, err := ListVolumeGroups()
		if err != nil {
			logger.Errorf("Failed to list VGs: %v", err)
			return err
		}
		logger.Debugf("Found %d volume groups", len(vgs))
		return emit.Result(vgs)
	})

	ipc.RegisterFunc("storage", "list_lvs", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Debugf("Listing logical volumes")
		lvs, err := ListLogicalVolumes()
		if err != nil {
			logger.Errorf("Failed to list LVs: %v", err)
			return err
		}
		logger.Debugf("Found %d logical volumes", len(lvs))
		return emit.Result(lvs)
	})

	// LVM Write Operations
	ipc.RegisterFunc("storage", "create_lv", func(ctx context.Context, args []string, emit ipc.Events) error {
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
	})

	ipc.RegisterFunc("storage", "delete_lv", func(ctx context.Context, args []string, emit ipc.Events) error {
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
	})

	ipc.RegisterFunc("storage", "resize_lv", func(ctx context.Context, args []string, emit ipc.Events) error {
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
	})

	// NFS Operations
	ipc.RegisterFunc("storage", "list_nfs_mounts", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Debugf("Listing NFS mounts")
		mounts, err := ListNFSMounts()
		if err != nil {
			logger.Errorf("Failed to list NFS mounts: %v", err)
			return err
		}
		logger.Debugf("Found %d NFS mounts", len(mounts))
		return emit.Result(mounts)
	})

	ipc.RegisterFunc("storage", "list_nfs_exports", func(ctx context.Context, args []string, emit ipc.Events) error {
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
	})

	ipc.RegisterFunc("storage", "mount_nfs", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 4 {
			logger.Errorf("mount_nfs: insufficient arguments (need server, exportPath, mountpoint, options)")
			return ipc.ErrInvalidArgs
		}
		// args: [server, exportPath, mountpoint, options, persist]
		persist := len(args) > 4 && (args[4] == "true" || args[4] == "1")
		options := ""
		if len(args) > 3 {
			options = args[3]
		}
		logger.Infof("Mounting NFS share: %s:%s -> %s (persist=%v)", args[0], args[1], args[2], persist)
		result, err := MountNFS(args[0], args[1], args[2], options, persist)
		if err != nil {
			logger.Errorf("Failed to mount NFS %s:%s: %v", args[0], args[1], err)
			return err
		}
		logger.Infof("Successfully mounted NFS share %s:%s at %s", args[0], args[1], args[2])
		return emit.Result(result)
	})

	ipc.RegisterFunc("storage", "unmount_nfs", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			logger.Errorf("unmount_nfs: missing mountpoint argument")
			return ipc.ErrInvalidArgs
		}
		// args: [mountpoint, removeFstab]
		removeFstab := len(args) > 1 && (args[1] == "true" || args[1] == "1")
		logger.Infof("Unmounting NFS: %s (removeFstab=%v)", args[0], removeFstab)
		result, err := UnmountNFS(args[0], removeFstab)
		if err != nil {
			logger.Errorf("Failed to unmount NFS %s: %v", args[0], err)
			return err
		}
		logger.Infof("Successfully unmounted NFS from %s", args[0])
		return emit.Result(result)
	})

	ipc.RegisterFunc("storage", "remount_nfs", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 2 {
			logger.Errorf("remount_nfs: insufficient arguments (need mountpoint, options)")
			return ipc.ErrInvalidArgs
		}
		// args: [mountpoint, options, updateFstab]
		updateFstab := len(args) > 2 && (args[2] == "true" || args[2] == "1")
		logger.Infof("Remounting NFS: %s with options=%s (updateFstab=%v)", args[0], args[1], updateFstab)
		result, err := RemountNFS(args[0], args[1], updateFstab)
		if err != nil {
			logger.Errorf("Failed to remount NFS %s: %v", args[0], err)
			return err
		}
		logger.Infof("Successfully remounted NFS at %s", args[0])
		return emit.Result(result)
	})

	ipc.RegisterFunc("storage", "unmount_filesystem", func(ctx context.Context, args []string, emit ipc.Events) error {
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
	})

	ipc.RegisterFunc("storage", "create_btrfs_subvolume", func(ctx context.Context, args []string, emit ipc.Events) error {
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
	})

	ipc.RegisterFunc("storage", "get_drive_info", func(ctx context.Context, args []string, emit ipc.Events) error {
		driveInfo, err := FetchDriveInfo()
		if err != nil {
			return err
		}
		return emit.Result(driveInfo)
	})

	ipc.RegisterFunc("storage", "run_smart_test", func(ctx context.Context, args []string, emit ipc.Events) error {
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
	})
}
