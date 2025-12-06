package wireguard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/wireguard"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

func WireguardListInterfaces(c *gin.Context) {
	sess := session.SessionFromContext(c)
	var out []map[string]interface{}
	if err := bridge.CallTypedWithSession(sess, "wireguard", "list_interfaces", nil, &out); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"interfaces": out})
}

func WireguardAddInterface(c *gin.Context) {
	sess := session.SessionFromContext(c)
	var req struct {
		Name       string                 `json:"name"`
		Address    []string               `json:"address"`
		ListenPort int                    `json:"listen_port"`
		EgressNic  string                 `json:"egress_nic"`
		DNS        []string               `json:"dns"`
		MTU        int                    `json:"mtu"`
		Peers      []wireguard.PeerConfig `json:"peers"`
		NumPeers   int                    `json:"num_peers"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input"})
		return
	}

	peersJSON, err := json.Marshal(req.Peers)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid peers"})
		return
	}

	args := []string{
		req.Name,
		strings.Join(req.Address, ","),
		strconv.Itoa(req.ListenPort),
		req.EgressNic,
		strings.Join(req.DNS, ","),
		strconv.Itoa(req.MTU),
		string(peersJSON),
		strconv.Itoa(req.NumPeers),
	}

	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "wireguard", "add_interface", args, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

func WireguardRemoveInterface(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	if err := bridge.CallTypedWithSession(sess, "wireguard", "remove_interface", []string{name}, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func WireguardListPeers(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	var out []map[string]interface{}
	if err := bridge.CallTypedWithSession(sess, "wireguard", "list_peers", []string{name}, &out); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"peers": out})
}

func WireguardAddPeer(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	args := []string{name}
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "wireguard", "add_peer", args, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "peer": result})
}

func WireguardRemovePeer(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	peername := c.Param("peername")
	args := []string{name, peername}
	if err := bridge.CallTypedWithSession(sess, "wireguard", "remove_peer", args, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func WireguardPeerQRCode(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	peername := c.Param("peername")
	args := []string{name, peername}
	var out map[string]string
	if err := bridge.CallTypedWithSession(sess, "wireguard", "peer_qrcode", args, &out); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, out)
}

func WireguardPeerConfigDownload(c *gin.Context) {
	sess := session.SessionFromContext(c)
	interfaceName := c.Param("name")
	peerName := c.Param("peername")
	args := []string{interfaceName, peerName}
	var out map[string]string
	if err := bridge.CallTypedWithSession(sess, "wireguard", "peer_config_download", args, &out); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	configContent := out["config"]

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s.conf\"", peerName))
	c.Header("Content-Type", "text/plain")
	c.String(http.StatusOK, configContent)
}

func WireguardGetKeys(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "wireguard", "get_keys", []string{name}, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

func WireguardUpInterface(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "wireguard", "up_interface", []string{name}, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

func WireguardDownInterface(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "wireguard", "down_interface", []string{name}, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}
