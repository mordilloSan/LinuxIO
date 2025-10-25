package network

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
	"github.com/mordilloSan/go_logger/logger"
)

func getNetworkInfo(c *gin.Context) {
	sess := session.SessionFromContext(c)

	rawResp, err := bridge.CallWithSession(sess, "dbus", "GetNetworkInfo", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed", "detail": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(rawResp), &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response", "detail": err.Error()})
		return
	}
	if resp.Status != "ok" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp.Output)
}

func postSetIPv4DHCP(c *gin.Context) {
	var req struct {
		Interface string `json:"interface"`
	}
	sess := session.SessionFromContext(c)
	if err := c.BindJSON(&req); err != nil || req.Interface == "" {
		logger.Warnf("Bad request for set-ipv4-dhcp: %+v", req)
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}

	// Call through bridge: group "dbus", cmd "SetIPv4", args [iface, "dhcp"]
	raw, err := bridge.CallWithSession(sess, "dbus", "SetIPv4",
		[]string{req.Interface, "dhcp"})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		c.JSON(500, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(500, gin.H{"error": resp.Error})
		return
	}

	c.JSON(200, gin.H{"status": "ok"})
}

func postSetIPv4Manual(c *gin.Context) {
	var req struct {
		Interface   string   `json:"interface"`
		AddressCIDR string   `json:"address_cidr"`
		Gateway     string   `json:"gateway"`
		DNS         []string `json:"dns"`
	}
	sess := session.SessionFromContext(c)

	if err := c.BindJSON(&req); err != nil {
		logger.Warnf("Bad request for set-ipv4-manual: %v", err)
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}

	// Validate required fields
	if req.Interface == "" {
		c.JSON(400, gin.H{"error": "interface is required"})
		return
	}
	if req.AddressCIDR == "" {
		c.JSON(400, gin.H{"error": "IP address is required"})
		return
	}
	if req.Gateway == "" {
		c.JSON(400, gin.H{"error": "gateway is required"})
		return
	}
	if len(req.DNS) == 0 {
		c.JSON(400, gin.H{"error": "at least one DNS server is required"})
		return
	}

	// Build args: [interface, addressCIDR, gateway, dns1, dns2, ...]
	args := []string{req.Interface, req.AddressCIDR, req.Gateway}
	args = append(args, req.DNS...)

	// Call through bridge for proper privileges
	raw, err := bridge.CallWithSession(sess, "dbus", "SetIPv4Manual", args)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		c.JSON(500, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(500, gin.H{"error": resp.Error})
		return
	}

	c.JSON(200, gin.H{"status": "ok"})
}

func postSetIPv6DHCP(c *gin.Context) {
	var req struct {
		Interface string `json:"interface"`
	}
	sess := session.SessionFromContext(c)
	if err := c.BindJSON(&req); err != nil || req.Interface == "" {
		logger.Warnf("Bad request for set-ipv6-dhcp: %+v", req)
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}

	// Call through bridge: group "dbus", cmd "SetIPv6", args [iface, "dhcp"]
	raw, err := bridge.CallWithSession(sess, "dbus", "SetIPv6",
		[]string{req.Interface, "dhcp"})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		c.JSON(500, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(500, gin.H{"error": resp.Error})
		return
	}

	c.JSON(200, gin.H{"status": "ok"})
}

func postSetIPv6Manual(c *gin.Context) {
	var req struct {
		Interface   string `json:"interface"`
		AddressCIDR string `json:"address_cidr"`
	}
	sess := session.SessionFromContext(c)
	if err := c.BindJSON(&req); err != nil || req.Interface == "" || req.AddressCIDR == "" {
		logger.Warnf("Bad request for set-ipv6-manual: %+v", req)
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}

	// Call through bridge: group "dbus", cmd "SetIPv6", args [iface, "static", cidr]
	raw, err := bridge.CallWithSession(sess, "dbus", "SetIPv6",
		[]string{req.Interface, "static", req.AddressCIDR})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		c.JSON(500, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(500, gin.H{"error": resp.Error})
		return
	}

	c.JSON(200, gin.H{"status": "ok"})
}
