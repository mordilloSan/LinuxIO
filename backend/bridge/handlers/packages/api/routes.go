package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var Update = routes.Runner("packages.update", apischema.TypeOf[apischema.PackageUpdateRequest](), apischema.TypeOf[apischema.JobSnapshot]())
var UpdatesApplyOfflineUpdates = routes.Job("updates.apply_offline_updates", apischema.NoRequest(), apischema.TypeOf[apischema.OfflineUpdatesResponse]())
var UpdatesGetAutoUpdates = routes.Query("updates.get_auto_updates", apischema.NoRequest(), apischema.TypeOf[apischema.AutoUpdateState]())
var UpdatesGetUpdateDetail = routes.Query("updates.get_update_detail", apischema.TypeOf[apischema.PackageIDRequest](), apischema.TypeOf[apischema.Update]())
var UpdatesGetUpdateHistory = routes.Query("updates.get_update_history", apischema.NoRequest(), apischema.TypeOf[[]apischema.UpdateHistoryRow]())
var UpdatesGetUpdatesBasic = routes.Query("updates.get_updates_basic", apischema.NoRequest(), apischema.TypeOf[[]apischema.Update]())
var UpdatesInstallPackage = routes.Job("updates.install_package", apischema.TypeOf[apischema.PackageIDRequest](), apischema.NoResponse())
var UpdatesSetAutoUpdates = routes.Job("updates.set_auto_updates", apischema.TypeOf[apischema.UpdatesSetAutoUpdatesRequest](), apischema.TypeOf[apischema.AutoUpdateState]())

var Routes = routes.All()
