package storage

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Query[apischema.NoRequest, []apischema.PhysicalVolume]("storage.list_pvs").Handle(handleListPVs),
	apischema.Query[apischema.NoRequest, []apischema.VolumeGroup]("storage.list_vgs").Handle(handleListVGs),
	apischema.Query[apischema.NoRequest, []apischema.LogicalVolume]("storage.list_lvs").Handle(handleListLVs),
	apischema.Job[apischema.CreateLogicalVolumeRequest, apischema.StorageCreateLVResult]("storage.create_lv").Handle(handleCreateLV),
	apischema.Job[apischema.VolumeGroupLogicalVolumeRequest, apischema.SuccessResponse]("storage.delete_lv").Handle(handleDeleteLV),
	apischema.Job[apischema.ResizeLogicalVolumeRequest, apischema.SuccessResponse]("storage.resize_lv").Handle(handleResizeLV),
	apischema.Query[apischema.NoRequest, []apischema.NFSMount]("storage.list_nfs_mounts").Handle(handleListNFSMounts),
	apischema.Query[apischema.ServerRequest, []string]("storage.list_nfs_exports").Handle(handleListNFSExports),
	apischema.Job[apischema.ServerExportMountOptionsPersistRequest, apischema.StorageMountResult]("storage.mount_nfs").Handle(handleMountNFS),
	apischema.Job[apischema.MountpointRemoveFstabRequest, apischema.StorageWarningResult]("storage.unmount_nfs").Handle(handleUnmountNFS),
	apischema.Job[apischema.MountpointOptionsUpdateFstabRequest, apischema.StorageMountResult]("storage.remount_nfs").Handle(handleRemountNFS),
	apischema.Query[apischema.NoRequest, []apischema.CIFSMount]("storage.list_cifs_mounts").Handle(handleListCIFSMounts),
	apischema.Query[apischema.ServerRequest, []string]("storage.list_cifs_shares").Handle(handleListCIFSShares),
	apischema.Job[apischema.CIFSMountRequest, apischema.StorageMountResult]("storage.mount_cifs").Handle(handleMountCIFS),
	apischema.Job[apischema.MountpointRemoveFstabRequest, apischema.StorageWarningResult]("storage.unmount_cifs").Handle(handleUnmountCIFS),
	apischema.Job[apischema.MountpointOptionsUpdateFstabRequest, apischema.StorageMountResult]("storage.remount_cifs").Handle(handleRemountCIFS),
	apischema.Job[apischema.MountpointRequest, apischema.StorageMountResult]("storage.unmount_filesystem").Handle(handleUnmountFilesystem),
	apischema.Job[apischema.MountpointNameRequest, apischema.StoragePathResult]("storage.create_btrfs_subvolume").Handle(handleCreateBtrfsSubvolume),
	apischema.Query[apischema.NoRequest, []apischema.ApiDisk]("storage.get_drive_info").Handle(handleGetDriveInfo),
)

var Routes = apischema.CombineRoutes(api.Routes(), smartTestRoutes)

// RegisterHandlers registers all storage handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterJobRoutes(router)

	api.Register(router)
}

func handleListPVs(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	slog.Debug("Listing physical volumes")
	pvs, err := ListPhysicalVolumes(ctx)
	if err != nil {
		slog.Error("failed to list physical volumes", "error", err)
		return err
	}
	slog.Debug("listed physical volumes", "count", len(pvs))
	return bridgeipc.EmitResult(emit, pvs, nil)
}

func handleListVGs(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	slog.Debug("Listing volume groups")
	vgs, err := ListVolumeGroups(ctx)
	if err != nil {
		slog.Error("failed to list volume groups", "error", err)
		return err
	}
	slog.Debug("listed volume groups", "count", len(vgs))
	return bridgeipc.EmitResult(emit, vgs, nil)
}

func handleListLVs(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	slog.Debug("Listing logical volumes")
	lvs, err := ListLogicalVolumes(ctx)
	if err != nil {
		slog.Error("failed to list logical volumes", "error", err)
		return err
	}
	slog.Debug("listed logical volumes", "count", len(lvs))
	return bridgeipc.EmitResult(emit, lvs, nil)
}

