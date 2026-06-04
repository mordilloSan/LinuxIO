package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var ContainerOpen = apischema.RouteSpec{Kind: apischema.KindDuplex, Route: "container.open", Mode: bridgeipc.ModeDuplex, Request: apischema.TypeOf[apischema.ContainerOpenRequest](), Result: apischema.NoResponse(), NoEndpoint: true}
var ListShells = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "terminal.list_shells", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.ContainerIDRequest](), Result: apischema.TypeOf[[]string]()}
var Open = apischema.RouteSpec{Kind: apischema.KindDuplex, Route: "terminal.open", Mode: bridgeipc.ModeDuplex, Request: apischema.TypeOf[apischema.TerminalOpenRequest](), Result: apischema.NoResponse(), NoEndpoint: true}

var Routes = []apischema.RouteSpec{
	ContainerOpen,
	ListShells,
	Open,
}
