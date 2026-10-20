package storage

import (
	"context"
	"log/slog"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
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
	slog.Debug("Listing btrfs filesystems")
	filesystems, err := btrfs.ListFilesystems(ctx)
	if err != nil {
		slog.Error("failed to list btrfs filesystems", "error", err)
		return nil, err
	}
	for _, fs := range filesystems {
		raiseBtrfsHealthAlerts(ctx, fs)
	}
	slog.Debug("listed btrfs filesystems", "count", len(filesystems))
	return filesystems, nil
}

func handleBtrfsSubvolumes(ctx context.Context, req apischema.UUIDRequest) ([]btrfs.BtrfsSubvolume, error) {
	slog.Debug("Listing btrfs subvolumes", "uuid", req.UUID)
	return btrfs.ListSubvolumes(ctx, req.UUID)
}

func handleBtrfsSubvolumeCreate(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (btrfs.BtrfsSubvolume, error) {
	slog.Info("creating btrfs subvolume", "uuid", req.UUID, "path", req.Path)
	sv, err := btrfs.CreateSubvolume(ctx, req.UUID, req.Path)
	if err != nil {
		slog.Error("failed to create btrfs subvolume", "uuid", req.UUID, "path", req.Path, "error", err)
	}
	return sv, err
}

func handleBtrfsSubvolumeDelete(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (apischema.SuccessResponse, error) {
	slog.Info("deleting btrfs subvolume", "uuid", req.UUID, "path", req.Path)
	if err := btrfs.DeleteSubvolume(ctx, req.UUID, req.Path); err != nil {
		slog.Error("failed to delete btrfs subvolume", "uuid", req.UUID, "path", req.Path, "error", err)
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

func handleBtrfsSnapshots(ctx context.Context, req apischema.BtrfsSnapshotsRequest) ([]btrfs.BtrfsSnapshot, error) {
	slog.Debug("Listing btrfs snapshots", "uuid", req.UUID)
	return btrfs.ListSnapshots(ctx, req.UUID, req.Path)
}

func handleBtrfsSnapshotCreate(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (btrfs.BtrfsSnapshot, error) {
	slog.Info("creating btrfs snapshot", "uuid", req.UUID, "source", req.Path)
	snap, err := btrfs.CreateSnapshot(ctx, req.UUID, req.Path, btrfs.KindManual, time.Now())
	if err != nil {
		slog.Error("failed to create btrfs snapshot", "uuid", req.UUID, "source", req.Path, "error", err)
	}
	return snap, err
}

func handleBtrfsSnapshotDelete(ctx context.Context, req apischema.BtrfsSnapshotRequest) (apischema.SuccessResponse, error) {
	slog.Info("deleting btrfs snapshot", "uuid", req.UUID, "path", req.SnapshotPath)
	if err := btrfs.DeleteSnapshot(ctx, req.UUID, req.SnapshotPath); err != nil {
		slog.Error("failed to delete btrfs snapshot", "uuid", req.UUID, "path", req.SnapshotPath, "error", err)
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

func handleBtrfsSubvolumeSetReadOnly(ctx context.Context, req apischema.BtrfsSubvolumeReadOnlyRequest) (btrfs.BtrfsSubvolume, error) {
	slog.Info("setting btrfs subvolume read-only", "uuid", req.UUID, "path", req.Path, "readOnly", req.ReadOnly)
	sv, err := btrfs.SetSubvolumeReadOnly(ctx, req.UUID, req.Path, req.ReadOnly)
	if err != nil {
		slog.Error("failed to set btrfs subvolume read-only", "uuid", req.UUID, "path", req.Path, "error", err)
	}
	return sv, err
}

func handleBtrfsSubvolumeSetDefault(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (apischema.SuccessResponse, error) {
	slog.Info("setting default btrfs subvolume", "uuid", req.UUID, "path", req.Path)
	if err := btrfs.SetDefaultSubvolume(ctx, req.UUID, req.Path); err != nil {
		slog.Error("failed to set default btrfs subvolume", "uuid", req.UUID, "path", req.Path, "error", err)
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

func handleBtrfsDeviceAdd(ctx context.Context, req apischema.BtrfsDeviceAddRequest) (btrfs.BtrfsFilesystem, error) {
	if err := ensureBtrfsDeviceFree(ctx, req.Device, req.Force); err != nil {
		return btrfs.BtrfsFilesystem{}, err
	}
	slog.Info("adding btrfs device", "uuid", req.UUID, "device", req.Device, "force", req.Force)
	if err := btrfs.AddDevice(ctx, req.UUID, req.Device, req.Force); err != nil {
		slog.Error("failed to add btrfs device", "uuid", req.UUID, "device", req.Device, "error", err)
		return btrfs.BtrfsFilesystem{}, err
	}
	return btrfs.FindFilesystem(ctx, req.UUID)
}

func handleBtrfsBalanceCancel(ctx context.Context, req apischema.UUIDRequest) (apischema.SuccessResponse, error) {
	slog.Info("canceling btrfs balance", "uuid", req.UUID)
	if err := btrfs.BalanceCancel(ctx, req.UUID); err != nil {
		slog.Error("failed to cancel btrfs balance", "uuid", req.UUID, "error", err)
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

func handleBtrfsScrubCancel(ctx context.Context, req apischema.UUIDRequest) (apischema.SuccessResponse, error) {
	slog.Info("canceling btrfs scrub", "uuid", req.UUID)
	if err := btrfs.ScrubCancel(ctx, req.UUID); err != nil {
		slog.Error("failed to cancel btrfs scrub", "uuid", req.UUID, "error", err)
		return apischema.SuccessResponse{}, err
	}
	return apischema.SuccessResponse{Success: true}, nil
}

func handleBtrfsSnapshotRules(ctx context.Context, _ apischema.NoRequest) (btrfs.BtrfsSnapshotRules, error) {
	rules, err := btrfs.LoadRules()
	if err != nil {
		return btrfs.BtrfsSnapshotRules{}, err
	}
	return btrfsSnapshotRules(ctx, rules), nil
}

func handleBtrfsSnapshotRuleSet(ctx context.Context, req apischema.BtrfsSnapshotRuleRequest) (btrfs.BtrfsSnapshotRules, error) {
	slog.Info("setting btrfs snapshot rule", "uuid", req.Rule.UUID, "path", req.Rule.Path, "enabled", req.Rule.Enabled)
	rules, err := setBtrfsSnapshotRule(ctx, req.Rule)
	if err != nil {
		slog.Error("failed to set btrfs snapshot rule", "uuid", req.Rule.UUID, "path", req.Rule.Path, "error", err)
		return btrfs.BtrfsSnapshotRules{}, err
	}
	return btrfsSnapshotRules(ctx, rules), nil
}

func handleBtrfsSnapshotRuleDelete(ctx context.Context, req apischema.BtrfsSubvolumeRequest) (btrfs.BtrfsSnapshotRules, error) {
	slog.Info("deleting btrfs snapshot rule", "uuid", req.UUID, "path", req.Path)
	rules, err := updateBtrfsSnapshotRules(ctx, func(rules *[]btrfs.BtrfsSnapshotRule) error {
		*rules = btrfs.DeleteRule(*rules, req.UUID, req.Path)
		return nil
	})
	if err != nil {
		slog.Error("failed to delete btrfs snapshot rule", "uuid", req.UUID, "path", req.Path, "error", err)
		return btrfs.BtrfsSnapshotRules{}, err
	}
	return btrfsSnapshotRules(ctx, rules), nil
}

func handleBtrfsReplicationJobs(_ context.Context, _ apischema.NoRequest) ([]btrfs.BtrfsReplicationJob, error) {
	return btrfs.LoadJobs()
}

func handleBtrfsReplicationJobSet(ctx context.Context, req apischema.BtrfsReplicationJobRequest) ([]btrfs.BtrfsReplicationJob, error) {
	slog.Info("setting btrfs replication job", "id", req.Job.ID, "uuid", req.Job.UUID, "path", req.Job.Path, "target", req.Job.Target.Kind)
	jobs, err := setBtrfsReplicationJob(ctx, req.Job)
	if err != nil {
		slog.Error("failed to set btrfs replication job", "id", req.Job.ID, "uuid", req.Job.UUID, "path", req.Job.Path, "error", err)
		return nil, err
	}
	return jobs, nil
}

func handleBtrfsReplicationJobDelete(ctx context.Context, req apischema.BtrfsJobIDRequest) ([]btrfs.BtrfsReplicationJob, error) {
	slog.Info("deleting btrfs replication job", "id", req.ID)
	jobs, err := updateBtrfsReplicationJobs(ctx, func(jobs *[]btrfs.BtrfsReplicationJob) error {
		*jobs = btrfs.DeleteJob(*jobs, req.ID)
		return nil
	})
	if err != nil {
		slog.Error("failed to delete btrfs replication job", "id", req.ID, "error", err)
		return nil, err
	}
	return jobs, nil
}

func handleBtrfsReplicationSSHKey(ctx context.Context, _ apischema.NoRequest) (btrfs.BtrfsSSHKey, error) {
	pub, err := btrfs.EnsureSSHKey(ctx)
	if err != nil {
		slog.Error("failed to prepare btrfs replication ssh key", "error", err)
		return btrfs.BtrfsSSHKey{}, err
	}
	return btrfs.BtrfsSSHKey{PublicKey: pub}, nil
}

func handleBtrfsReplicationSSHTrust(ctx context.Context, req apischema.BtrfsSSHTrustRequest) (btrfs.BtrfsSSHTrust, error) {
	slog.Info("checking btrfs replication host key", "host", req.Host, "port", req.Port, "confirm", req.Confirm)
	trust, err := trustBtrfsReplicationHost(ctx, req)
	if err != nil {
		slog.Error("failed to check btrfs replication host key", "host", req.Host, "port", req.Port, "error", err)
		return btrfs.BtrfsSSHTrust{}, err
	}
	return trust, nil
}

func handleBtrfsSnapshotRunNow(ctx context.Context, _ apischema.NoRequest) (btrfs.RunReport, error) {
	rules, err := btrfs.LoadRules()
	if err != nil {
		return btrfs.RunReport{}, err
	}
	slog.Info("running btrfs snapshot rules", "rules", len(rules))
	return btrfs.Run(ctx, rules, time.Now(), btrfs.KindTimeline)
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

func handleGetBtrfsMaintenance(ctx context.Context, _ apischema.NoRequest) (apischema.BtrfsMaintenanceSchedule, error) {
	return GetBtrfsMaintenance(ctx)
}

func handleSetBtrfsMaintenance(ctx context.Context, req apischema.BtrfsMaintenanceRequest) (apischema.BtrfsMaintenanceSchedule, error) {
	slog.Info("setting btrfs maintenance schedule", "mountpoint", req.Mountpoint, "task", req.Task, "period", req.Period)
	schedule, err := SetBtrfsMaintenance(ctx, req)
	if err != nil {
		slog.Error("failed to set btrfs maintenance schedule", "mountpoint", req.Mountpoint, "task", req.Task, "error", err)
		return apischema.BtrfsMaintenanceSchedule{}, err
	}
	return schedule, nil
}

func handleGetDriveInfo(ctx context.Context, _ apischema.NoRequest) ([]apischema.ApiDisk, error) {
	return FetchDriveInfo(ctx)
}

func handleGetTopology(ctx context.Context, _ apischema.NoRequest) (apischema.StorageTopologyInventory, error) {
	return GetTopology(ctx)
}
