package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var Get = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "config.get", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[apischema.AppConfig]()}
var Set = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "config.set", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ConfigSetPayload](), Result: apischema.TypeOf[apischema.ConfigSetResult]()}

var Routes = []apischema.RouteSpec{
	Get,
	Set,
}
