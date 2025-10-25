package services

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
	"github.com/mordilloSan/go_logger/logger"
)

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
	sess := session.SessionFromContext(c)
	serviceName := c.Param("name")

	if !validServiceName.MatchString(serviceName) {
		logger.Warnf("Invalid service name for %s: %q by user: %s", action, serviceName, sess.User.Username)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service name"})
		return
	}

	_, err := bridge.CallWithSession(sess, "dbus", action, []string{serviceName})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func getServiceStatus(c *gin.Context) {
	sess := session.SessionFromContext(c)

	output, err := bridge.CallWithSession(sess, "dbus", "ListServices", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var resp ipc.Response

	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode bridge response"})
		return
	}

	if resp.Status != "ok" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp.Output) // Changed from c.Data()
}

func getServiceDetail(c *gin.Context) {
	sess := session.SessionFromContext(c)
	serviceName := c.Param("name")

	output, err := bridge.CallWithSession(sess, "dbus", "GetServiceInfo", []string{serviceName})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode bridge response"})
		return
	}
	if resp.Status != "ok" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp.Output) // Changed from c.Data()
}

func getServiceLogs(c *gin.Context) {
	sess := session.SessionFromContext(c)
	serviceName := c.Param("name")

	if !validServiceName.MatchString(serviceName) {
		logger.Warnf("Invalid service name for logs: %q by user: %s", serviceName, sess.User.Username)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid service name"})
		return
	}

	// Get optional query parameters
	lines := c.DefaultQuery("lines", "100") // Default 100 lines

	output, err := bridge.CallWithSession(sess, "dbus", "GetServiceLogs", []string{serviceName, lines})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode bridge response"})
		return
	}

	if resp.Status != "ok" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}

	c.JSON(http.StatusOK, resp.Output)
}
