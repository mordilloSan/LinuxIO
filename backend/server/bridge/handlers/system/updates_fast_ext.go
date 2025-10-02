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

func handleGetFastUpdates(c *gin.Context) {
    sess := session.SessionFromContext(c)
    logger.Infof("%s requested fast updates (session: %s)", sess.User.Username, sess.SessionID)
    rawResp, err := bridge.CallWithSession(sess, "system", "get_updates_fast", nil)
    if err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed", "detail": err.Error(), "output": rawResp}); return }
    var resp ipc.Response
    if err := json.Unmarshal([]byte(rawResp), &resp); err != nil { c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response", "detail": err.Error(), "output": rawResp}); return }
    if resp.Status != "ok" { c.JSON(http.StatusInternalServerError, gin.H{"error": resp.Error, "output": string(resp.Output)}); return }
    c.Data(http.StatusOK, "application/json", resp.Output)
}

