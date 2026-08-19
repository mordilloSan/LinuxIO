package storage

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, []apischema.PhysicalVolume]("storage.list_pvs", apischema.RetrySafe()).Handle(handleListPVs),
	apischema.Call[apischema.NoRequest, []apischema.VolumeGroup]("storage.list_vgs", apischema.RetrySafe()).Handle(handleListVGs),
	apischema.Call[apischema.NoRequest, []apischema.LogicalVolume]("storage.list_lvs", apischema.RetrySafe()).Handle(handleListLVs),
	apischema.Call[apischema.CreateLogicalVolumeRequest, apischema.SuccessPathResponse]("storage.create_lv").Handle(handleCreateLV),
	apischema.Call[apischema.VolumeGroupLogicalVolumeRequest, apischema.SuccessResponse]("storage.delete_lv").Handle(handleDeleteLV),
	apischema.Call[apischema.ResizeLogicalVolumeRequest, apischema.SuccessResponse]("storage.resize_lv").Handle(handleResizeLV),
	apischema.Call[apischema.NoRequest, []apischema.NFSMount]("storage.list_nfs_mounts", apischema.RetrySafe()).Handle(handleListNFSMounts),
	apischema.Call[apischema.ServerRequest, []string]("storage.list_nfs_exports", apischema.RetrySafe()).Handle(handleListNFSExports),
	apischema.Call[apischema.ServerExportMountOptionsPersistRequest, apischema.StorageMountResult]("storage.mount_nfs").Handle(handleMountNFS),
	apischema.Call[apischema.MountpointRemoveFstabRequest, apischema.StorageWarningResult]("storage.unmount_nfs").Handle(handleUnmountNFS),
	apischema.Call[apischema.MountpointOptionsUpdateFstabRequest, apischema.StorageMountResult]("storage.remount_nfs").Handle(handleRemountNFS),
	apischema.Call[apischema.NoRequest, []apischema.CIFSMount]("storage.list_cifs_mounts", apischema.RetrySafe()).Handle(handleListCIFSMounts),
	apischema.Call[apischema.ServerRequest, []string]("storage.list_cifs_shares", apischema.RetrySafe()).Handle(handleListCIFSShares),
	apischema.Call[apischema.CIFSMountRequest, apischema.StorageMountResult]("storage.mount_cifs").Handle(handleMountCIFS),
	apischema.Call[apischema.MountpointRemoveFstabRequest, apischema.StorageWarningResult]("storage.unmount_cifs").Handle(handleUnmountCIFS),
	apischema.Call[apischema.MountpointOptionsUpdateFstabRequest, apischema.StorageMountResult]("storage.remount_cifs").Handle(handleRemountCIFS),
	apischema.Call[apischema.MountpointRequest, apischema.StorageMountResult]("storage.unmount_filesystem").Handle(handleUnmountFilesystem),
	apischema.Call[apischema.MountpointNameRequest, apischema.StoragePathResult]("storage.create_btrfs_subvolume").Handle(handleCreateBtrfsSubvolume),
	apischema.Call[apischema.NoRequest, []apischema.ApiDisk]("storage.get_drive_info", apischema.RetrySafe()).Handle(handleGetDriveInfo),
)

var Routes = apischema.CombineRoutes(api.Routes(), smartTestRoutes)

// RegisterHandlers registers all storage handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterTaskRoutes(router)

	api.Register(router)
}

func handleListPVs(ctx context.Context, _ apischema.NoRequest) ([]apischema.PhysicalVolume, error) {
	slog.Debug("Listing physical volumes")
	pvs, err := ListPhysicalVolumes(ctx)
	if err != nil {
		slog.Error("failed to list physical volumes", "error", err)
		return nil, err
	}
	slog.Debug("listed physical volumes", "count", len(pvs))
	return pvs, nil
}

func handleListVGs(ctx context.Context, _ apischema.NoRequest) ([]apischema.VolumeGroup, error) {
	slog.Debug("Listing volume groups")
	vgs, err := ListVolumeGroups(ctx)
	if err != nil {
		slog.Error("failed to list volume groups", "error", err)
		return nil, err
	}
	slog.Debug("listed volume groups", "count", len(vgs))
	return vgs, nil
}

