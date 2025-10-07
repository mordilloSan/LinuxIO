package system

import (
	"encoding/json"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/mordilloSan/LinuxIO/common/ipc"
	"github.com/mordilloSan/LinuxIO/common/logger"
	"github.com/mordilloSan/LinuxIO/common/session"
	"github.com/mordilloSan/LinuxIO/server/bridge"
)

func handleGetInfo(c *gin.Context) {
	sess := session.SessionFromContext(c)
	logger.Infof("%s requested Host info (session: %s)", sess.User.Username, sess.SessionID)
	rawResp, err := bridge.CallWithSession(sess, "system", "get_host_info", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed", "detail": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal([]byte(rawResp), &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response", "detail": err.Error()})
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

	c.JSON(http.StatusOK, resp.Output)
}

func handleGetUptime(c *gin.Context) {
	sess := session.SessionFromContext(c)
	logger.Infof("%s requested uptime (session: %s)", sess.User.Username, sess.SessionID)
	rawResp, err := bridge.CallWithSession(sess, "system", "get_uptime", nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed", "detail": err.Error()})
		return
	}
	var resp ipc.Response
	if err := json.Unmarshal([]byte(rawResp), &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response", "detail": err.Error()})
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

	c.JSON(http.StatusOK, resp.Output)
}
