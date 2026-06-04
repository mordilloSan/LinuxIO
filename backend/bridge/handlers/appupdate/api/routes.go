package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var ControlAppUpdate = apischema.RouteSpec{Kind: apischema.KindRunner, Route: "control.app_update", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.AppUpdateRequest](), Result: apischema.NoResponse(), NoEndpoint: true}

var Routes = []apischema.RouteSpec{
	ControlAppUpdate,
}
