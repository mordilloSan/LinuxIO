package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var DisableService = routes.Job("systemd.disable_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var EnableService = routes.Job("systemd.enable_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var GetUnitInfo = routes.Query("systemd.get_unit_info", apischema.TypeOf[apischema.UnitNameRequest](), apischema.TypeOf[apischema.UnitInfo]())
var ListServices = routes.Query("systemd.list_services", apischema.NoRequest(), apischema.TypeOf[[]apischema.Service]())
var ListSockets = routes.Query("systemd.list_sockets", apischema.NoRequest(), apischema.TypeOf[[]apischema.Socket]())
var ListTimers = routes.Query("systemd.list_timers", apischema.NoRequest(), apischema.TypeOf[[]apischema.Timer]())
var MaskService = routes.Job("systemd.mask_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var ReloadService = routes.Job("systemd.reload_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var ResetFailedService = routes.Job("systemd.reset_failed_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var RestartService = routes.Job("systemd.restart_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var StartService = routes.Job("systemd.start_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var StopService = routes.Job("systemd.stop_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())
var UnmaskService = routes.Job("systemd.unmask_service", apischema.TypeOf[apischema.ServiceNameRequest](), apischema.NoResponse())

var Routes = routes.All()
