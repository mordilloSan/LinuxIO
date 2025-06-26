package wireguard

import (
	"fmt"
	"go-backend/internal/auth"
	"net"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type Manager struct {
	client *wgctrl.Client
}

func NewManager() (*Manager, error) {
	cli, err := wgctrl.New()
	if err != nil {
		return nil, err
	}
	return &Manager{client: cli}, nil
}

func (m *Manager) Close() error {
	return m.client.Close()
}

// List all WireGuard interfaces (names)
func (m *Manager) ListInterfaces() ([]string, error) {
	devices, err := m.client.Devices()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(devices))
	for i, d := range devices {
		names[i] = d.Name
	}
	return names, nil
}

// Get detailed info about a specific interface
func (m *Manager) GetInterface(name string) (*wgtypes.Device, error) {
	return m.client.Device(name)
}

// Add WireGuard interface (pure Go, via netlink)
func (m *Manager) AddInterface(name string) error {
	la := netlink.NewLinkAttrs()
	la.Name = name
	wg := &netlink.Wireguard{LinkAttrs: la}
	if err := netlink.LinkAdd(wg); err != nil {
		return fmt.Errorf("failed to add WireGuard interface: %w", err)
	}
	return nil
}

// Remove WireGuard interface
func (m *Manager) RemoveInterface(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("could not find interface %q: %w", name, err)
	}
	return netlink.LinkDel(link)
}

// Bring interface up
func (m *Manager) SetInterfaceUp(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("could not find interface %q: %w", name, err)
	}
	return netlink.LinkSetUp(link)
}

// Bring interface down
func (m *Manager) SetInterfaceDown(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("could not find interface %q: %w", name, err)
	}
	return netlink.LinkSetDown(link)
}

// List peers for an interface
func (m *Manager) ListPeers(name string) ([]wgtypes.Peer, error) {
	dev, err := m.GetInterface(name)
	if err != nil {
		return nil, err
	}
	return dev.Peers, nil
}

// Add a peer to an interface
func (m *Manager) AddPeer(iface string, peer wgtypes.PeerConfig) error {
	return m.client.ConfigureDevice(iface, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{peer},
	})
}

// Remove a peer from an interface (by public key)
func (m *Manager) RemovePeer(iface string, pubKey wgtypes.Key) error {
	return m.client.ConfigureDevice(iface, wgtypes.Config{
		Peers: []wgtypes.PeerConfig{
			{
				PublicKey: pubKey,
				Remove:    true,
			},
		},
	})
}

// Show public/private keys of interface
func (m *Manager) GetKeys(iface string) (publicKey string, privateKey string, err error) {
	dev, err := m.GetInterface(iface)
	if err != nil {
		return "", "", err
	}
	return dev.PublicKey.String(), dev.PrivateKey.String(), nil
}

// Generate new WireGuard keypair
func GenerateKeypair() (privKey string, pubKey string, err error) {
	priv, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		return "", "", err
	}
	return priv.String(), priv.PublicKey().String(), nil
}

func RegisterWireGuardRoutes(router *gin.Engine) {
	manager, err := NewManager()
	if err != nil {
		panic("Could not initialize WireGuard manager: " + err.Error())
	}
	// You might want to share this manager, or recreate per-request as needed.

	api := router.Group("/wireguard", auth.AuthMiddleware())
	{
		api.GET("/interfaces", func(c *gin.Context) {
			ifaces, err := manager.ListInterfaces()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"interfaces": ifaces})
		})

		api.GET("/interface/:name", func(c *gin.Context) {
			name := c.Param("name")
			dev, err := manager.GetInterface(name)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, dev)
		})

		api.POST("/interface", func(c *gin.Context) {
			var req struct {
				Name string `json:"name"`
			}
			if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "missing or invalid interface name"})
				return
			}
			if err := manager.AddInterface(req.Name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"created": req.Name})
		})

		api.DELETE("/interface/:name", func(c *gin.Context) {
			name := c.Param("name")
			if err := manager.RemoveInterface(name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"deleted": name})
		})

		api.POST("/interface/:name/up", func(c *gin.Context) {
			name := c.Param("name")
			if err := manager.SetInterfaceUp(name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"up": name})
		})

		api.POST("/interface/:name/down", func(c *gin.Context) {
			name := c.Param("name")
			if err := manager.SetInterfaceDown(name); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"down": name})
		})

		api.GET("/interface/:name/peers", func(c *gin.Context) {
			name := c.Param("name")
			peers, err := manager.ListPeers(name)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"peers": peers})
		})

		api.POST("/interface/:name/peer", func(c *gin.Context) {
			name := c.Param("name")
			var req struct {
				PublicKey           string   `json:"public_key"`
				AllowedIPs          []string `json:"allowed_ips"`
				Endpoint            string   `json:"endpoint,omitempty"`
				PresharedKey        string   `json:"preshared_key,omitempty"`
				PersistentKeepalive int      `json:"persistent_keepalive,omitempty"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			pub, err := wgtypes.ParseKey(req.PublicKey)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid public key"})
				return
			}
			var allowedIPs []net.IPNet
			for _, cidr := range req.AllowedIPs {
				_, ipnet, err := net.ParseCIDR(cidr)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid allowed_ip: " + cidr})
					return
				}
				allowedIPs = append(allowedIPs, *ipnet)
			}
			peerConf := wgtypes.PeerConfig{
				PublicKey:  pub,
				AllowedIPs: allowedIPs,
			}
			if req.Endpoint != "" {
				endpoint, err := net.ResolveUDPAddr("udp", req.Endpoint)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid endpoint"})
					return
				}
				peerConf.Endpoint = endpoint
			}
			if req.PresharedKey != "" {
				psk, err := wgtypes.ParseKey(req.PresharedKey)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid preshared key"})
					return
				}
				peerConf.PresharedKey = &psk
			}
			if req.PersistentKeepalive > 0 {
				dur := time.Duration(req.PersistentKeepalive) * time.Second
				peerConf.PersistentKeepaliveInterval = &dur
			}
			if err := manager.AddPeer(name, peerConf); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"added": req.PublicKey})
		})

		api.DELETE("/interface/:name/peer/:pubkey", func(c *gin.Context) {
			name := c.Param("name")
			pubkey := c.Param("pubkey")
			key, err := wgtypes.ParseKey(pubkey)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid public key"})
				return
			}
			if err := manager.RemovePeer(name, key); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"removed": pubkey})
		})

		api.GET("/interface/:name/keys", func(c *gin.Context) {
			name := c.Param("name")
			pub, priv, err := manager.GetKeys(name)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"public_key": pub, "private_key": priv})
		})

		api.GET("/keygen", func(c *gin.Context) {
			priv, pub, err := GenerateKeypair()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"private_key": priv, "public_key": pub})
		})
	}
}
