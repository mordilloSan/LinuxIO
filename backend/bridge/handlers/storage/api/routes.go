package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var CreateBtrfsSubvolume = routes.Job("storage.create_btrfs_subvolume", apischema.TypeOf[apischema.MountpointNameRequest](), apischema.TypeOf[apischema.StoragePathResult]())
var CreateLv = routes.Job("storage.create_lv", apischema.TypeOf[apischema.CreateLogicalVolumeRequest](), apischema.TypeOf[apischema.StorageCreateLVResult]())
var DeleteLv = routes.Job("storage.delete_lv", apischema.TypeOf[apischema.VolumeGroupLogicalVolumeRequest](), apischema.TypeOf[apischema.SuccessResponse]())
var GetDriveInfo = routes.Query("storage.get_drive_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.ApiDisk]())
var ListLVs = routes.Query("storage.list_lvs", apischema.NoRequest(), apischema.TypeOf[[]apischema.LogicalVolume]())
var ListNFSExports = routes.Query("storage.list_nfs_exports", apischema.TypeOf[apischema.ServerRequest](), apischema.TypeOf[[]string]())
var ListNFSMounts = routes.Query("storage.list_nfs_mounts", apischema.NoRequest(), apischema.TypeOf[[]apischema.NFSMount]())
var ListPVs = routes.Query("storage.list_pvs", apischema.NoRequest(), apischema.TypeOf[[]apischema.PhysicalVolume]())
var ListVGs = routes.Query("storage.list_vgs", apischema.NoRequest(), apischema.TypeOf[[]apischema.VolumeGroup]())
var MountNFS = routes.Job("storage.mount_nfs", apischema.TypeOf[apischema.ServerExportMountOptionsPersistRequest](), apischema.TypeOf[apischema.StorageMountResult]())
var RemountNFS = routes.Job("storage.remount_nfs", apischema.TypeOf[apischema.MountpointOptionsUpdateFstabRequest](), apischema.TypeOf[apischema.StorageMountResult]())
var ResizeLv = routes.Job("storage.resize_lv", apischema.TypeOf[apischema.ResizeLogicalVolumeRequest](), apischema.TypeOf[apischema.SuccessResponse]())
var RunSmartTest = routes.Runner("storage.run_smart_test", apischema.TypeOf[apischema.DeviceTestTypeRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var UnmountFilesystem = routes.Job("storage.unmount_filesystem", apischema.TypeOf[apischema.MountpointRequest](), apischema.TypeOf[apischema.StorageMountResult]())
var UnmountNFS = routes.Job("storage.unmount_nfs", apischema.TypeOf[apischema.MountpointRemoveFstabRequest](), apischema.TypeOf[apischema.StorageWarningResult]())

var Routes = routes.All()
