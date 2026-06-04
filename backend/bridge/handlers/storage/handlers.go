package storage

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteCreateBtrfsSubvolume = routes.Job("storage.create_btrfs_subvolume", apischema.TypeOf[apischema.MountpointNameRequest](), apischema.TypeOf[apischema.StoragePathResult]())
var RouteCreateLv = routes.Job("storage.create_lv", apischema.TypeOf[apischema.CreateLogicalVolumeRequest](), apischema.TypeOf[apischema.StorageCreateLVResult]())
var RouteDeleteLv = routes.Job("storage.delete_lv", apischema.TypeOf[apischema.VolumeGroupLogicalVolumeRequest](), apischema.TypeOf[apischema.SuccessResponse]())
var RouteGetDriveInfo = routes.Query("storage.get_drive_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.ApiDisk]())
var RouteListLVs = routes.Query("storage.list_lvs", apischema.NoRequest(), apischema.TypeOf[[]apischema.LogicalVolume]())
var RouteListNFSExports = routes.Query("storage.list_nfs_exports", apischema.TypeOf[apischema.ServerRequest](), apischema.TypeOf[[]string]())
var RouteListNFSMounts = routes.Query("storage.list_nfs_mounts", apischema.NoRequest(), apischema.TypeOf[[]apischema.NFSMount]())
var RouteListPVs = routes.Query("storage.list_pvs", apischema.NoRequest(), apischema.TypeOf[[]apischema.PhysicalVolume]())
var RouteListVGs = routes.Query("storage.list_vgs", apischema.NoRequest(), apischema.TypeOf[[]apischema.VolumeGroup]())
var RouteMountNFS = routes.Job("storage.mount_nfs", apischema.TypeOf[apischema.ServerExportMountOptionsPersistRequest](), apischema.TypeOf[apischema.StorageMountResult]())
var RouteRemountNFS = routes.Job("storage.remount_nfs", apischema.TypeOf[apischema.MountpointOptionsUpdateFstabRequest](), apischema.TypeOf[apischema.StorageMountResult]())
var RouteResizeLv = routes.Job("storage.resize_lv", apischema.TypeOf[apischema.ResizeLogicalVolumeRequest](), apischema.TypeOf[apischema.SuccessResponse]())
var RouteRunSmartTest = routes.Runner("storage.run_smart_test", apischema.TypeOf[apischema.DeviceTestTypeRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var RouteUnmountFilesystem = routes.Job("storage.unmount_filesystem", apischema.TypeOf[apischema.MountpointRequest](), apischema.TypeOf[apischema.StorageMountResult]())
var RouteUnmountNFS = routes.Job("storage.unmount_nfs", apischema.TypeOf[apischema.MountpointRemoveFstabRequest](), apischema.TypeOf[apischema.StorageWarningResult]())

var Routes = routes.All()

// RegisterHandlers registers all storage handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterJobRoutes(router)

	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: RouteListPVs, Handle: handleListPVs},
		{Route: RouteListVGs, Handle: handleListVGs},
		{Route: RouteListLVs, Handle: handleListLVs},
		{Route: RouteCreateLv, Handle: handleCreateLV},
		{Route: RouteDeleteLv, Handle: handleDeleteLV},
		{Route: RouteResizeLv, Handle: handleResizeLV},
		{Route: RouteListNFSMounts, Handle: handleListNFSMounts},
		{Route: RouteListNFSExports, Handle: handleListNFSExports},
		{Route: RouteMountNFS, Handle: handleMountNFS},
		{Route: RouteUnmountNFS, Handle: handleUnmountNFS},
		{Route: RouteRemountNFS, Handle: handleRemountNFS},
		{Route: RouteUnmountFilesystem, Handle: handleUnmountFilesystem},
		{Route: RouteCreateBtrfsSubvolume, Handle: handleCreateBtrfsSubvolume},
		{Route: RouteGetDriveInfo, Handle: handleGetDriveInfo},
	})
}

func handleListPVs(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	slog.Debug("Listing physical volumes")
	pvs, err := ListPhysicalVolumes(ctx)
	if err != nil {
		slog.Error("failed to list physical volumes", "error", err)
		return err
	}
	slog.Debug("listed physical volumes", "count", len(pvs))
	return bridgeipc.EmitResult(emit, pvs, nil)
}

func handleListVGs(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	slog.Debug("Listing volume groups")
	vgs, err := ListVolumeGroups(ctx)
	if err != nil {
		slog.Error("failed to list volume groups", "error", err)
		return err
	}
	slog.Debug("listed volume groups", "count", len(vgs))
	return bridgeipc.EmitResult(emit, vgs, nil)
}

func handleListLVs(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
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

func handleListNFSMounts(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
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

func handleGetDriveInfo(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	driveInfo, err := FetchDriveInfo(ctx)
	if err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, driveInfo, nil)
}
