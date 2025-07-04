package wireguard

import (
	"encoding/json"
	"go-backend/cmd/bridge/handlers/types"
	"go-backend/internal/auth"
	"go-backend/internal/bridge"
	"go-backend/internal/utils"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// Always: Unmarshal into types.BridgeResponse, then unmarshal Output.

func WireguardListInterfaces(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	data, err := bridge.CallWithSession(sess, "wireguard", "list_interfaces", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp types.BridgeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
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
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	var req struct {
		Name       string             `json:"name"`
		Address    []string           `json:"address"`
		ListenPort int                `json:"listen_port"`
		EgressNic  string             `json:"egress_nic"`
		DNS        []string           `json:"dns"`
		MTU        int                `json:"mtu"`
		Peers      []utils.PeerConfig `json:"peers"`
		NumPeers   int                `json:"num_peers"`
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
	var resp types.BridgeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}

	var out map[string]interface{}
	if err := json.Unmarshal(resp.Output, &out); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge output"})
		return
	}
	c.JSON(http.StatusOK, out)
}

func WireguardRemoveInterface(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	name := c.Param("name")
	data, err := bridge.CallWithSession(sess, "wireguard", "remove_interface", []string{name})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp types.BridgeResponse
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
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	name := c.Param("name")
	data, err := bridge.CallWithSession(sess, "wireguard", "list_peers", []string{name})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp types.BridgeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
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
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	var req struct {
		PublicKey           string   `json:"public_key"`
		AllowedIPs          []string `json:"allowed_ips"`
		Endpoint            string   `json:"endpoint"`
		PresharedKey        string   `json:"preshared_key"`
		PersistentKeepalive int      `json:"persistent_keepalive"`
		Name                string   `json:"name"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid input"})
		return
	}
	name := c.Param("name")
	args := []string{
		name,
		req.PublicKey,
		strings.Join(req.AllowedIPs, ","),
		req.Endpoint,
		req.PresharedKey,
		strconv.Itoa(req.PersistentKeepalive),
		req.Name, // Pass the peer's display name/label (may be "")
	}
	data, err := bridge.CallWithSession(sess, "wireguard", "add_peer", args)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp types.BridgeResponse
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

func WireguardRemovePeer(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	name := c.Param("name")
	pubkey := c.Param("pubkey")
	args := []string{name, pubkey}
	data, err := bridge.CallWithSession(sess, "wireguard", "remove_peer", args)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp types.BridgeResponse
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

func WireguardGetKeys(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	name := c.Param("name")
	data, err := bridge.CallWithSession(sess, "wireguard", "get_keys", []string{name})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp types.BridgeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}
	var out map[string]string
	if err := json.Unmarshal(resp.Output, &out); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge output"})
		return
	}
	c.JSON(http.StatusOK, out)
}

// PUT /wireguard/interface/:name/up
func WireguardUpInterface(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	name := c.Param("name")
	data, err := bridge.CallWithSession(sess, "wireguard", "up_interface", []string{name})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var resp types.BridgeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(500, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(500, gin.H{"error": resp.Error})
		return
	}
	var out map[string]interface{}
	if err := json.Unmarshal(resp.Output, &out); err != nil {
		c.JSON(500, gin.H{"error": "invalid bridge output"})
		return
	}
	c.JSON(200, out)
}

// PUT /wireguard/interface/:name/down
func WireguardDownInterface(c *gin.Context) {
	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	name := c.Param("name")
	data, err := bridge.CallWithSession(sess, "wireguard", "down_interface", []string{name})
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	var resp types.BridgeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(500, gin.H{"error": "invalid bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(500, gin.H{"error": resp.Error})
		return
	}
	var out map[string]interface{}
	if err := json.Unmarshal(resp.Output, &out); err != nil {
		c.JSON(500, gin.H{"error": "invalid bridge output"})
		return
	}
	c.JSON(200, out)
}

func RegisterWireguardRoutes(r *gin.Engine) {
	wg := r.Group("/wireguard")
	wg.Use(auth.AuthMiddleware())
	wg.GET("/interfaces", WireguardListInterfaces)
	wg.POST("/interface", WireguardAddInterface)
	wg.DELETE("/interface/:name", WireguardRemoveInterface)
	wg.GET("/interface/:name/peers", WireguardListPeers)
	wg.POST("/interface/:name/peer", WireguardAddPeer)
	wg.DELETE("/interface/:name/peer/:pubkey", WireguardRemovePeer)
	wg.GET("/interface/:name/keys", WireguardGetKeys)
	wg.POST("/interface/:name/up", WireguardUpInterface)
	wg.POST("/interface/:name/down", WireguardDownInterface)
}
