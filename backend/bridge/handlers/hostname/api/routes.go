package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var SetHostname = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "hostname.set_hostname", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.HostnameRequest](), Result: apischema.NoResponse()}

var Routes = []apischema.RouteSpec{
	SetHostname,
}
