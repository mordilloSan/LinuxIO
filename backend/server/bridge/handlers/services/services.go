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
	logger.Infof("User %s requested %s on %s", sess.User.Username, action, serviceName)

	_, err := bridge.CallWithSession(sess, "dbus", action, []string{serviceName})
	if err != nil {
		logger.Errorf("Failed to %s %s via bridge (user: %s,): %v", action, serviceName, sess.User.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	logger.Infof("%s on %s succeeded for user %s", action, serviceName, sess.User.Username)
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func getServiceStatus(c *gin.Context) {
	sess := session.SessionFromContext(c)

	output, err := bridge.CallWithSession(sess, "dbus", "ListServices", nil)
	if err != nil {
		logger.Errorf("Failed to list services via bridge (user: %s,): %v", sess.User.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var resp ipc.Response

	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		logger.Errorf("Failed to decode bridge response (user: %s): %v", sess.User.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode bridge response"})
		return
	}

	if resp.Status != "ok" {
		logger.Warnf("Bridge returned error for service status (user: %s): %v", sess.User.Username, resp.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	logger.Debugf("Returned service status to user %s", sess.User.Username)
	c.JSON(http.StatusOK, resp.Output) // Changed from c.Data()
}

func getServiceDetail(c *gin.Context) {
	sess := session.SessionFromContext(c)
	serviceName := c.Param("name")
	logger.Infof("%s requested detail for %s", sess.User.Username, serviceName)

	output, err := bridge.CallWithSession(sess, "dbus", "GetServiceInfo", []string{serviceName})
	if err != nil {
		logger.Errorf("Failed to get info for %s via bridge (user: %s,): %v", serviceName, sess.User.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		logger.Errorf("Failed to decode bridge response for %s (user: %s): %v", serviceName, sess.User.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode bridge response"})
		return
	}
	if resp.Status != "ok" {
		logger.Warnf("Bridge returned error for %s (user: %s): %v", serviceName, sess.User.Username, resp.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	logger.Debugf("Returned detail for %s to user %s", serviceName, sess.User.Username)
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

	logger.Infof("User %s requested logs for %s", sess.User.Username, serviceName)

	output, err := bridge.CallWithSession(sess, "dbus", "GetServiceLogs", []string{serviceName, lines})
	if err != nil {
		logger.Errorf("Failed to get logs for %s via bridge (user: %s,): %v", serviceName, sess.User.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal([]byte(output), &resp); err != nil {
		logger.Errorf("Failed to decode bridge response for %s logs (user: %s): %v", serviceName, sess.User.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "decode bridge response"})
		return
	}

	if resp.Status != "ok" {
		logger.Warnf("Bridge returned error for %s logs (user: %s): %v", serviceName, sess.User.Username, resp.Error)
		c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error})
		return
	}

	logger.Debugf("Returned logs for %s to user %s", serviceName, sess.User.Username)
	c.JSON(http.StatusOK, resp.Output)
}
