package network

import "github.com/gin-gonic/gin"

// RegisterNetworkRoutes mounts all /network endpoints on the given (already-authenticated) group.
func RegisterNetworkRoutes(network *gin.RouterGroup) {
	network.GET("/info", getNetworkInfo)
	network.POST("/set-dns", postSetDNS)
	network.POST("/set-gateway", postSetGateway)
	network.POST("/set-mtu", postSetMTU)
	network.POST("/set-ipv4-dhcp", postSetIPv4DHCP)
	network.POST("/set-ipv4-static", postSetIPv4Static)
	network.POST("/set-ipv6-dhcp", postSetIPv6DHCP)
	network.POST("/set-ipv6-static", postSetIPv6Static)
}
