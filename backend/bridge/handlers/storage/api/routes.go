package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var CreateBtrfsSubvolume = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "storage.create_btrfs_subvolume", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.MountpointNameRequest](), Result: apischema.TypeOf[apischema.StoragePathResult]()}
var CreateLv = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "storage.create_lv", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.CreateLogicalVolumeRequest](), Result: apischema.TypeOf[apischema.StorageCreateLVResult]()}
var DeleteLv = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "storage.delete_lv", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.VolumeGroupLogicalVolumeRequest](), Result: apischema.TypeOf[apischema.SuccessResponse]()}
var GetDriveInfo = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "storage.get_drive_info", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.ApiDisk]()}
var ListLVs = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "storage.list_lvs", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.LogicalVolume]()}
var ListNFSExports = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "storage.list_nfs_exports", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.ServerRequest](), Result: apischema.TypeOf[[]string]()}
var ListNFSMounts = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "storage.list_nfs_mounts", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.NFSMount]()}
var ListPVs = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "storage.list_pvs", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.PhysicalVolume]()}
var ListVGs = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "storage.list_vgs", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.VolumeGroup]()}
var MountNFS = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "storage.mount_nfs", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ServerExportMountOptionsPersistRequest](), Result: apischema.TypeOf[apischema.StorageMountResult]()}
var RemountNFS = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "storage.remount_nfs", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.MountpointOptionsUpdateFstabRequest](), Result: apischema.TypeOf[apischema.StorageMountResult]()}
var ResizeLv = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "storage.resize_lv", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ResizeLogicalVolumeRequest](), Result: apischema.TypeOf[apischema.SuccessResponse]()}
var RunSmartTest = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "storage.run_smart_test", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.DeviceTestTypeRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var UnmountFilesystem = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "storage.unmount_filesystem", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.MountpointRequest](), Result: apischema.TypeOf[apischema.StorageMountResult]()}
var UnmountNFS = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "storage.unmount_nfs", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.MountpointRemoveFstabRequest](), Result: apischema.TypeOf[apischema.StorageWarningResult]()}

var Routes = []apischema.RouteSpec{
	CreateBtrfsSubvolume,
	CreateLv,
	DeleteLv,
	GetDriveInfo,
	ListLVs,
	ListNFSExports,
	ListNFSMounts,
	ListPVs,
	ListVGs,
	MountNFS,
	RemountNFS,
	ResizeLv,
	RunSmartTest,
	UnmountFilesystem,
	UnmountNFS,
}