func handleCreateLV(ctx context.Context, req apischema.CreateLogicalVolumeRequest, emit bridgeipc.Events) error {
	slog.Info("creating logical volume", "volume_group", req.VGName, "name", req.LVName, "size", req.Size)
	result, err := CreateLogicalVolume(ctx, req.VGName, req.LVName, req.Size)
	if err != nil {
		slog.Error("failed to create logical volume", "volume_group", req.VGName, "name", req.LVName, "error", err)
		return err
	}
	slog.Info("logical volume created", "volume_group", req.VGName, "name", req.LVName)
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleDeleteLV(ctx context.Context, req apischema.VolumeGroupLogicalVolumeRequest, emit bridgeipc.Events) error {
	slog.Info("deleting logical volume", "volume_group", req.VGName, "name", req.LVName)
	result, err := DeleteLogicalVolume(ctx, req.VGName, req.LVName)
	if err != nil {
		slog.Error("failed to delete logical volume", "volume_group", req.VGName, "name", req.LVName, "error", err)
		return err
	}
	slog.Info("logical volume deleted", "volume_group", req.VGName, "name", req.LVName)
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleResizeLV(ctx context.Context, req apischema.ResizeLogicalVolumeRequest, emit bridgeipc.Events) error {
	slog.Info("resizing logical volume", "volume_group", req.VGName, "name", req.LVName, "size", req.NewSize)
	result, err := ResizeLogicalVolume(ctx, req.VGName, req.LVName, req.NewSize)
	if err != nil {
		slog.Error("failed to resize logical volume", "volume_group", req.VGName, "name", req.LVName, "error", err)
		return err
	}
	slog.Info("logical volume resized", "volume_group", req.VGName, "name", req.LVName, "size", req.NewSize)
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleListNFSMounts(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	slog.Debug("Listing NFS mounts")
	mounts, err := ListNFSMounts(ctx)
	if err != nil {
		slog.Error("failed to list NFS mounts", "error", err)
		return err
	}
	slog.Debug("listed NFS mounts", "count", len(mounts))
	return bridgeipc.EmitResult(emit, mounts, nil)
}

func handleListNFSExports(ctx context.Context, req apischema.ServerRequest, emit bridgeipc.Events) error {
	slog.Debug("listing NFS exports", "server", req.Server)
	exports, err := ListNFSExports(ctx, req.Server)
	if err != nil {
		slog.Error("failed to list NFS exports", "server", req.Server, "error", err)
		return err
	}
	slog.Debug("listed NFS exports", "server", req.Server, "count", len(exports))
	return bridgeipc.EmitResult(emit, exports, nil)
}

func handleMountNFS(ctx context.Context, req apischema.ServerExportMountOptionsPersistRequest, emit bridgeipc.Events) error {
	persist := truthy(req.Persist)
	slog.Debug("mount_nfs request",
		"server", req.Server,
		"path", req.ExportPath,
		"mountpoint", req.Mountpoint,
		"options", req.Options,
		"persistent", persist)
	result, err := MountNFS(ctx, req.Server, req.ExportPath, req.Mountpoint, req.Options, persist)
	if err != nil {
		slog.Error("failed to mount NFS share",
			"server", req.Server,
			"path", req.ExportPath,
			"mountpoint", req.Mountpoint,
			"error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleUnmountNFS(ctx context.Context, req apischema.MountpointRemoveFstabRequest, emit bridgeipc.Events) error {
	removeFstab := truthy(req.RemoveFstab)
	slog.Debug("unmount_nfs request", "mountpoint", req.Mountpoint, "remove_fstab", removeFstab)
	result, err := UnmountNFS(ctx, req.Mountpoint, removeFstab)
	if err != nil {
		slog.Error("failed to unmount NFS share", "mountpoint", req.Mountpoint, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleRemountNFS(ctx context.Context, req apischema.MountpointOptionsUpdateFstabRequest, emit bridgeipc.Events) error {
	updateFstab := truthy(req.UpdateFstab)
	slog.Debug("remount_nfs request", "mountpoint", req.Mountpoint, "options", req.Options, "update_fstab", updateFstab)
	result, err := RemountNFS(ctx, req.Mountpoint, req.Options, updateFstab)
	if err != nil {
		slog.Error("failed to remount NFS share", "mountpoint", req.Mountpoint, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleListCIFSMounts(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	slog.Debug("Listing CIFS mounts")
	mounts, err := ListCIFSMounts(ctx)
	if err != nil {
		slog.Error("failed to list CIFS mounts", "error", err)
		return err
	}
	slog.Debug("listed CIFS mounts", "count", len(mounts))
	return bridgeipc.EmitResult(emit, mounts, nil)
}

func handleListCIFSShares(ctx context.Context, req apischema.ServerRequest, emit bridgeipc.Events) error {
	slog.Debug("listing CIFS shares", "server", req.Server)
	shares, err := ListCIFSShares(ctx, req.Server)
	if err != nil {
		slog.Error("failed to list CIFS shares", "server", req.Server, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, shares, nil)
}

func handleMountCIFS(ctx context.Context, req apischema.CIFSMountRequest, emit bridgeipc.Events) error {
	// Password safety: never log req.Password or req.Options.
	slog.Debug("mount_cifs request", "server", req.Server, "share", req.Share, "mountpoint", req.Mountpoint)
	result, err := MountCIFS(ctx, cifsMountParams{
		server:     req.Server,
		share:      req.Share,
		mountpoint: req.Mountpoint,
		username:   req.Username,
		password:   req.Password,
		domain:     req.Domain,
		options:    req.Options,
	})
	if err != nil {
		slog.Error("failed to mount CIFS share", "server", req.Server, "share", req.Share, "mountpoint", req.Mountpoint, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleUnmountCIFS(ctx context.Context, req apischema.MountpointRemoveFstabRequest, emit bridgeipc.Events) error {
	removeFstab := truthy(req.RemoveFstab)
	slog.Debug("unmount_cifs request", "mountpoint", req.Mountpoint, "remove_fstab", removeFstab)
	result, err := UnmountCIFS(ctx, req.Mountpoint, removeFstab)
	if err != nil {
		slog.Error("failed to unmount CIFS share", "mountpoint", req.Mountpoint, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleRemountCIFS(ctx context.Context, req apischema.MountpointOptionsUpdateFstabRequest, emit bridgeipc.Events) error {
	updateFstab := truthy(req.UpdateFstab)
	// Password safety: never log req.Options.
	slog.Debug("remount_cifs request", "mountpoint", req.Mountpoint, "update_fstab", updateFstab)
	result, err := RemountCIFS(ctx, req.Mountpoint, req.Options, updateFstab)
	if err != nil {
		slog.Error("failed to remount CIFS share", "mountpoint", req.Mountpoint, "error", err)
		return err
	}
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleUnmountFilesystem(ctx context.Context, req apischema.MountpointRequest, emit bridgeipc.Events) error {
	slog.Info("unmounting filesystem", "mountpoint", req.Mountpoint)
	result, err := UnmountFilesystem(ctx, req.Mountpoint)
	if err != nil {
		slog.Error("failed to unmount filesystem", "mountpoint", req.Mountpoint, "error", err)
		return err
	}
	slog.Info("filesystem unmounted", "mountpoint", req.Mountpoint)
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleCreateBtrfsSubvolume(ctx context.Context, req apischema.MountpointNameRequest, emit bridgeipc.Events) error {
	slog.Info("creating btrfs subvolume", "mountpoint", req.Mountpoint, "name", req.Name)
	result, err := CreateBtrfsSubvolume(ctx, req.Mountpoint, req.Name)
	if err != nil {
		slog.Error("failed to create btrfs subvolume", "mountpoint", req.Mountpoint, "name", req.Name, "error", err)
		return err
	}
	slog.Info("btrfs subvolume created", "mountpoint", req.Mountpoint, "name", req.Name)
	return bridgeipc.EmitResult(emit, result, nil)
}

func handleGetDriveInfo(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	driveInfo, err := FetchDriveInfo(ctx)
	if err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, driveInfo, nil)
}
