package storage

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
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
	apischema.Call[apischema.NoRequest, []btrfs.BtrfsFilesystem]("storage.btrfs_list", apischema.RetrySafe()).Handle(handleBtrfsList),
	apischema.Call[apischema.UUIDRequest, []btrfs.BtrfsSubvolume]("storage.btrfs_subvolumes", apischema.RetrySafe()).Handle(handleBtrfsSubvolumes),
	apischema.Call[apischema.BtrfsSubvolumeRequest, btrfs.BtrfsSubvolume]("storage.btrfs_subvolume_create", apischema.Privileged()).Handle(handleBtrfsSubvolumeCreate),
	apischema.Call[apischema.BtrfsSubvolumeRequest, apischema.SuccessResponse]("storage.btrfs_subvolume_delete", apischema.Privileged()).Handle(handleBtrfsSubvolumeDelete),
	apischema.Call[apischema.BtrfsSnapshotsRequest, []btrfs.BtrfsSnapshot]("storage.btrfs_snapshots", apischema.RetrySafe()).Handle(handleBtrfsSnapshots),
	apischema.Call[apischema.BtrfsSubvolumeRequest, btrfs.BtrfsSnapshot]("storage.btrfs_snapshot_create", apischema.Privileged()).Handle(handleBtrfsSnapshotCreate),
	apischema.Call[apischema.BtrfsSnapshotRequest, apischema.SuccessResponse]("storage.btrfs_snapshot_delete", apischema.Privileged()).Handle(handleBtrfsSnapshotDelete),
	apischema.Call[apischema.BtrfsSubvolumeReadOnlyRequest, btrfs.BtrfsSubvolume]("storage.btrfs_subvolume_set_readonly", apischema.Privileged()).Handle(handleBtrfsSubvolumeSetReadOnly),
	apischema.Call[apischema.BtrfsSubvolumeRequest, apischema.SuccessResponse]("storage.btrfs_subvolume_set_default", apischema.Privileged()).Handle(handleBtrfsSubvolumeSetDefault),
	apischema.Call[apischema.UUIDRequest, btrfs.BtrfsSettings]("storage.btrfs_settings", apischema.RetrySafe()).Handle(handleBtrfsSettings),
	apischema.Call[apischema.BtrfsQuotaRequest, btrfs.BtrfsSettings]("storage.btrfs_set_quotas", apischema.Privileged()).Handle(handleBtrfsSetQuotas),
	apischema.Call[apischema.BtrfsQgroupLimitRequest, btrfs.BtrfsSubvolume]("storage.btrfs_qgroup_limit", apischema.Privileged()).Handle(handleBtrfsQgroupLimit),
	apischema.Call[apischema.BtrfsCompressionRequest, btrfs.BtrfsSettings]("storage.btrfs_set_compression", apischema.Privileged()).Handle(handleBtrfsSetCompression),
	apischema.Call[apischema.BtrfsSubvolumeCompressionRequest, btrfs.BtrfsSubvolume]("storage.btrfs_subvolume_set_compression", apischema.Privileged()).Handle(handleBtrfsSubvolumeSetCompression),
	apischema.Call[apischema.NoRequest, btrfs.BtrfsSnapshotRules]("storage.btrfs_snapshot_rules", apischema.RetrySafe()).Handle(handleBtrfsSnapshotRules),
	apischema.Call[apischema.BtrfsSnapshotRuleRequest, btrfs.BtrfsSnapshotRules]("storage.btrfs_snapshot_rule_set", apischema.Privileged()).Handle(handleBtrfsSnapshotRuleSet),
	apischema.Call[apischema.BtrfsSubvolumeRequest, btrfs.BtrfsSnapshotRules]("storage.btrfs_snapshot_rule_delete", apischema.Privileged()).Handle(handleBtrfsSnapshotRuleDelete),
	apischema.Call[apischema.NoRequest, btrfs.RunReport]("storage.btrfs_snapshot_run_now", apischema.Privileged()).Handle(handleBtrfsSnapshotRunNow),
	apischema.Call[apischema.NoRequest, []btrfs.BtrfsReplicationJob]("storage.btrfs_replication_jobs", apischema.RetrySafe()).Handle(handleBtrfsReplicationJobs),
	apischema.Call[apischema.BtrfsReplicationJobRequest, []btrfs.BtrfsReplicationJob]("storage.btrfs_replication_job_set", apischema.Privileged()).Handle(handleBtrfsReplicationJobSet),
	apischema.Call[apischema.BtrfsJobIDRequest, []btrfs.BtrfsReplicationJob]("storage.btrfs_replication_job_delete", apischema.Privileged()).Handle(handleBtrfsReplicationJobDelete),
	apischema.Call[apischema.NoRequest, btrfs.BtrfsSSHKey]("storage.btrfs_replication_ssh_key", apischema.Privileged()).Handle(handleBtrfsReplicationSSHKey),
	apischema.Call[apischema.BtrfsSSHTrustRequest, btrfs.BtrfsSSHTrust]("storage.btrfs_replication_ssh_trust", apischema.Privileged()).Handle(handleBtrfsReplicationSSHTrust),
	apischema.Call[apischema.BtrfsDeviceAddRequest, btrfs.BtrfsFilesystem]("storage.btrfs_device_add", apischema.Privileged()).Handle(handleBtrfsDeviceAdd),
	apischema.Call[apischema.UUIDRequest, apischema.SuccessResponse]("storage.btrfs_balance_cancel", apischema.Privileged()).Handle(handleBtrfsBalanceCancel),
	apischema.Call[apischema.BtrfsPoolCreateRequest, btrfs.BtrfsFilesystem]("storage.btrfs_pool_create", apischema.Privileged()).Handle(handleBtrfsPoolCreate),
	apischema.Call[apischema.UUIDRequest, apischema.SuccessResponse]("storage.btrfs_pool_destroy", apischema.Privileged()).Handle(handleBtrfsPoolDestroy),
	apischema.Call[apischema.BtrfsPoolMountRequest, btrfs.BtrfsFilesystem]("storage.btrfs_pool_mount", apischema.Privileged()).Handle(handleBtrfsPoolMount),
	apischema.Call[apischema.NoRequest, []btrfs.BtrfsOfflinePool]("storage.btrfs_pools_offline", apischema.RetrySafe()).Handle(handleBtrfsPoolsOffline),
	apischema.Call[apischema.BtrfsDeviceRequest, btrfs.BtrfsFilesystem]("storage.btrfs_device_grow", apischema.Privileged()).Handle(handleBtrfsDeviceGrow),
	apischema.Call[apischema.BtrfsLabelRequest, btrfs.BtrfsFilesystem]("storage.btrfs_set_label", apischema.Privileged()).Handle(handleBtrfsSetLabel),
	apischema.Call[apischema.UUIDRequest, apischema.SuccessResponse]("storage.btrfs_scrub_cancel", apischema.Privileged()).Handle(handleBtrfsScrubCancel),
	apischema.Call[apischema.NoRequest, apischema.BtrfsMaintenanceSchedule]("storage.get_btrfs_maintenance", apischema.RetrySafe()).Handle(handleGetBtrfsMaintenance),
	apischema.Call[apischema.BtrfsMaintenanceRequest, apischema.BtrfsMaintenanceSchedule]("storage.set_btrfs_maintenance").Handle(handleSetBtrfsMaintenance),
	apischema.Call[apischema.NoRequest, []apischema.ApiDisk]("storage.get_drive_info", apischema.RetrySafe()).Handle(handleGetDriveInfo),
	apischema.Call[apischema.NoRequest, apischema.StorageTopologyInventory]("storage.get_topology", apischema.RetrySafe()).Handle(handleGetTopology),
)

