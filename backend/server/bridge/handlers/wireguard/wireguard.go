package wireguard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/wireguard"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

func WireguardListInterfaces(c *gin.Context) {
	sess := session.SessionFromContext(c)
	data, err := bridge.CallWithSession(sess, "wireguard", "list_interfaces", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
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

	var out []map[string]interface{}
	if err := json.Unmarshal(resp.Output, &out); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge output"})
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

	data, err := bridge.CallWithSession(sess, "wireguard", "add_interface", args)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
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

func WireguardRemoveInterface(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	data, err := bridge.CallWithSession(sess, "wireguard", "remove_interface", []string{name})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func WireguardListPeers(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	data, err := bridge.CallWithSession(sess, "wireguard", "list_peers", []string{name})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
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

	var out []map[string]interface{}
	if err := json.Unmarshal(resp.Output, &out); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge output"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"peers": out})
}

func WireguardAddPeer(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	args := []string{name}
	data, err := bridge.CallWithSession(sess, "wireguard", "add_peer", args)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "peer": resp.Output})
}

func WireguardRemovePeer(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	peername := c.Param("peername")
	args := []string{name, peername}
	data, err := bridge.CallWithSession(sess, "wireguard", "remove_peer", args)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func WireguardPeerQRCode(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	peername := c.Param("peername")
	args := []string{name, peername}
	data, err := bridge.CallWithSession(sess, "wireguard", "peer_qrcode", args)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
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

	var out map[string]string
	if err := json.Unmarshal(resp.Output, &out); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge output"})
		return
	}
	c.JSON(http.StatusOK, out)
}

func WireguardPeerConfigDownload(c *gin.Context) {
	sess := session.SessionFromContext(c)
	interfaceName := c.Param("name")
	peerName := c.Param("peername")
	args := []string{interfaceName, peerName}
	data, err := bridge.CallWithSession(sess, "wireguard", "peer_config_download", args)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
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

	var out map[string]string
	if err := json.Unmarshal(resp.Output, &out); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge output"})
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
	data, err := bridge.CallWithSession(sess, "wireguard", "get_keys", []string{name})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
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

func WireguardUpInterface(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	data, err := bridge.CallWithSession(sess, "wireguard", "up_interface", []string{name})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(500, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(500, gin.H{"error": resp.Error})
		return
	}
	if resp.Output == nil {
		c.JSON(500, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(200, resp.Output)
}

func WireguardDownInterface(c *gin.Context) {
	sess := session.SessionFromContext(c)
	name := c.Param("name")
	data, err := bridge.CallWithSession(sess, "wireguard", "down_interface", []string{name})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(500, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(500, gin.H{"error": resp.Error})
		return
	}
	if resp.Output == nil {
		c.JSON(500, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(200, resp.Output)
}
