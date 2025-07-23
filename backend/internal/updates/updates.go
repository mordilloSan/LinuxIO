package updates

import (
	"encoding/json"
	"net/http"
	"strings"

	"backend/internal/auth"
	"backend/internal/bridge"
	"backend/internal/logger"

	"github.com/gin-gonic/gin"
)

type BridgeResponse struct {
	Status string          `json:"status"`
	Output json.RawMessage `json:"output"`
	Error  string          `json:"error"`
}

func RegisterUpdateRoutes(router *gin.Engine) {
	system := router.Group("/system", auth.AuthMiddleware())
	{
		system.GET("/updates", getUpdatesHandler)
		system.POST("/update", updatePackageHandler)
		system.GET("/updates/update-history", getUpdateHistoryHandler)
		system.GET("/updates/settings", getUpdateSettings)
		system.POST("/updates/settings", postUpdateSettings)
	}
}

func getUpdatesHandler(c *gin.Context) {
	logger.Infof("🔍 Checking for system updates (D-Bus)...")

	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}

	output, err := bridge.CallWithSession(sess, "dbus", "GetUpdates", nil)
	if err != nil {
		logger.Errorf("❌ Failed to get updates: %v\nOutput: %s", err, output)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to get updates",
			"details": err.Error(),
			"output":  output,
		})
		return
	}

	// 1. Unmarshal bridge response object
	var resp struct {
		Status string          `json:"status"`
		Output json.RawMessage `json:"output"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		logger.Errorf("❌ Failed to decode bridge response: %v\nOutput: %s", err, output)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to decode bridge response",
			"details": err.Error(),
			"output":  output,
		})
		return
	}

	// 2. Defensive: If output is empty/null, treat as empty array
	updates := []Update{}
	if string(resp.Output) != "null" && len(resp.Output) > 0 {
		if err := json.Unmarshal(resp.Output, &updates); err != nil {
			logger.Errorf("❌ Failed to decode updates JSON: %v\nOutput: %s", err, string(resp.Output))
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "failed to decode updates JSON",
				"details": err.Error(),
				"output":  string(resp.Output),
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{"updates": updates})
}

func updatePackageHandler(c *gin.Context) {
	var req struct {
		PackageID string `json:"package"` // Now this must be the *full* PackageKit ID
	}

	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.PackageID) == "" {
		logger.Warnf("⚠️ Missing or invalid package id in update request.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request. 'package' field is required."})
		return
	}

	if !strings.Contains(req.PackageID, ";") {
		logger.Warnf("⚠️ Invalid package_id submitted: %s", req.PackageID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid package_id"})
		return
	}

	logger.Infof("📦 Triggering update for package: %s", req.PackageID)

	sess := auth.GetSessionOrAbort(c)
	if sess == nil {
		return
	}

	output, err := bridge.CallWithSession(sess, "dbus", "InstallPackage", []string{req.PackageID})

	if err != nil {
		logger.Errorf("❌ Failed to update %s: %v\nOutput: %s", req.PackageID, err, output)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to update package",
			"details": err.Error(),
			"output":  output,
		})
		return
	}

	logger.Infof("✅ Package %s updated successfully.\nOutput:\n%s", req.PackageID, output)
	c.JSON(http.StatusOK, gin.H{
		"message": "package updates triggered",
		"output":  output,
	})
}

// GET /system/updates/settings
func getUpdateSettings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"enabled":   true,
		"frequency": "daily",
		"lastRun":   "2025-05-15T12:34:00Z",
	})
}

// POST /system/updates/settings
func postUpdateSettings(c *gin.Context) {
	var req struct {
		Enabled   bool   `json:"enabled"`
		Frequency string `json:"frequency"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid settings"})
		return
	}
	// Save logic here...
	c.Status(http.StatusNoContent)
}
