package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var DisableService = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "systemd.disable_service", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ServiceNameRequest](), Result: apischema.NoResponse()}
var EnableService = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "systemd.enable_service", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ServiceNameRequest](), Result: apischema.NoResponse()}
var GetUnitInfo = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "systemd.get_unit_info", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.UnitNameRequest](), Result: apischema.TypeOf[apischema.UnitInfo]()}
var ListServices = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "systemd.list_services", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.Service]()}
var ListSockets = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "systemd.list_sockets", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.Socket]()}
var ListTimers = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "systemd.list_timers", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.Timer]()}
var MaskService = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "systemd.mask_service", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ServiceNameRequest](), Result: apischema.NoResponse()}
var ReloadService = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "systemd.reload_service", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ServiceNameRequest](), Result: apischema.NoResponse()}
var ResetFailedService = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "systemd.reset_failed_service", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ServiceNameRequest](), Result: apischema.NoResponse()}
var RestartService = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "systemd.restart_service", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ServiceNameRequest](), Result: apischema.NoResponse()}
var StartService = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "systemd.start_service", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ServiceNameRequest](), Result: apischema.NoResponse()}
var StopService = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "systemd.stop_service", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ServiceNameRequest](), Result: apischema.NoResponse()}
var UnmaskService = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "systemd.unmask_service", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ServiceNameRequest](), Result: apischema.NoResponse()}

var Routes = []apischema.RouteSpec{
	DisableService,
	EnableService,
	GetUnitInfo,
	ListServices,
	ListSockets,
	ListTimers,
	MaskService,
	ReloadService,
	ResetFailedService,
	RestartService,
	StartService,
	StopService,
	UnmaskService,
}
