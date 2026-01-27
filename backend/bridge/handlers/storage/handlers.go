package storage

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/go-logger/logger"
)

// RegisterHandlers registers all storage handlers with the global registry
func RegisterHandlers() {
	// LVM Read Operations
	ipc.RegisterFunc("storage", "list_pvs", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Debugf("[Storage] Listing physical volumes")
		pvs, err := ListPhysicalVolumes()
		if err != nil {
			logger.Errorf("[Storage] Failed to list PVs: %v", err)
			return err
		}
		logger.Debugf("[Storage] Found %d physical volumes", len(pvs))
		return emit.Result(pvs)
	})

	ipc.RegisterFunc("storage", "list_vgs", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Debugf("[Storage] Listing volume groups")
		vgs, err := ListVolumeGroups()
		if err != nil {
			logger.Errorf("[Storage] Failed to list VGs: %v", err)
			return err
		}
		logger.Debugf("[Storage] Found %d volume groups", len(vgs))
		return emit.Result(vgs)
	})

	ipc.RegisterFunc("storage", "list_lvs", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Debugf("[Storage] Listing logical volumes")
		lvs, err := ListLogicalVolumes()
		if err != nil {
			logger.Errorf("[Storage] Failed to list LVs: %v", err)
			return err
		}
		logger.Debugf("[Storage] Found %d logical volumes", len(lvs))
		return emit.Result(lvs)
	})

	// LVM Write Operations
	ipc.RegisterFunc("storage", "create_lv", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 3 {
			logger.Errorf("[Storage] create_lv: insufficient arguments (need vgName, lvName, size)")
			return ipc.ErrInvalidArgs
		}
		logger.Infof("[Storage] Creating logical volume: vg=%s, lv=%s, size=%s", args[0], args[1], args[2])
		result, err := CreateLogicalVolume(args[0], args[1], args[2])
		if err != nil {
			logger.Errorf("[Storage] Failed to create LV %s/%s: %v", args[0], args[1], err)
			return err
		}
		logger.Infof("[Storage] Successfully created logical volume %s/%s", args[0], args[1])
		return emit.Result(result)
	})

	ipc.RegisterFunc("storage", "delete_lv", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 2 {
			logger.Errorf("[Storage] delete_lv: insufficient arguments (need vgName, lvName)")
			return ipc.ErrInvalidArgs
		}
		logger.Infof("[Storage] Deleting logical volume: vg=%s, lv=%s", args[0], args[1])
		result, err := DeleteLogicalVolume(args[0], args[1])
		if err != nil {
			logger.Errorf("[Storage] Failed to delete LV %s/%s: %v", args[0], args[1], err)
			return err
		}
		logger.Infof("[Storage] Successfully deleted logical volume %s/%s", args[0], args[1])
		return emit.Result(result)
	})

	ipc.RegisterFunc("storage", "resize_lv", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 3 {
			logger.Errorf("[Storage] resize_lv: insufficient arguments (need vgName, lvName, newSize)")
			return ipc.ErrInvalidArgs
		}
		logger.Infof("[Storage] Resizing logical volume: vg=%s, lv=%s, newSize=%s", args[0], args[1], args[2])
		result, err := ResizeLogicalVolume(args[0], args[1], args[2])
		if err != nil {
			logger.Errorf("[Storage] Failed to resize LV %s/%s: %v", args[0], args[1], err)
			return err
		}
		logger.Infof("[Storage] Successfully resized logical volume %s/%s to %s", args[0], args[1], args[2])
		return emit.Result(result)
	})

	// NFS Operations
	ipc.RegisterFunc("storage", "list_nfs_mounts", func(ctx context.Context, args []string, emit ipc.Events) error {
		logger.Debugf("[Storage] Listing NFS mounts")
		mounts, err := ListNFSMounts()
		if err != nil {
			logger.Errorf("[Storage] Failed to list NFS mounts: %v", err)
			return err
		}
		logger.Debugf("[Storage] Found %d NFS mounts", len(mounts))
		return emit.Result(mounts)
	})

	ipc.RegisterFunc("storage", "list_nfs_exports", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			logger.Errorf("[Storage] list_nfs_exports: missing server argument")
			return ipc.ErrInvalidArgs
		}
		logger.Debugf("[Storage] Listing NFS exports from server: %s", args[0])
		exports, err := ListNFSExports(args[0])
		if err != nil {
			logger.Errorf("[Storage] Failed to list NFS exports from %s: %v", args[0], err)
			return err
		}
		logger.Debugf("[Storage] Found %d NFS exports from %s", len(exports), args[0])
		return emit.Result(exports)
	})

	ipc.RegisterFunc("storage", "mount_nfs", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 4 {
			logger.Errorf("[Storage] mount_nfs: insufficient arguments (need server, exportPath, mountpoint, options)")
			return ipc.ErrInvalidArgs
		}
		// args: [server, exportPath, mountpoint, options, persist]
		persist := len(args) > 4 && (args[4] == "true" || args[4] == "1")
		options := ""
		if len(args) > 3 {
			options = args[3]
		}
		logger.Infof("[Storage] Mounting NFS share: %s:%s -> %s (persist=%v)", args[0], args[1], args[2], persist)
		result, err := MountNFS(args[0], args[1], args[2], options, persist)
		if err != nil {
			logger.Errorf("[Storage] Failed to mount NFS %s:%s: %v", args[0], args[1], err)
			return err
		}
		logger.Infof("[Storage] Successfully mounted NFS share %s:%s at %s", args[0], args[1], args[2])
		return emit.Result(result)
	})

	ipc.RegisterFunc("storage", "unmount_nfs", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			logger.Errorf("[Storage] unmount_nfs: missing mountpoint argument")
			return ipc.ErrInvalidArgs
		}
		// args: [mountpoint, removeFstab]
		removeFstab := len(args) > 1 && (args[1] == "true" || args[1] == "1")
		logger.Infof("[Storage] Unmounting NFS: %s (removeFstab=%v)", args[0], removeFstab)
		result, err := UnmountNFS(args[0], removeFstab)
		if err != nil {
			logger.Errorf("[Storage] Failed to unmount NFS %s: %v", args[0], err)
			return err
		}
		logger.Infof("[Storage] Successfully unmounted NFS from %s", args[0])
		return emit.Result(result)
	})

	ipc.RegisterFunc("storage", "remount_nfs", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 2 {
			logger.Errorf("[Storage] remount_nfs: insufficient arguments (need mountpoint, options)")
			return ipc.ErrInvalidArgs
		}
		// args: [mountpoint, options, updateFstab]
		updateFstab := len(args) > 2 && (args[2] == "true" || args[2] == "1")
		logger.Infof("[Storage] Remounting NFS: %s with options=%s (updateFstab=%v)", args[0], args[1], updateFstab)
		result, err := RemountNFS(args[0], args[1], updateFstab)
		if err != nil {
			logger.Errorf("[Storage] Failed to remount NFS %s: %v", args[0], err)
			return err
		}
		logger.Infof("[Storage] Successfully remounted NFS at %s", args[0])
		return emit.Result(result)
	})
}
