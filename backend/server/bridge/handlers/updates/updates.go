package updates

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
	"github.com/mordilloSan/go_logger/logger"
)

func getUpdatesHandler(c *gin.Context) {
	logger.Infof(" Checking for system updates (D-Bus)...")

	sess := session.SessionFromContext(c)

	output, err := bridge.CallWithSession(sess, "dbus", "GetUpdates", nil)
	if err != nil {
		logger.Errorf(" Failed to get updates: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to get updates",
			"details": err.Error(),
		})
		return
	}

	// 1. Unmarshal bridge response object
	var resp ipc.Response
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		logger.Errorf(" Failed to decode bridge response: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to decode bridge response",
			"details": err.Error(),
		})
		return
	}

	if resp.Status != "ok" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}

	// 2. Defensive: If output is empty/null, treat as empty array
	updates := []Update{}
	if resp.Output != nil {
		// Re-marshal Output back to JSON, then unmarshal to target type
		if outputBytes, err := json.Marshal(resp.Output); err == nil {
			if err := json.Unmarshal(outputBytes, &updates); err != nil {
				logger.Errorf(" Failed to decode updates JSON: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{
					"error":   "failed to decode updates JSON",
					"details": err.Error(),
				})
				return
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"updates": updates})
}

func updatePackageHandler(c *gin.Context) {
	var req struct {
		PackageID string `json:"package"` // Now this must be the *full* PackageKit ID
	}

	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.PackageID) == "" {
		logger.Warnf(" Missing or invalid package id in update request.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request. 'package' field is required."})
		return
	}

	if !strings.Contains(req.PackageID, ";") {
		logger.Warnf(" Invalid package_id submitted: %s", req.PackageID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid package_id"})
		return
	}

	logger.Infof("Triggering update for package: %s", req.PackageID)

	sess := session.SessionFromContext(c)

	output, err := bridge.CallWithSession(sess, "dbus", "InstallPackage", []string{req.PackageID})
	if err != nil {
		logger.Errorf(" Failed to update %s: %v", req.PackageID, err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to update package",
			"details": err.Error(),
		})
		return
	}

	logger.Infof("Package %s updated successfully.\nOutput:\n%s", req.PackageID, output)
	c.JSON(http.StatusOK, gin.H{
		"message": "package updates triggered",
		"output":  string(output), // Keep this - it's useful success output
	})
}
