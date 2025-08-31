package filebrowser

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/LinuxIO/cmd/server/config"
	"github.com/mordilloSan/LinuxIO/internal/logger"
	"github.com/mordilloSan/LinuxIO/internal/session"
)

// run-once guard (per user)
var navDefaultsOnce sync.Map // username -> struct{}

// Exported so auth (or others) can call it.
func ApplyNavigatorDefaults(c *gin.Context, sess *session.Session) error {
	// Defensive checks
	if c == nil || c.Request == nil || sess == nil || sess.User.Username == "" || sess.SessionID == "" {
		return fmt.Errorf("invalid input")
	}
	username := sess.User.Username

	// Ensure we only try once per user (until server restart).
	if _, loaded := navDefaultsOnce.LoadOrStore(username, struct{}{}); loaded {
		logger.Debugf("[navigator.defaults] already applied for user=%s", username)
		return nil
	}

	// Derive dark-mode & theme color from LinuxIO config
	dark := false
	themeHex := ""
	if cfg, _, loadErr := config.Load(username); loadErr == nil {
		dark = strings.EqualFold(cfg.AppSettings.Theme, "DARK")
		themeHex = config.NormalizeForFB(cfg.AppSettings.PrimaryColor) // centralized hex or ""
	}

	// Short context for the 3 calls below
	ctx, cancel := context.WithTimeout(c.Request.Context(), 1500*time.Millisecond)
	defer cancel()

	// 1) Ensure FB login (SSO with same cookie)
	if loginErr := fbLogin(ctx, c, sess.SessionID); loginErr != nil {
		navDefaultsOnce.Delete(username) // allow retry on next call
		return fmt.Errorf("fb login: %w", loginErr)
	}

	// 2) GET current user (self) to obtain numeric id & current fields
	userObj, userID, err := fbGetSelf(ctx, c, sess.SessionID)
	if err != nil {
		navDefaultsOnce.Delete(username)
		return fmt.Errorf("fb get self: %w", err)
	}

	// 3) Merge defaults & compute which changed
	which := make([]string, 0, 8)
	if setIfDiff(userObj, "darkMode", dark) {
		which = append(which, "darkMode")
	}
	if setIfDiff(userObj, "viewMode", "normal") {
		which = append(which, "viewMode")
	}
	if setIfDiff(userObj, "showHidden", true) {
		which = append(which, "showHidden")
	}
	if themeHex != "" && setIfDiff(userObj, "themeColor", themeHex) {
		which = append(which, "themeColor")
	}
	if setIfDiff(userObj, "locale", "en") {
		which = append(which, "locale")
	}
	if setIfDiff(userObj, "disableSettings", true) {
		which = append(which, "disableSettings")
	}
	if setIfDiff(userObj, "disableUpdateNotifications", true) {
		which = append(which, "disableUpdateNotifications")
	}
	if setIfDiff(userObj, "hideSidebarFileActions", true) {
		which = append(which, "hideSidebarFileActions")
	}
	if setIfDiff(userObj, "editorQuickSave", true) {
		which = append(which, "editorQuickSave")
	}
	if setIfDiff(userObj, "lockPassword", true) {
		which = append(which, "lockPassword")
	}
	if setIfDiff(userObj, "disableQuickToggles", true) {
		which = append(which, "disableQuickToggles")
	}

	// Nothing to update? Done.
	if len(which) == 0 {
		logger.Debugf("[navigator.defaults] no changes for user=%s", username)
		return nil
	}

	// 4) PUT patch using numeric id
	if err := fbPatchUser(ctx, c, sess.SessionID, userID, which, userObj); err != nil {
		navDefaultsOnce.Delete(username) // allow retry on next login or manual call
		return fmt.Errorf("fb patch user: %w", err)
	}

	logger.Debugf("[navigator.defaults] applied for user=%s dark=%v which=%v", username, dark, which)
	return nil
}

// ---- FB calls (unchanged) ----

func fbLogin(ctx context.Context, c *gin.Context, sessionID string) error {
	url := origin(c) + "/navigator/api/auth/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Cookie", "session_id="+sessionID)
	resp, err := newHTTPClient(c).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("POST /navigator/api/auth/login -> %d", resp.StatusCode)
	}
	return nil
}

func fbGetSelf(ctx context.Context, c *gin.Context, sessionID string) (map[string]any, int, error) {
	url := origin(c) + "/navigator/api/users?id=self"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Cookie", "session_id="+sessionID)

	resp, err := newHTTPClient(c).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return nil, 0, fmt.Errorf("GET /navigator/api/users?id=self -> %d", resp.StatusCode)
	}

	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, 0, err
	}

	// Expect numeric "id" but allow string "1" fallback
	if idF, ok := m["id"].(float64); ok {
		return m, int(idF), nil
	}
	if s, ok := m["id"].(string); ok && s == "1" {
		return m, 1, nil
	}
	return nil, 0, fmt.Errorf("self user has no numeric id")
}

func fbPatchUser(ctx context.Context, c *gin.Context, sessionID string, id int, which []string, fullUser map[string]any) error {
	url := fmt.Sprintf("%s/navigator/api/users?id=%d", origin(c), id)
	payload := map[string]any{
		"what":  "user",
		"which": which,
		"data":  fullUser,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Cookie", "session_id="+sessionID)
	req.Header.Set("Content-Type", "application/json")

	resp, err := newHTTPClient(c).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("PUT /navigator/api/users?id=%d -> %d", id, resp.StatusCode)
	}
	return nil
}

// ---- misc helpers ----

func setIfDiff(m map[string]any, key string, v any) bool {
	if cur, ok := m[key]; ok {
		lhs, _ := json.Marshal(cur)
		rhs, _ := json.Marshal(v)
		if bytes.Equal(lhs, rhs) {
			return false
		}
	}
	m[key] = v
	return true
}

func origin(c *gin.Context) string {
	if c.Request.TLS != nil {
		return "https://" + c.Request.Host
	}
	if proto := c.Request.Header.Get("X-Forwarded-Proto"); strings.EqualFold(proto, "https") {
		return "https://" + c.Request.Host
	}
	return "http://" + c.Request.Host
}

func newHTTPClient(c *gin.Context) *http.Client {
	tr := &http.Transport{ForceAttemptHTTP2: true}
	if c.Request.TLS != nil || strings.EqualFold(c.Request.Header.Get("X-Forwarded-Proto"), "https") {
		roots := TrustedRootPool()
		if roots == nil {
			if sys, err := x509.SystemCertPool(); err == nil {
				roots = sys
			} else {
				roots = x509.NewCertPool()
			}
		}
		tr.TLSClientConfig = &tls.Config{
			RootCAs:    roots,
			ServerName: hostWithoutPort(c.Request.Host),
			MinVersion: tls.VersionTLS12,
		}
	}
	return &http.Client{Timeout: 2 * time.Second, Transport: tr}
}

func hostWithoutPort(h string) string {
	if i := strings.IndexByte(h, ':'); i >= 0 {
		return h[:i]
	}
	return h
}

// Accessor used by your internal HTTP client code
func TrustedRootPool() *x509.CertPool {
	// You can customize this to return a cached or custom pool if needed.
	roots, err := x509.SystemCertPool()
	if err != nil {
		return x509.NewCertPool()
	}
	return roots
}
