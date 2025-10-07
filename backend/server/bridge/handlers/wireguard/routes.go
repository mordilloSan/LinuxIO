package wireguard

import "github.com/gin-gonic/gin"

func RegisterWireguardRoutes(wireguard *gin.RouterGroup) {
	wireguard.GET("/interfaces", WireguardListInterfaces)
	wireguard.POST("/interface", WireguardAddInterface)
	wireguard.DELETE("/interface/:name", WireguardRemoveInterface)
	wireguard.GET("/interface/:name/peers", WireguardListPeers)
	wireguard.POST("/interface/:name/peer", WireguardAddPeer)
	wireguard.DELETE("/interface/:name/peer/:peername", WireguardRemovePeer)
	wireguard.GET("/interface/:name/peer/:peername/qrcode", WireguardPeerQRCode)
	wireguard.GET("/interface/:name/peer/:peername/config", WireguardPeerConfigDownload)
	wireguard.GET("/interface/:name/keys", WireguardGetKeys)
	wireguard.POST("/interface/:name/up", WireguardUpInterface)
	wireguard.POST("/interface/:name/down", WireguardDownInterface)
}
