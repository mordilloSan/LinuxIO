package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var AddInterface = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "wireguard.add_interface", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.WireGuardAddInterfaceRequest](), Result: apischema.NoResponse()}
var AddPeer = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "wireguard.add_peer", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.InterfaceNameRequest](), Result: apischema.NoResponse()}
var DisableInterface = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "wireguard.disable_interface", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.NameRequest](), Result: apischema.NoResponse()}
var DownInterface = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "wireguard.down_interface", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.NameRequest](), Result: apischema.NoResponse()}
var EnableInterface = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "wireguard.enable_interface", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.NameRequest](), Result: apischema.NoResponse()}
var ListInterfaces = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "wireguard.list_interfaces", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.WireGuardInterface]()}
var ListPeers = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "wireguard.list_peers", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.InterfaceNameRequest](), Result: apischema.TypeOf[[]apischema.Peer]()}
var PeerConfigDownload = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "wireguard.peer_config_download", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.InterfaceNamePeerNameRequest](), Result: apischema.TypeOf[apischema.PeerConfigDownload]()}
var PeerQrcode = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "wireguard.peer_qrcode", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.InterfaceNamePeerNameRequest](), Result: apischema.TypeOf[apischema.QRCodeResponse]()}
var RemoveInterface = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "wireguard.remove_interface", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.NameRequest](), Result: apischema.NoResponse()}
var RemovePeer = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "wireguard.remove_peer", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.InterfaceNamePeerNameRequest](), Result: apischema.NoResponse()}
var UpInterface = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "wireguard.up_interface", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.NameRequest](), Result: apischema.NoResponse()}

var Routes = []apischema.RouteSpec{
	AddInterface,
	AddPeer,
	DisableInterface,
	DownInterface,
	EnableInterface,
	ListInterfaces,
	ListPeers,
	PeerConfigDownload,
	PeerQrcode,
	RemoveInterface,
	RemovePeer,
	UpInterface,
}
