package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var DisableConnection = routes.Job("network.disable_connection", apischema.TypeOf[apischema.InterfaceRequest](), apischema.NoResponse())
var EnableConnection = routes.Job("network.enable_connection", apischema.TypeOf[apischema.InterfaceRequest](), apischema.NoResponse())
var GetNetworkInfo = routes.Query("network.get_network_info", apischema.NoRequest(), apischema.TypeOf[[]apischema.NetworkInterface]())
var SetIPv4 = routes.Job("network.set_ipv4", apischema.TypeOf[apischema.InterfaceMethodRequest](), apischema.NoResponse())
var SetIPv4Manual = routes.Job("network.set_ipv4_manual", apischema.TypeOf[apischema.IPv4ManualRequest](), apischema.NoResponse())
var SetIPv6 = routes.Job("network.set_ipv6", apischema.TypeOf[apischema.InterfaceMethodRequest](), apischema.NoResponse())
var SetMTU = routes.Job("network.set_mtu", apischema.TypeOf[apischema.InterfaceMTURequest](), apischema.NoResponse())

var Routes = routes.All()
