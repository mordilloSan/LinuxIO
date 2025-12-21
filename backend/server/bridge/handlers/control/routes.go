package control

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

// RegisterControlRoutes mounts all /control endpoints on the given (already-authenticated) group.
func RegisterControlRoutes(control *gin.RouterGroup) {
	control.GET("/version", GetVersion) // Version check (read-only, used by Footer)
}

// GetVersion retrieves the current version info (no privileges required)
func GetVersion(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no active session"})
		return
	}

	// Call bridge to get version info (no args needed)
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "control", "version", []string{}, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("version check failed: %v", err)})
		return
	}

	c.Data(http.StatusOK, "application/json", result)
}