func handleListLVs(ctx context.Context, _ apischema.NoRequest) ([]apischema.LogicalVolume, error) {
	slog.Debug("Listing logical volumes")
	lvs, err := ListLogicalVolumes(ctx)
	if err != nil {
		slog.Error("failed to list logical volumes", "error", err)
		return nil, err
	}
	slog.Debug("listed logical volumes", "count", len(lvs))
	return lvs, nil
}

func handleCreateLV(ctx context.Context, req apischema.CreateLogicalVolumeRequest) (apischema.SuccessPathResponse, error) {
	slog.Info("creating logical volume", "volume_group", req.VGName, "name", req.LVName, "size", req.Size)
	result, err := CreateLogicalVolume(ctx, req.VGName, req.LVName, req.Size)
	if err != nil {
		slog.Error("failed to create logical volume", "volume_group", req.VGName, "name", req.LVName, "error", err)
		return apischema.SuccessPathResponse{}, err
	}
	slog.Info("logical volume created", "volume_group", req.VGName, "name", req.LVName)
	return result, nil
}

func handleDeleteLV(ctx context.Context, req apischema.VolumeGroupLogicalVolumeRequest) (apischema.SuccessResponse, error) {
	slog.Info("deleting logical volume", "volume_group", req.VGName, "name", req.LVName)
	result, err := DeleteLogicalVolume(ctx, req.VGName, req.LVName)
	if err != nil {
		slog.Error("failed to delete logical volume", "volume_group", req.VGName, "name", req.LVName, "error", err)
		return apischema.SuccessResponse{}, err
	}
	slog.Info("logical volume deleted", "volume_group", req.VGName, "name", req.LVName)
	return result, nil
}

func handleResizeLV(ctx context.Context, req apischema.ResizeLogicalVolumeRequest) (apischema.SuccessResponse, error) {
	slog.Info("resizing logical volume", "volume_group", req.VGName, "name", req.LVName, "size", req.NewSize)
	result, err := ResizeLogicalVolume(ctx, req.VGName, req.LVName, req.NewSize)
	if err != nil {
		slog.Error("failed to resize logical volume", "volume_group", req.VGName, "name", req.LVName, "error", err)
		return apischema.SuccessResponse{}, err
	}
	slog.Info("logical volume resized", "volume_group", req.VGName, "name", req.LVName, "size", req.NewSize)
	return result, nil
}

func handleListNFSMounts(ctx context.Context, _ apischema.NoRequest) ([]apischema.NFSMount, error) {
	slog.Debug("Listing NFS mounts")
	mounts, err := ListNFSMounts(ctx)
	if err != nil {
		slog.Error("failed to list NFS mounts", "error", err)
		return nil, err
	}
	slog.Debug("listed NFS mounts", "count", len(mounts))
	return mounts, nil
}

func handleListNFSExports(ctx context.Context, req apischema.ServerRequest) ([]string, error) {
	slog.Debug("listing NFS exports", "server", req.Server)
	exports, err := ListNFSExports(ctx, req.Server)
	if err != nil {
		slog.Error("failed to list NFS exports", "server", req.Server, "error", err)
		return nil, err
	}
	slog.Debug("listed NFS exports", "server", req.Server, "count", len(exports))
	return exports, nil
}

func handleMountNFS(ctx context.Context, req apischema.ServerExportMountOptionsPersistRequest) (apischema.StorageMountResult, error) {
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
		return apischema.StorageMountResult{}, err
	}
	return result, nil
}

func handleUnmountNFS(ctx context.Context, req apischema.MountpointRemoveFstabRequest) (apischema.StorageWarningResult, error) {
	removeFstab := truthy(req.RemoveFstab)
	slog.Debug("unmount_nfs request", "mountpoint", req.Mountpoint, "remove_fstab", removeFstab)
	result, err := UnmountNFS(ctx, req.Mountpoint, removeFstab)
	if err != nil {
		slog.Error("failed to unmount NFS share", "mountpoint", req.Mountpoint, "error", err)
		return apischema.StorageWarningResult{}, err
	}
	return result, nil
}