var Routes = apischema.CombineRoutes(api.Routes(), smartTestRoutes, btrfsRollbackRoutes, btrfsDeviceRoutes, btrfsReplicationRoutes)

// RegisterHandlers registers all storage handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterTaskRoutes(router)

	api.Register(router)
}

func handleBtrfsList(ctx context.Context, _ apischema.NoRequest) ([]btrfs.BtrfsFilesystem, error) {
	return ListBtrfsFilesystems(ctx)
}

func handleBtrfsSubvolumes(ctx context.Context, req apischema.UUIDRequest) ([]btrfs.BtrfsSubvolume, error) {
	return btrfs.ListSubvolumes(ctx, req.UUID)
}

func handleBtrfsSubvolumeCreate(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (btrfs.BtrfsSubvolume, error) {
	return CreateBtrfsSubvolume(ctx, req)
}

func handleBtrfsSubvolumeDelete(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (apischema.SuccessResponse, error) {
	if err := DeleteBtrfsSubvolume(ctx, req); err != nil {
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

func handleBtrfsSnapshots(ctx context.Context, req apischema.BtrfsSnapshotsRequest) ([]btrfs.BtrfsSnapshot, error) {
	return btrfs.ListSnapshots(ctx, req.UUID, req.Path)
}

func handleBtrfsSnapshotCreate(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (btrfs.BtrfsSnapshot, error) {
	return CreateBtrfsSnapshot(ctx, req)
}

func handleBtrfsSnapshotDelete(ctx context.Context, req apischema.BtrfsSnapshotRequest) (apischema.SuccessResponse, error) {
	if err := DeleteBtrfsSnapshot(ctx, req); err != nil {
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

func handleBtrfsSubvolumeSetReadOnly(ctx context.Context, req apischema.BtrfsSubvolumeReadOnlyRequest) (btrfs.BtrfsSubvolume, error) {
	return SetBtrfsSubvolumeReadOnly(ctx, req)
}

func handleBtrfsSubvolumeSetDefault(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (apischema.SuccessResponse, error) {
	if err := SetBtrfsDefaultSubvolume(ctx, req); err != nil {
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

func handleBtrfsSettings(ctx context.Context, req apischema.UUIDRequest) (btrfs.BtrfsSettings, error) {
	return GetBtrfsSettings(ctx, req.UUID)
}

func handleBtrfsSetQuotas(ctx context.Context, req apischema.BtrfsQuotaRequest) (btrfs.BtrfsSettings, error) {
	return SetBtrfsQuotas(ctx, req)
}

func handleBtrfsQgroupLimit(ctx context.Context, req apischema.BtrfsQgroupLimitRequest) (btrfs.BtrfsSubvolume, error) {
	return LimitBtrfsQgroup(ctx, req)
}

func handleBtrfsSetCompression(ctx context.Context, req apischema.BtrfsCompressionRequest) (btrfs.BtrfsSettings, error) {
	return ApplyBtrfsCompression(ctx, req)
}

func handleBtrfsSubvolumeSetCompression(ctx context.Context, req apischema.BtrfsSubvolumeCompressionRequest) (btrfs.BtrfsSubvolume, error) {
	return SetBtrfsSubvolumeCompression(ctx, req)
}

func handleBtrfsSnapshotRules(ctx context.Context, _ apischema.NoRequest) (btrfs.BtrfsSnapshotRules, error) {
	return LoadBtrfsSnapshotRules(ctx)
}

func handleBtrfsSnapshotRuleSet(ctx context.Context, req apischema.BtrfsSnapshotRuleRequest) (btrfs.BtrfsSnapshotRules, error) {
	return SaveBtrfsSnapshotRule(ctx, req.Rule)
}

func handleBtrfsSnapshotRuleDelete(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (btrfs.BtrfsSnapshotRules, error) {
	return DeleteBtrfsSnapshotRule(ctx, req.UUID, req.Path)
}

func handleBtrfsSnapshotRunNow(ctx context.Context, _ apischema.NoRequest) (btrfs.RunReport, error) {
	return RunBtrfsSnapshotRules(ctx)
}

func handleBtrfsReplicationJobs(_ context.Context, _ apischema.NoRequest) ([]btrfs.BtrfsReplicationJob, error) {
	return btrfs.LoadJobs()
}

func handleBtrfsReplicationJobSet(ctx context.Context, req apischema.BtrfsReplicationJobRequest) ([]btrfs.BtrfsReplicationJob, error) {
	return SaveBtrfsReplicationJob(ctx, req.Job)
}

func handleBtrfsReplicationJobDelete(ctx context.Context, req apischema.BtrfsJobIDRequest) ([]btrfs.BtrfsReplicationJob, error) {
	return DeleteBtrfsReplicationJob(ctx, req.ID)
}

func handleBtrfsReplicationSSHKey(ctx context.Context, _ apischema.NoRequest) (btrfs.BtrfsSSHKey, error) {
	return EnsureBtrfsReplicationSSHKey(ctx)
}

func handleBtrfsReplicationSSHTrust(ctx context.Context, req apischema.BtrfsSSHTrustRequest) (btrfs.BtrfsSSHTrust, error) {
	return CheckBtrfsReplicationHost(ctx, req)
}

func handleBtrfsDeviceAdd(ctx context.Context, req apischema.BtrfsDeviceAddRequest) (btrfs.BtrfsFilesystem, error) {
	return AddBtrfsDevice(ctx, req)
}

func handleBtrfsBalanceCancel(ctx context.Context, req apischema.UUIDRequest) (apischema.SuccessResponse, error) {
	if err := CancelBtrfsBalance(ctx, req.UUID); err != nil {
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

func handleBtrfsScrubCancel(ctx context.Context, req apischema.UUIDRequest) (apischema.SuccessResponse, error) {
	if err := CancelBtrfsScrub(ctx, req.UUID); err != nil {
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

func handleBtrfsPoolCreate(ctx context.Context, req apischema.BtrfsPoolCreateRequest) (btrfs.BtrfsFilesystem, error) {
	return CreateBtrfsPool(ctx, req)
}

func handleBtrfsPoolDestroy(ctx context.Context, req apischema.UUIDRequest) (apischema.SuccessResponse, error) {
	if err := DestroyBtrfsPool(ctx, req.UUID); err != nil {
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

func handleBtrfsPoolMount(ctx context.Context, req apischema.BtrfsPoolMountRequest) (btrfs.BtrfsFilesystem, error) {
	return MountBtrfsPool(ctx, req)
}

func handleBtrfsPoolsOffline(ctx context.Context, _ apischema.NoRequest) ([]btrfs.BtrfsOfflinePool, error) {
	return ListOfflinePools(ctx)
}

func handleBtrfsDeviceGrow(ctx context.Context, req apischema.BtrfsDeviceRequest) (btrfs.BtrfsFilesystem, error) {
	return GrowBtrfsDevice(ctx, req)
}

func handleBtrfsSetLabel(ctx context.Context, req apischema.BtrfsLabelRequest) (btrfs.BtrfsFilesystem, error) {
	return SetBtrfsLabel(ctx, req)
}

func handleListPVs(ctx context.Context, _ apischema.NoRequest) ([]apischema.PhysicalVolume, error) {
	return ListPhysicalVolumes(ctx)
}

func handleListVGs(ctx context.Context, _ apischema.NoRequest) ([]apischema.VolumeGroup, error) {
	return ListVolumeGroups(ctx)
}

func handleListLVs(ctx context.Context, _ apischema.NoRequest) ([]apischema.LogicalVolume, error) {
	return ListLogicalVolumes(ctx)
}

func handleCreateLV(ctx context.Context, req apischema.CreateLogicalVolumeRequest) (apischema.SuccessPathResponse, error) {
	return CreateLogicalVolume(ctx, req.VGName, req.LVName, req.Size)
}

func handleDeleteLV(ctx context.Context, req apischema.VolumeGroupLogicalVolumeRequest) (apischema.SuccessResponse, error) {
	return DeleteLogicalVolume(ctx, req.VGName, req.LVName)
}

func handleResizeLV(ctx context.Context, req apischema.ResizeLogicalVolumeRequest) (apischema.SuccessResponse, error) {
	return ResizeLogicalVolume(ctx, req.VGName, req.LVName, req.NewSize)
}

func handleListNFSMounts(ctx context.Context, _ apischema.NoRequest) ([]apischema.NFSMount, error) {
	return ListNFSMounts(ctx)
}

func handleListNFSExports(ctx context.Context, req apischema.ServerRequest) ([]string, error) {
	return ListNFSExports(ctx, req.Server)
}

func handleMountNFS(ctx context.Context, req apischema.ServerExportMountOptionsPersistRequest) (apischema.StorageMountResult, error) {
	return MountNFS(ctx, req.Server, req.ExportPath, req.Mountpoint, req.Options, truthy(req.Persist))
}

func handleUnmountNFS(ctx context.Context, req apischema.MountpointRemoveFstabRequest) (apischema.StorageWarningResult, error) {
	return UnmountNFS(ctx, req.Mountpoint, truthy(req.RemoveFstab))
}

func handleRemountNFS(ctx context.Context, req apischema.MountpointOptionsUpdateFstabRequest) (apischema.StorageMountResult, error) {
	return RemountNFS(ctx, req.Mountpoint, req.Options, truthy(req.UpdateFstab))
}

func handleListCIFSMounts(ctx context.Context, _ apischema.NoRequest) ([]apischema.CIFSMount, error) {
	return ListCIFSMounts(ctx)
}

func handleListCIFSShares(ctx context.Context, req apischema.ServerRequest) ([]string, error) {
	return ListCIFSShares(ctx, req.Server)
}

func handleMountCIFS(ctx context.Context, req apischema.CIFSMountRequest) (apischema.StorageMountResult, error) {
	return MountCIFS(ctx, cifsMountParams{
		server:     req.Server,
		share:      req.Share,
		mountpoint: req.Mountpoint,
		username:   req.Username,
		password:   req.Password,
		domain:     req.Domain,
		options:    req.Options,
	})
}

func handleUnmountCIFS(ctx context.Context, req apischema.MountpointRemoveFstabRequest) (apischema.StorageWarningResult, error) {
	return UnmountCIFS(ctx, req.Mountpoint, truthy(req.RemoveFstab))
}

func handleRemountCIFS(ctx context.Context, req apischema.MountpointOptionsUpdateFstabRequest) (apischema.StorageMountResult, error) {
	return RemountCIFS(ctx, req.Mountpoint, req.Options, truthy(req.UpdateFstab))
}

func handleUnmountFilesystem(ctx context.Context, req apischema.MountpointRequest) (apischema.StorageMountResult, error) {
	return UnmountFilesystem(ctx, req.Mountpoint)
}

func handleGetBtrfsMaintenance(ctx context.Context, _ apischema.NoRequest) (apischema.BtrfsMaintenanceSchedule, error) {
	return GetBtrfsMaintenance(ctx)
}

func handleSetBtrfsMaintenance(ctx context.Context, req apischema.BtrfsMaintenanceRequest) (apischema.BtrfsMaintenanceSchedule, error) {
	return SetBtrfsMaintenance(ctx, req)
}

func handleGetDriveInfo(ctx context.Context, _ apischema.NoRequest) ([]apischema.ApiDisk, error) {
	return FetchDriveInfo(ctx)
}

func handleGetTopology(ctx context.Context, _ apischema.NoRequest) (apischema.StorageTopologyInventory, error) {
	return GetTopology(ctx)
}
