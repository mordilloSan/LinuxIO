package auth

import (
	"os/user"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

// --- test seams (overridden in tests) ---
var (
	startBridge        = bridge.StartBridge
	callBridgeWithSess = bridge.CallTypedWithSession
	getBridgeBinary    = bridge.GetBridgeBinaryPath
	lookupUser         = user.Lookup
)

// RegisterAuthRoutes wires public and private auth endpoints under /auth.
func RegisterAuthRoutes(r *gin.Engine, sm *session.Manager, env string, verbose bool, bridgeBinaryOverride string) {
	h := &Handlers{
		SM:                   sm,
		Env:                  env,
		Verbose:              verbose,
		BridgeBinaryOverride: bridgeBinaryOverride,
	}

	// public
	r.POST("/auth/login", h.Login)

	// private
	priv := r.Group("/auth", sm.RequireSession())
	priv.GET("/me", h.Me)
	priv.GET("/logout", h.Logout)
}
