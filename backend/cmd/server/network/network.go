package network

import (
	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/cmd/server/auth"
)

func RegisterNetworkRoutes(router *gin.Engine) {
	network := router.Group("/network", auth.AuthMiddleware())
	{
		network.GET("/info", getSimpleNetInfoHandler)
		network.GET("/info2", getNetworkInfo2)
		network.POST("/set-dns", postSetDNS)
		network.POST("/set-gateway", postSetGateway)
		network.POST("/set-mtu", postSetMTU)
		network.POST("/set-ipv4-dhcp", postSetIPv4DHCP)
		network.POST("/set-ipv4-static", postSetIPv4Static)
		network.POST("/set-ipv6-dhcp", postSetIPv6DHCP)
		network.POST("/set-ipv6-static", postSetIPv6Static)
	}
}
