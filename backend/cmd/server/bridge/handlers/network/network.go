package network

import (
	"encoding/json"

	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/cmd/bridge/handlers/dbus"
	"github.com/mordilloSan/LinuxIO/cmd/server/bridge"
	"github.com/mordilloSan/LinuxIO/internal/ipc"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
)

func getNetworkInfo(c *gin.Context) {
	sess := session.SessionFromContext(c)
	logger.Infof("%s requested network info (session: %s)", sess.User.Username, sess.SessionID)

	rawResp, err := bridge.CallWithSession(sess, "dbus", "GetNetworkInfo", nil)
	if err != nil {
		logger.Errorf("Bridge call failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed", "detail": err.Error(), "output": rawResp})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(rawResp), &resp); err != nil {
		logger.Errorf("Invalid bridge response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response", "detail": err.Error(), "output": rawResp})
		return
	}
	if resp.Status != "ok" {
		logger.Warnf("Bridge returned error: %v", resp.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error, "output": string(resp.Output)})
		return
	}

	var data []dbus.NMInterfaceInfo
	if err := json.Unmarshal(resp.Output, &data); err != nil {
		logger.Errorf("Invalid output structure: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid output structure", "detail": err.Error(), "output": string(resp.Output)})
		return
	}
	logger.Debugf("Successfully returned %d interfaces to %s", len(data), sess.User.Username)
	c.JSON(http.StatusOK, data)
}

func postSetDNS(c *gin.Context) {
	var req struct {
		Interface string   `json:"interface"`
		DNS       []string `json:"dns"`
	}
	sess := session.SessionFromContext(c)
	if err := c.BindJSON(&req); err != nil || req.Interface == "" || len(req.DNS) == 0 {
		logger.Warnf("Bad request for set-dns: %+v", req)
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	logger.Infof("%s sets DNS on %s: %v", sess.User.Username, req.Interface, req.DNS)
	err := dbus.SetDNS(req.Interface, req.DNS)
	if err != nil {
		logger.Errorf("Failed to set DNS on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	logger.Infof("Set DNS on %s to %v (user: %s, session: %s)", req.Interface, req.DNS, sess.User.Username, sess.SessionID)
	c.JSON(200, gin.H{"status": "ok"})
}

func postSetGateway(c *gin.Context) {
	var req struct {
		Interface string `json:"interface"`
		Gateway   string `json:"gateway"`
	}
	sess := session.SessionFromContext(c)
	if err := c.BindJSON(&req); err != nil || req.Interface == "" || req.Gateway == "" {
		logger.Warnf("Bad request for set-gateway: %+v", req)
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	logger.Infof("%s sets gateway on %s: %s", sess.User.Username, req.Interface, req.Gateway)
	err := dbus.SetGateway(req.Interface, req.Gateway)
	if err != nil {
		logger.Errorf("Failed to set gateway on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	logger.Infof("Set gateway on %s to %s (user: %s, session: %s)", req.Interface, req.Gateway, sess.User.Username, sess.SessionID)
	c.JSON(200, gin.H{"status": "ok"})
}

func postSetMTU(c *gin.Context) {
	var req struct {
		Interface string `json:"interface"`
		MTU       string `json:"mtu"`
	}
	sess := session.SessionFromContext(c)
	if err := c.BindJSON(&req); err != nil || req.Interface == "" || req.MTU == "" {
		logger.Warnf("Bad request for set-mtu: %+v", req)
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	logger.Infof("%s sets MTU on %s: %s", sess.User.Username, req.Interface, req.MTU)
	err := dbus.SetMTU(req.Interface, req.MTU)
	if err != nil {
		logger.Errorf("Failed to set MTU on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	logger.Infof("Set MTU on %s to %s (user: %s, session: %s)", req.Interface, req.MTU, sess.User.Username, sess.SessionID)
	c.JSON(200, gin.H{"status": "ok"})
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
	err := dbus.SetIPv4DHCP(req.Interface)
	if err != nil {
		logger.Errorf("Failed to set IPv4 DHCP on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	logger.Infof("Set IPv4 DHCP on %s (user: %s, session: %s)", req.Interface, sess.User.Username, sess.SessionID)
	c.JSON(200, gin.H{"status": "ok"})
}

func postSetIPv4Static(c *gin.Context) {
	var req struct {
		Interface   string `json:"interface"`
		AddressCIDR string `json:"address_cidr"`
	}
	sess := session.SessionFromContext(c)
	if err := c.BindJSON(&req); err != nil || req.Interface == "" || req.AddressCIDR == "" {
		logger.Warnf("Bad request for set-ipv4-static: %+v", req)
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	logger.Infof("%s sets IPv4 static on %s: %s", sess.User.Username, req.Interface, req.AddressCIDR)
	err := dbus.SetIPv4Static(req.Interface, req.AddressCIDR)
	if err != nil {
		logger.Errorf("Failed to set IPv4 static on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	logger.Infof("Set IPv4 static on %s to %s (user: %s, session: %s)", req.Interface, req.AddressCIDR, sess.User.Username, sess.SessionID)
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
	err := dbus.SetIPv6DHCP(req.Interface)
	if err != nil {
		logger.Errorf("Failed to set IPv6 DHCP on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	logger.Infof("Set IPv6 DHCP on %s (user: %s, session: %s)", req.Interface, sess.User.Username, sess.SessionID)
	c.JSON(200, gin.H{"status": "ok"})
}

func postSetIPv6Static(c *gin.Context) {
	var req struct {
		Interface   string `json:"interface"`
		AddressCIDR string `json:"address_cidr"`
	}
	sess := session.SessionFromContext(c)
	if err := c.BindJSON(&req); err != nil || req.Interface == "" || req.AddressCIDR == "" {
		logger.Warnf("Bad request for set-ipv6-static: %+v", req)
		c.JSON(400, gin.H{"error": "invalid request"})
		return
	}
	logger.Infof("%s sets IPv6 static on %s: %s", sess.User.Username, req.Interface, req.AddressCIDR)
	err := dbus.SetIPv6Static(req.Interface, req.AddressCIDR)
	if err != nil {
		logger.Errorf("Failed to set IPv6 static on %s: %v", req.Interface, err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	logger.Infof("Set IPv6 static on %s to %s (user: %s, session: %s)", req.Interface, req.AddressCIDR, sess.User.Username, sess.SessionID)
	c.JSON(200, gin.H{"status": "ok"})
}
