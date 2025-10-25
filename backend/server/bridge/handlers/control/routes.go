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
	resp, err := bridge.CallWithSession(sess, "control", "version", []string{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("version check failed: %v", err)})
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid response from bridge"})
		return
	}

	c.JSON(http.StatusOK, result)
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
	resp, err := bridge.CallWithSession(sess, "control", "update", args)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("update failed: %v", err)})
		return
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp, &result); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid response from bridge"})
		return
	}

	c.JSON(http.StatusOK, result)
}
