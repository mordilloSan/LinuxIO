package updates

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
	"github.com/mordilloSan/go_logger/logger"
)

func getUpdatesHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)

	var raw json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "dbus", "GetUpdates", nil, &raw); err != nil {
		if errors.Is(err, bridge.ErrEmptyBridgeOutput) {
			c.JSON(http.StatusOK, gin.H{"updates": []Update{}})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to get updates",
			"details": err.Error(),
		})
		return
	}

	// 2. Defensive: If output is empty/null, treat as empty array
	updates := []Update{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &updates); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "failed to decode updates JSON",
				"details": err.Error(),
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
		logger.Warnf(" Missing or invalid package id in update request.")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request. 'package' field is required."})
		return
	}

	if !strings.Contains(req.PackageID, ";") {
		logger.Warnf(" Invalid package_id submitted: %s", req.PackageID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid package_id"})
		return
	}

	sess := session.SessionFromContext(c)

	if err := bridge.CallTypedWithSession(sess, "dbus", "InstallPackage", []string{req.PackageID}, nil); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Failed to update package",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "package updates triggered",
		"package": req.PackageID,
	})
}
