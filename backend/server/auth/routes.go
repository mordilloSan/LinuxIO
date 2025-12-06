package auth

import (
	"os/user"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

type Config struct {
	Env                  string
	Verbose              bool
	BridgeBinaryOverride string
}

// --- test seams (overridden in tests) ---
var (
	startBridge        = bridge.StartBridge
	callBridgeWithSess = bridge.CallTypedWithSession
	getBridgeBinary    = bridge.GetBridgeBinaryPath
	lookupUser         = user.Lookup
)

// RegisterAuthRoutes wires public and private auth endpoints.
// - pub: routes without auth middleware (e.g., /auth/login)
// - priv: routes with auth middleware already attached (e.g., /auth/me, /auth/logout)
func RegisterAuthRoutes(pub *gin.RouterGroup, priv *gin.RouterGroup, sm *session.Manager, c Config) {
	h := &Handlers{
		SM:                   sm,
		Env:                  c.Env,
		Verbose:              c.Verbose,
		BridgeBinaryOverride: c.BridgeBinaryOverride,
	}

	// public
	pub.POST("/login", h.Login)

	// private (requires middleware applied by caller)
	priv.GET("/me", h.Me)
	priv.GET("/logout", h.Logout)
}
