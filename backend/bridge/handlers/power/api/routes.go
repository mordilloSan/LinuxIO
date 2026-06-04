package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Disable = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "power.disable", Privileged: true, Mode: bridgeipc.ModeJob, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.PowerStatus]()}
var GetStatus = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "power.get_status", Privileged: true, Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.PowerStatus]()}
var SetProfile = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "power.set_profile", Privileged: true, Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ProfileRequest](), Result: apischema.TypeOf[apischema.PowerStatus]()}
var Start = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "power.start", Privileged: true, Mode: bridgeipc.ModeJob, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.PowerStatus]()}

var Routes = []apischema.RouteSpec{
	Disable,
	GetStatus,
	SetProfile,
	Start,
}
