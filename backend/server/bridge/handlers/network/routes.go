package network

import "github.com/gin-gonic/gin"

// RegisterNetworkRoutes mounts all /network endpoints on the given (already-authenticated) group.
func RegisterNetworkRoutes(network *gin.RouterGroup) {
	// Get network interface information
	network.GET("/info", getNetworkInfo)

	// IPv4 configuration
	network.POST("/set-ipv4-dhcp", postSetIPv4DHCP)     // Switch to DHCP (auto)
	network.POST("/set-ipv4-manual", postSetIPv4Manual) // Set static IP with DNS and Gateway

	// IPv6 configuration
	network.POST("/set-ipv6-dhcp", postSetIPv6DHCP)     // Switch to IPv6 DHCP
	network.POST("/set-ipv6-manual", postSetIPv6Manual) // Set static IPv6
}
