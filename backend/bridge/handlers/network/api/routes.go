package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var DisableConnection = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "network.disable_connection", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.InterfaceRequest](), Result: apischema.NoResponse()}
var EnableConnection = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "network.enable_connection", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.InterfaceRequest](), Result: apischema.NoResponse()}
var GetNetworkInfo = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "network.get_network_info", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.NetworkInterface]()}
var SetIPv4 = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "network.set_ipv4", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.InterfaceMethodRequest](), Result: apischema.NoResponse()}
var SetIPv4Manual = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "network.set_ipv4_manual", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.IPv4ManualRequest](), Result: apischema.NoResponse()}
var SetIPv6 = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "network.set_ipv6", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.InterfaceMethodRequest](), Result: apischema.NoResponse()}
var SetMTU = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "network.set_mtu", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.InterfaceMTURequest](), Result: apischema.NoResponse()}

var Routes = []apischema.RouteSpec{
	DisableConnection,
	EnableConnection,
	GetNetworkInfo,
	SetIPv4,
	SetIPv4Manual,
	SetIPv6,
	SetMTU,
}
