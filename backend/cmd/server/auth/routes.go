package auth

import "github.com/gin-gonic/gin"

type Config struct {
	Env                  string
	Verbose              bool
	BridgeBinaryOverride string
}

var cfg Config

// RegisterAuthRoutes wires public and private auth endpoints.
// - pub: routes without auth middleware (e.g., /auth/login)
// - priv: routes with auth middleware already attached (e.g., /auth/me, /auth/logout)
func RegisterAuthRoutes(pub *gin.RouterGroup, priv *gin.RouterGroup, c Config) {
	cfg = c
	// public
	pub.POST("/login", loginHandler)

	// private (requires middleware applied by caller)
	priv.GET("/me", meHandler)
	priv.GET("/logout", logoutHandler)
}
