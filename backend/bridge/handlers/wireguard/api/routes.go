package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var AddInterface = routes.Job("wireguard.add_interface", apischema.TypeOf[apischema.WireGuardAddInterfaceRequest](), apischema.NoResponse())
var AddPeer = routes.Job("wireguard.add_peer", apischema.TypeOf[apischema.InterfaceNameRequest](), apischema.NoResponse())
var DisableInterface = routes.Job("wireguard.disable_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse())
var DownInterface = routes.Job("wireguard.down_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse())
var EnableInterface = routes.Job("wireguard.enable_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse())
var ListInterfaces = routes.Query("wireguard.list_interfaces", apischema.NoRequest(), apischema.TypeOf[[]apischema.WireGuardInterface]())
var ListPeers = routes.Query("wireguard.list_peers", apischema.TypeOf[apischema.InterfaceNameRequest](), apischema.TypeOf[[]apischema.Peer]())
var PeerConfigDownload = routes.Query("wireguard.peer_config_download", apischema.TypeOf[apischema.InterfaceNamePeerNameRequest](), apischema.TypeOf[apischema.PeerConfigDownload]())
var PeerQrcode = routes.Query("wireguard.peer_qrcode", apischema.TypeOf[apischema.InterfaceNamePeerNameRequest](), apischema.TypeOf[apischema.QRCodeResponse]())
var RemoveInterface = routes.Job("wireguard.remove_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse())
var RemovePeer = routes.Job("wireguard.remove_peer", apischema.TypeOf[apischema.InterfaceNamePeerNameRequest](), apischema.NoResponse())
var UpInterface = routes.Job("wireguard.up_interface", apischema.TypeOf[apischema.NameRequest](), apischema.NoResponse())

var Routes = routes.All()
