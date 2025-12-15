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
	control.GET("/version", GetVersion)    // New: check version (read-only)
	control.POST("/update", TriggerUpdate) // Existing: trigger update
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

	// DEBUG: Log what we're sending
	fmt.Printf("[DEBUG GetVersion] Sending response: %s\n", string(result))
	c.Data(http.StatusOK, "application/json", result)
}

// TriggerUpdate executes the update via bridge control (must be privileged user)
func TriggerUpdate(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no active session"})
		return
	}

	if !sess.Privileged {
		c.JSON(http.StatusForbidden, gin.H{"error": "update requires sudo privileges"})
		return
	}

	// Optional: accept target version in request body
	var req struct {
		Version string `json:"version,omitempty"`
	}
	_ = c.BindJSON(&req) // Ignore errors, empty version = latest

	// Build args array for IPC (bridge expects []string)
	args := []string{}
	if req.Version != "" {
		args = append(args, req.Version)
	}

	// Call bridge control update handler
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "control", "update", args, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("update failed: %v", err)})
		return
	}

	c.Data(http.StatusOK, "application/json", result)
}