func handleRemountNFS(ctx context.Context, req apischema.MountpointOptionsUpdateFstabRequest) (apischema.StorageMountResult, error) {
	updateFstab := truthy(req.UpdateFstab)
	slog.Debug("remount_nfs request", "mountpoint", req.Mountpoint, "options", req.Options, "update_fstab", updateFstab)
	result, err := RemountNFS(ctx, req.Mountpoint, req.Options, updateFstab)
	if err != nil {
		slog.Error("failed to remount NFS share", "mountpoint", req.Mountpoint, "error", err)
		return apischema.StorageMountResult{}, err
	}
	return result, nil
}

func handleListCIFSMounts(ctx context.Context, _ apischema.NoRequest) ([]apischema.CIFSMount, error) {
	slog.Debug("Listing CIFS mounts")
	mounts, err := ListCIFSMounts(ctx)
	if err != nil {
		slog.Error("failed to list CIFS mounts", "error", err)
		return nil, err
	}
	slog.Debug("listed CIFS mounts", "count", len(mounts))
	return mounts, nil
}

func handleListCIFSShares(ctx context.Context, req apischema.ServerRequest) ([]string, error) {
	slog.Debug("listing CIFS shares", "server", req.Server)
	shares, err := ListCIFSShares(ctx, req.Server)
	if err != nil {
		slog.Error("failed to list CIFS shares", "server", req.Server, "error", err)
		return nil, err
	}
	return shares, nil
}

func handleMountCIFS(ctx context.Context, req apischema.CIFSMountRequest) (apischema.StorageMountResult, error) {
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
		return apischema.StorageMountResult{}, err
	}
	return result, nil
}

func handleUnmountCIFS(ctx context.Context, req apischema.MountpointRemoveFstabRequest) (apischema.StorageWarningResult, error) {
	removeFstab := truthy(req.RemoveFstab)
	slog.Debug("unmount_cifs request", "mountpoint", req.Mountpoint, "remove_fstab", removeFstab)
	result, err := UnmountCIFS(ctx, req.Mountpoint, removeFstab)
	if err != nil {
		slog.Error("failed to unmount CIFS share", "mountpoint", req.Mountpoint, "error", err)
		return apischema.StorageWarningResult{}, err
	}
	return result, nil
}

func handleRemountCIFS(ctx context.Context, req apischema.MountpointOptionsUpdateFstabRequest) (apischema.StorageMountResult, error) {
	updateFstab := truthy(req.UpdateFstab)
	// Password safety: never log req.Options.
	slog.Debug("remount_cifs request", "mountpoint", req.Mountpoint, "update_fstab", updateFstab)
	result, err := RemountCIFS(ctx, req.Mountpoint, req.Options, updateFstab)
	if err != nil {
		slog.Error("failed to remount CIFS share", "mountpoint", req.Mountpoint, "error", err)
		return apischema.StorageMountResult{}, err
	}
	return result, nil
}

func handleUnmountFilesystem(ctx context.Context, req apischema.MountpointRequest) (apischema.StorageMountResult, error) {
	slog.Info("unmounting filesystem", "mountpoint", req.Mountpoint)
	result, err := UnmountFilesystem(ctx, req.Mountpoint)
	if err != nil {
		slog.Error("failed to unmount filesystem", "mountpoint", req.Mountpoint, "error", err)
		return apischema.StorageMountResult{}, err
	}
	slog.Info("filesystem unmounted", "mountpoint", req.Mountpoint)
	return result, nil
}

func handleCreateBtrfsSubvolume(ctx context.Context, req apischema.MountpointNameRequest) (apischema.StoragePathResult, error) {
	slog.Info("creating btrfs subvolume", "mountpoint", req.Mountpoint, "name", req.Name)
	result, err := CreateBtrfsSubvolume(ctx, req.Mountpoint, req.Name)
	if err != nil {
		slog.Error("failed to create btrfs subvolume", "mountpoint", req.Mountpoint, "name", req.Name, "error", err)
		return apischema.StoragePathResult{}, err
	}
	slog.Info("btrfs subvolume created", "mountpoint", req.Mountpoint, "name", req.Name)
	return result, nil
}

func handleGetDriveInfo(ctx context.Context, _ apischema.NoRequest) ([]apischema.ApiDisk, error) {
	return FetchDriveInfo(ctx)
}
