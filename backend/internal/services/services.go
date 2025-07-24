package services

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/mordilloSan/LinuxIO/backend/cmd/server/auth"
	"github.com/mordilloSan/LinuxIO/backend/internal/bridge"
	"github.com/mordilloSan/LinuxIO/backend/internal/logger"
	"github.com/mordilloSan/LinuxIO/backend/internal/session"

	"github.com/gin-gonic/gin"
)

func RegisterServiceRoutes(router *gin.Engine) {
	system := router.Group("/system", auth.AuthMiddleware())
	{
		system.GET("/services/status", getServiceStatus)
		system.GET("/services/:name", getServiceDetail)
		system.POST("/services/:name/start", startService)
		system.POST("/services/:name/stop", stopService)
		system.POST("/services/:name/restart", restartService)
		system.POST("/services/:name/reload", reloadService)
		system.POST("/services/:name/enable", enableService)
		system.POST("/services/:name/disable", disableService)
		system.POST("/services/:name/mask", maskService)
		system.POST("/services/:name/unmask", unmaskService)
	}
}

func startService(c *gin.Context)   { serviceAction(c, "StartService") }
func stopService(c *gin.Context)    { serviceAction(c, "StopService") }
func restartService(c *gin.Context) { serviceAction(c, "RestartService") }
func reloadService(c *gin.Context)  { serviceAction(c, "ReloadService") }
func enableService(c *gin.Context)  { serviceAction(c, "EnableService") }
func disableService(c *gin.Context) { serviceAction(c, "DisableService") }
func maskService(c *gin.Context)    { serviceAction(c, "MaskService") }
func unmaskService(c *gin.Context)  { serviceAction(c, "UnmaskService") }

var validServiceName = regexp.MustCompile(`^[\w.-]+\.service$`)

// Generic handler for service actions
func serviceAction(c *gin.Context, action string) {
	sess := session.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	serviceName := c.Param("name")

	if !validServiceName.MatchString(serviceName) {
		logger.Warnf("Invalid service name for %s: %q by user: %s", action, serviceName, sess.User.Name)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service name"})
		return
	}
	logger.Infof("User %s requested %s on %s (session: %s)", sess.User.Name, action, serviceName, sess.SessionID)

	_, err := bridge.CallWithSession(sess, "dbus", action, []string{serviceName})
	if err != nil {
		logger.Errorf("Failed to %s %s via bridge (user: %s, session: %s): %v", action, serviceName, sess.User.Name, sess.SessionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	logger.Infof("%s on %s succeeded for user %s (session: %s)", action, serviceName, sess.User.Name, sess.SessionID)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func getServiceStatus(c *gin.Context) {
	sess := session.GetSessionOrAbort(c)
	if sess == nil {
		return
	}

	output, err := bridge.CallWithSession(sess, "dbus", "ListServices", nil)
	if err != nil {
		logger.Errorf("Failed to list services via bridge (user: %s, session: %s): %v", sess.User.Name, sess.SessionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var resp struct {
		Status string          `json:"status"`
		Output json.RawMessage `json:"output"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		logger.Errorf("Failed to decode bridge response (user: %s): %v", sess.User.Name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode bridge response"})
		return
	}

	if resp.Status != "ok" {
		logger.Warnf("Bridge returned error for service status (user: %s): %v", sess.User.Name, resp.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}

	logger.Debugf("Returned service status to user %s", sess.User.Name)
	c.Data(http.StatusOK, "application/json", resp.Output)
}

func getServiceDetail(c *gin.Context) {
	sess := session.GetSessionOrAbort(c)
	if sess == nil {
		return
	}
	serviceName := c.Param("name")
	logger.Infof("%s requested detail for %s (session: %s)", sess.User.Name, serviceName, sess.SessionID)

	output, err := bridge.CallWithSession(sess, "dbus", "GetServiceInfo", []string{serviceName})
	if err != nil {
		logger.Errorf("Failed to get info for %s via bridge (user: %s, session: %s): %v", serviceName, sess.User.Name, sess.SessionID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp struct {
		Status string          `json:"status"`
		Output json.RawMessage `json:"output"`
		Error  string          `json:"error"`
	}
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		logger.Errorf("Failed to decode bridge response for %s (user: %s): %v", serviceName, sess.User.Name, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode bridge response"})
		return
	}
	if resp.Status != "ok" {
		logger.Warnf("Bridge returned error for %s (user: %s): %v", serviceName, sess.User.Name, resp.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}
	logger.Debugf("Returned detail for %s to user %s", serviceName, sess.User.Name)
	c.Data(http.StatusOK, "application/json", resp.Output)
}
