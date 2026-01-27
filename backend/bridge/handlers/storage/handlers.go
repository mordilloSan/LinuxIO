package storage

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers all storage handlers with the global registry
func RegisterHandlers() {
	// LVM Read Operations
	ipc.RegisterFunc("storage", "list_pvs", func(ctx context.Context, args []string, emit ipc.Events) error {
		pvs, err := ListPhysicalVolumes()
		if err != nil {
			return err
		}
		return emit.Result(pvs)
	})

	ipc.RegisterFunc("storage", "list_vgs", func(ctx context.Context, args []string, emit ipc.Events) error {
		vgs, err := ListVolumeGroups()
		if err != nil {
			return err
		}
		return emit.Result(vgs)
	})

	ipc.RegisterFunc("storage", "list_lvs", func(ctx context.Context, args []string, emit ipc.Events) error {
		lvs, err := ListLogicalVolumes()
		if err != nil {
			return err
		}
		return emit.Result(lvs)
	})

	// LVM Write Operations
	ipc.RegisterFunc("storage", "create_lv", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 3 {
			return ipc.ErrInvalidArgs
		}
		result, err := CreateLogicalVolume(args[0], args[1], args[2])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("storage", "delete_lv", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 2 {
			return ipc.ErrInvalidArgs
		}
		result, err := DeleteLogicalVolume(args[0], args[1])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("storage", "resize_lv", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 3 {
			return ipc.ErrInvalidArgs
		}
		result, err := ResizeLogicalVolume(args[0], args[1], args[2])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	// NFS Operations
	ipc.RegisterFunc("storage", "list_nfs_mounts", func(ctx context.Context, args []string, emit ipc.Events) error {
		mounts, err := ListNFSMounts()
		if err != nil {
			return err
		}
		return emit.Result(mounts)
	})

	ipc.RegisterFunc("storage", "mount_nfs", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 4 {
			return ipc.ErrInvalidArgs
		}
		// args: [server, exportPath, mountpoint, options, persist]
		persist := len(args) > 4 && (args[4] == "true" || args[4] == "1")
		options := ""
		if len(args) > 3 {
			options = args[3]
		}
		result, err := MountNFS(args[0], args[1], args[2], options, persist)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("storage", "unmount_nfs", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		// args: [mountpoint, removeFstab]
		removeFstab := len(args) > 1 && (args[1] == "true" || args[1] == "1")
		result, err := UnmountNFS(args[0], removeFstab)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("storage", "remount_nfs", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 2 {
			return ipc.ErrInvalidArgs
		}
		// args: [mountpoint, options, updateFstab]
		updateFstab := len(args) > 2 && (args[2] == "true" || args[2] == "1")
		result, err := RemountNFS(args[0], args[1], updateFstab)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})
}
