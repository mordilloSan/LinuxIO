package network

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/common/ipc"
	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
	"github.com/mordilloSan/LinuxIO/server/bridge"
)

func getNetworkInfo(c *gin.Context) {
	sess := session.SessionFromContext(c)
	logger.Infof("%s requested network info (session: %s)", sess.User.Username, sess.SessionID)

	rawResp, err := bridge.CallWithSession(sess, "dbus", "GetNetworkInfo", nil)
	if err != nil {
		logger.Errorf("Bridge call failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed", "detail": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(rawResp), &resp); err != nil {
		logger.Errorf("Invalid bridge response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response", "detail": err.Error()})
		return
	}
	if resp.Status != "ok" {
		logger.Warnf("Bridge returned error: %v", resp.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	logger.Debugf("Successfully returned network interfaces to %s", sess.User.Username)
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

	logger.Infof("%s requests IPv4 DHCP on %s", sess.User.Username, req.Interface)

	// Call through bridge: group "dbus", cmd "SetIPv4", args [iface, "dhcp"]
	raw, err := bridge.CallWithSession(sess, "dbus", "SetIPv4",
		[]string{req.Interface, "dhcp"})
	if err != nil {
		logger.Errorf("Bridge call failed for SetIPv4 DHCP on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		logger.Errorf("Invalid bridge response for SetIPv4 DHCP on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		logger.Warnf("Bridge returned error for SetIPv4 DHCP on %s: %s", req.Interface, resp.Error)
		c.JSON(500, gin.H{"error": resp.Error})
		return
	}

	logger.Infof("Set IPv4 DHCP on %s (user: %s, session: %s)", req.Interface, sess.User.Username, sess.SessionID)
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

	logger.Infof("%s sets manual IPv4 config on %s: IP=%s, Gateway=%s, DNS=%v",
		sess.User.Username, req.Interface, req.AddressCIDR, req.Gateway, req.DNS)

	// Build args: [interface, addressCIDR, gateway, dns1, dns2, ...]
	args := []string{req.Interface, req.AddressCIDR, req.Gateway}
	args = append(args, req.DNS...)

	// Call through bridge for proper privileges
	raw, err := bridge.CallWithSession(sess, "dbus", "SetIPv4Manual", args)
	if err != nil {
		logger.Errorf("Bridge call failed for SetIPv4Manual on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		logger.Errorf("Invalid bridge response for SetIPv4Manual on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		logger.Warnf("Bridge returned error for SetIPv4Manual on %s: %s", req.Interface, resp.Error)
		c.JSON(500, gin.H{"error": resp.Error})
		return
	}

	logger.Infof("Set manual IPv4 config on %s (user: %s, session: %s)",
		req.Interface, sess.User.Username, sess.SessionID)
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

	logger.Infof("%s requests IPv6 DHCP on %s", sess.User.Username, req.Interface)

	// Call through bridge: group "dbus", cmd "SetIPv6", args [iface, "dhcp"]
	raw, err := bridge.CallWithSession(sess, "dbus", "SetIPv6",
		[]string{req.Interface, "dhcp"})
	if err != nil {
		logger.Errorf("Bridge call failed for SetIPv6 DHCP on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		logger.Errorf("Invalid bridge response for SetIPv6 DHCP on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		logger.Warnf("Bridge returned error for SetIPv6 DHCP on %s: %s", req.Interface, resp.Error)
		c.JSON(500, gin.H{"error": resp.Error})
		return
	}

	logger.Infof("Set IPv6 DHCP on %s (user: %s, session: %s)", req.Interface, sess.User.Username, sess.SessionID)
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

	logger.Infof("%s sets IPv6 static on %s: %s", sess.User.Username, req.Interface, req.AddressCIDR)

	// Call through bridge: group "dbus", cmd "SetIPv6", args [iface, "static", cidr]
	raw, err := bridge.CallWithSession(sess, "dbus", "SetIPv6",
		[]string{req.Interface, "static", req.AddressCIDR})
	if err != nil {
		logger.Errorf("Bridge call failed for SetIPv6 static on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		logger.Errorf("Invalid bridge response for SetIPv6 static on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		logger.Warnf("Bridge returned error for SetIPv6 static on %s: %s", req.Interface, resp.Error)
		c.JSON(500, gin.H{"error": resp.Error})
		return
	}

	logger.Infof("Set IPv6 static on %s to %s (user: %s, session: %s)", req.Interface, req.AddressCIDR, sess.User.Username, sess.SessionID)
	c.JSON(200, gin.H{"status": "ok"})
}
