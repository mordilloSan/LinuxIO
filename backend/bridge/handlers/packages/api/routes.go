package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Update = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "packages.update", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.PackageUpdateRequest](), Result: apischema.TypeOf[apischema.JobSnapshot]()}
var UpdatesApplyOfflineUpdates = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "updates.apply_offline_updates", Mode: bridgeipc.ModeJob, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.OfflineUpdatesResponse]()}
var UpdatesGetAutoUpdates = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "updates.get_auto_updates", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.AutoUpdateState]()}
var UpdatesGetUpdateDetail = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "updates.get_update_detail", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.PackageIDRequest](), Result: apischema.TypeOf[apischema.Update]()}
var UpdatesGetUpdateHistory = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "updates.get_update_history", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.UpdateHistoryRow]()}
var UpdatesGetUpdatesBasic = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "updates.get_updates_basic", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.Update]()}
var UpdatesInstallPackage = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "updates.install_package", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.PackageIDRequest](), Result: apischema.NoResponse()}
var UpdatesSetAutoUpdates = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "updates.set_auto_updates", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.UpdatesSetAutoUpdatesRequest](), Result: apischema.TypeOf[apischema.AutoUpdateState]()}

var Routes = []apischema.RouteSpec{
	Update,
	UpdatesApplyOfflineUpdates,
	UpdatesGetAutoUpdates,
	UpdatesGetUpdateDetail,
	UpdatesGetUpdateHistory,
	UpdatesGetUpdatesBasic,
	UpdatesInstallPackage,
	UpdatesSetAutoUpdates,
}
