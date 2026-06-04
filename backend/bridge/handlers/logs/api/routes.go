package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var GeneralFollow = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "logs.general.follow", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.GeneralLogsFollowRequest](), Result: apischema.NoResponse(), NoEndpoint: true}
var ServiceFollow = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "logs.service.follow", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ServiceLogsFollowRequest](), Result: apischema.NoResponse(), NoEndpoint: true}

var Routes = []apischema.RouteSpec{
	GeneralFollow,
	ServiceFollow,
}
