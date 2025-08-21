package auth

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/internal/config"
	"github.com/mordilloSan/LinuxIO/internal/logger"
)

// syncFilebrowser:
//  1. warmup GET /navigator/ (triggers auto-provision/login via your proxy header)
//  2. PUT /navigator/api/users?id=1 with darkMode + viewMode
//
// If you later move beyond single-user mapping, replace the hardcoded id=1 with
// your own user lookup, or a server-side mapping.
func syncFilebrowser(c *gin.Context, sessionID string, username string) {
	ctx, cancel := context.WithTimeout(c, 1500*time.Millisecond)
	defer cancel()

	cc := c.Copy()
	cc.Request = c.Request.Clone(ctx)

	// derive dark from persisted config
	dark := false
	if cfg, _, err := config.Load(username); err == nil {
		dark = strings.EqualFold(cfg.ThemeSettings.Theme, "DARK")
	} else {
		logger.Debugf("[login.sync] theme load failed for %s: %v", username, err)
	}

	// 1) warm-up
	if err := warmupNavigator(cc, sessionID); err != nil {
		logger.Debugf("[fb.warmup] skipped: %v", err)
	}
	// 2) sync prefs (id=1 for single-user setups)
	if err := updateUserPrefsWithSession(cc, sessionID, 1, map[string]any{
		"darkMode":   dark,
		"viewMode":   "normal",
		"showHidden": true,
	}); err != nil {
		logger.Debugf("[fb.sync] skipped: %v", err)
	}
}

func warmupNavigator(c *gin.Context, sessionID string) error {
	url := origin(c) + "/navigator/"
	req, err := http.NewRequestWithContext(c, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Cookie", "session_id="+sessionID)

	resp, err := newHTTPClient(c).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	logger.Debugf("[fb.warmup] GET /navigator/ -> %d", resp.StatusCode)
	return nil
}

func updateUserPrefsWithSession(c *gin.Context, sessionID string, fbUserID int, fields map[string]any) error {
	if fbUserID <= 0 {
		return fmt.Errorf("invalid filebrowser id %d", fbUserID)
	}
	url := origin(c) + fmt.Sprintf("/navigator/api/users?id=%d", fbUserID)

	payload := map[string]any{"what": "user", "which": keys(fields), "data": fields}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(c, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "session_id="+sessionID)

	resp, err := newHTTPClient(c).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("PUT %s returned %d", url, resp.StatusCode)
	}
	logger.Debugf("[fb.sync] PUT /navigator/api/users?id=%d fields=%v OK", fbUserID, keys(fields))
	return nil
}

// helpers

func isHTTPS(c *gin.Context) bool {
	// direct TLS or TLS terminated upstream (proxy)
	if c.Request.TLS != nil {
		return true
	}
	if proto := c.Request.Header.Get("X-Forwarded-Proto"); strings.EqualFold(proto, "https") {
		return true
	}
	return false
}

func origin(c *gin.Context) string {
	if isHTTPS(c) {
		return "https://" + c.Request.Host
	}
	return "http://" + c.Request.Host
}

func newHTTPClient(c *gin.Context) *http.Client {
	tr := &http.Transport{}
	if isHTTPS(c) {
		// self-signed cert on local server in prod
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &http.Client{Timeout: 2 * time.Second, Transport: tr}
}

func keys(m map[string]any) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
