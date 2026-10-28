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
	apischema.Mutation("storage.create_lv", handleCreateLV, apischema.Privileged()),
	apischema.Mutation("storage.delete_lv", handleDeleteLV, apischema.Privileged()),
	apischema.Mutation("storage.resize_lv", handleResizeLV, apischema.Privileged()),
	apischema.Call[apischema.NoRequest, []apischema.NFSMount]("storage.list_nfs_mounts", apischema.RetrySafe()).Handle(handleListNFSMounts),
	apischema.Call[apischema.ServerRequest, []string]("storage.list_nfs_exports", apischema.RetrySafe()).Handle(handleListNFSExports),
	apischema.Mutation("storage.mount_nfs", handleMountNFS, apischema.Privileged()),
	apischema.Mutation("storage.unmount_nfs", handleUnmountNFS, apischema.Privileged()),
	apischema.Mutation("storage.remount_nfs", handleRemountNFS, apischema.Privileged()),
	apischema.Call[apischema.NoRequest, []apischema.CIFSMount]("storage.list_cifs_mounts", apischema.RetrySafe()).Handle(handleListCIFSMounts),
	apischema.Call[apischema.ServerRequest, []string]("storage.list_cifs_shares", apischema.RetrySafe()).Handle(handleListCIFSShares),
	apischema.Mutation("storage.mount_cifs", handleMountCIFS, apischema.Privileged()),
	apischema.Mutation("storage.unmount_cifs", handleUnmountCIFS, apischema.Privileged()),
	apischema.Mutation("storage.remount_cifs", handleRemountCIFS, apischema.Privileged()),
	apischema.Mutation("storage.unmount_filesystem", handleUnmountFilesystem, apischema.Privileged()),
	apischema.Mutation("storage.partition_delete", DeletePartition, apischema.Privileged()),
	apischema.Mutation("storage.partition_mount", MountPartition, apischema.Privileged()),
	apischema.Mutation("storage.disk_initialize", InitializeDisk, apischema.Privileged()),
	apischema.Call[apischema.NoRequest, []apischema.MDArray]("storage.md_list", apischema.RetrySafe()).Handle(handleMDList),
	apischema.Mutation("storage.md_sync_action", handleMDSyncAction, apischema.Privileged()),
	apischema.Mutation("storage.md_member_fail", handleMDMemberFail, apischema.Privileged()),
	apischema.Mutation("storage.md_member_remove", handleMDMemberRemove, apischema.Privileged()),
	apischema.Mutation("storage.md_member_add", MDMemberAdd, apischema.Privileged()),
	apischema.Mutation("storage.md_member_replace", MDMemberReplace, apischema.Privileged()),
	apischema.Call[apischema.NoRequest, []btrfs.BtrfsFilesystem]("storage.btrfs_list", apischema.RetrySafe()).Handle(handleBtrfsList),
	apischema.Call[apischema.UUIDRequest, []btrfs.BtrfsSubvolume]("storage.btrfs_subvolumes", apischema.RetrySafe()).Handle(handleBtrfsSubvolumes),
	apischema.Mutation("storage.btrfs_subvolume_create", handleBtrfsSubvolumeCreate, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_subvolume_delete", handleBtrfsSubvolumeDelete, apischema.Privileged()),
	apischema.Call[apischema.BtrfsSnapshotsRequest, []btrfs.BtrfsSnapshot]("storage.btrfs_snapshots", apischema.RetrySafe()).Handle(handleBtrfsSnapshots),
	apischema.Mutation("storage.btrfs_snapshot_create", handleBtrfsSnapshotCreate, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_snapshot_delete", handleBtrfsSnapshotDelete, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_subvolume_set_readonly", handleBtrfsSubvolumeSetReadOnly, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_subvolume_set_default", handleBtrfsSubvolumeSetDefault, apischema.Privileged()),
	apischema.Call[apischema.UUIDRequest, btrfs.BtrfsSettings]("storage.btrfs_settings", apischema.RetrySafe()).Handle(handleBtrfsSettings),
	apischema.Mutation("storage.btrfs_set_quotas", handleBtrfsSetQuotas, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_qgroup_limit", handleBtrfsQgroupLimit, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_set_compression", handleBtrfsSetCompression, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_subvolume_set_compression", handleBtrfsSubvolumeSetCompression, apischema.Privileged()),
	apischema.Call[apischema.NoRequest, btrfs.BtrfsSnapshotRules]("storage.btrfs_snapshot_rules", apischema.RetrySafe()).Handle(handleBtrfsSnapshotRules),
	apischema.Mutation("storage.btrfs_snapshot_rule_set", handleBtrfsSnapshotRuleSet, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_snapshot_rule_delete", handleBtrfsSnapshotRuleDelete, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_snapshot_run_now", handleBtrfsSnapshotRunNow, apischema.Privileged()),
	apischema.Call[apischema.NoRequest, []btrfs.BtrfsReplicationJob]("storage.btrfs_replication_jobs", apischema.RetrySafe()).Handle(handleBtrfsReplicationJobs),
	apischema.Mutation("storage.btrfs_replication_job_set", handleBtrfsReplicationJobSet, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_replication_job_delete", handleBtrfsReplicationJobDelete, apischema.Privileged()),
	apischema.Call[apischema.NoRequest, btrfs.BtrfsSSHKey]("storage.btrfs_replication_ssh_key", apischema.Privileged()).Handle(handleBtrfsReplicationSSHKey),
	apischema.Mutation("storage.btrfs_replication_ssh_trust", handleBtrfsReplicationSSHTrust, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_device_add", handleBtrfsDeviceAdd, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_balance_cancel", handleBtrfsBalanceCancel, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_pool_destroy", handleBtrfsPoolDestroy, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_pool_mount", handleBtrfsPoolMount, apischema.Privileged()),
	apischema.Call[apischema.NoRequest, []btrfs.BtrfsOfflinePool]("storage.btrfs_pools_offline", apischema.RetrySafe()).Handle(handleBtrfsPoolsOffline),
	apischema.Mutation("storage.btrfs_device_grow", handleBtrfsDeviceGrow, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_set_label", handleBtrfsSetLabel, apischema.Privileged()),
	apischema.Mutation("storage.btrfs_scrub_cancel", handleBtrfsScrubCancel, apischema.Privileged()),
	apischema.Call[apischema.NoRequest, apischema.BtrfsMaintenanceSchedule]("storage.get_btrfs_maintenance", apischema.RetrySafe()).Handle(handleGetBtrfsMaintenance),
	apischema.Mutation("storage.set_btrfs_maintenance", handleSetBtrfsMaintenance, apischema.Privileged()),
	apischema.Call[apischema.NoRequest, []apischema.ApiDisk]("storage.get_drive_info", apischema.RetrySafe()).Handle(handleGetDriveInfo),
	apischema.Call[apischema.NoRequest, apischema.StorageTopologyInventory]("storage.get_topology", apischema.RetrySafe()).Handle(handleGetTopology),
)

var Routes = apischema.CombineRoutes(api.Routes(), smartTestRoutes, btrfsRollbackRoutes, btrfsDeviceRoutes, btrfsReplicationRoutes, partitionRoutes)

// RegisterHandlers registers all storage handlers with the global registry
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	RegisterTaskRoutes(router)

	api.Register(router)
}

func handleMDList(_ context.Context, _ apischema.NoRequest) ([]apischema.MDArray, error) {
	return ListMDArrays()
}

func handleMDSyncAction(_ context.Context, req apischema.MDSyncActionRequest) (apischema.MDArray, error) {
	return MDSyncAction(req)
}

func handleMDMemberFail(_ context.Context, req apischema.MDMemberRequest) (apischema.MDArray, error) {
	return MDMemberFail(req)
}

func handleMDMemberRemove(_ context.Context, req apischema.MDMemberRequest) (apischema.MDArray, error) {
	return MDMemberRemove(req)
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

func handleUnmountFilesystem(ctx context.Context, req apischema.MountpointRemoveFstabRequest) (apischema.StorageMountResult, error) {
	return UnmountFilesystem(ctx, req.Mountpoint, truthy(req.RemoveFstab))
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
