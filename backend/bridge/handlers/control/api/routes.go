package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Logoff = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "control.logoff", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.SessionIDRequest](), Result: apischema.NoResponse()}
var PowerOff = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "control.power_off", Mode: bridgeipc.ModeJob, Request: apischema.NoRequest(), Result: apischema.NoResponse()}
var Reboot = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "control.reboot", Mode: bridgeipc.ModeJob, Request: apischema.NoRequest(), Result: apischema.NoResponse()}
var Version = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "control.version", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.VersionResponse]()}

var Routes = []apischema.RouteSpec{
	Logoff,
	PowerOff,
	Reboot,
	Version,
}
