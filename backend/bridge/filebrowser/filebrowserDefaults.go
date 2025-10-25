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

	"github.com/mordilloSan/LinuxIO/backend/bridge/userconfig"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/web"
	"github.com/mordilloSan/go_logger/logger"
)

// run-once guard (per user)
var navDefaultsOnce sync.Map // username -> struct{}

// Exported so auth (or bridge) can call it.
// Pass a baseURL like "http://127.0.0.1:8090" or "https://linux.engmariz.com".
func ApplyNavigatorDefaults(baseURL string, sess *session.Session, serverCert string) error {
	// Defensive checks
	if baseURL == "" || sess == nil || sess.User.Username == "" || sess.SessionID == "" {
		return fmt.Errorf("invalid input")
	}
	username := sess.User.Username

	// Ensure we only try once per user (until restart).
	if _, loaded := navDefaultsOnce.LoadOrStore(username, struct{}{}); loaded {
		logger.Debugf("[navigator.defaults] already applied for user=%s", username)
		return nil
	}

	// Derive dark-mode & theme color from LinuxIO config
	dark := false
	themeHex := ""
	cfg, _, err := userconfig.Load(username)
	if err == nil {
		dark = strings.EqualFold(cfg.AppSettings.Theme, "DARK")
		themeHex = userconfig.NormalizeForFB(cfg.AppSettings.PrimaryColor)
	}

	// Short context for the 3 calls below
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	// 1) Ensure FB login (SSO with same cookie)
	if loginErr := fbLogin(ctx, baseURL, sess.SessionID, serverCert); loginErr != nil {
		navDefaultsOnce.Delete(username) // allow retry on next call
		return fmt.Errorf("fb login: %w", loginErr)
	}

	// 2) GET current user (self) to obtain numeric id & current fields
	userObj, userID, err := fbGetSelf(ctx, baseURL, sess.SessionID, serverCert)
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
	if err := fbPatchUser(ctx, baseURL, sess.SessionID, userID, which, userObj, serverCert); err != nil {
		navDefaultsOnce.Delete(username) // allow retry
		return fmt.Errorf("fb patch user: %w", err)
	}

	logger.Debugf("[navigator.defaults] applied for user=%s dark=%v which=%v", username, dark, which)
	return nil
}

// ---- Helpers ----

func fbLogin(ctx context.Context, baseURL, sessionID string, serverCert string) error {
	url := baseURL + "/navigator/api/auth/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Cookie", "session_id="+sessionID)
	resp, err := newHTTPClient(baseURL, serverCert).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("POST /navigator/api/auth/login -> %d", resp.StatusCode)
	}
	return nil
}

func fbGetSelf(ctx context.Context, baseURL, sessionID string, serverCert string) (map[string]any, int, error) {
	url := baseURL + "/navigator/api/users?id=self"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Cookie", "session_id="+sessionID)

	resp, err := newHTTPClient(baseURL, serverCert).Do(req)
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

func fbPatchUser(ctx context.Context, baseURL, sessionID string, id int, which []string, fullUser map[string]any, serverCert string) error {
	url := fmt.Sprintf("%s/navigator/api/users?id=%d", baseURL, id)
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

	resp, err := newHTTPClient(baseURL, serverCert).Do(req)
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

func newHTTPClient(baseURL string, serverCert string) *http.Client {
	tr := &http.Transport{ForceAttemptHTTP2: true}

	if after, ok := strings.CutPrefix(baseURL, "https://"); ok {
		host := hostWithoutPort(after)
		pool := web.GetRootPool()
		if serverCert != "" {
			if cp := x509.NewCertPool(); cp.AppendCertsFromPEM([]byte(serverCert)) {
				pool = cp
			}
		}
		tr.TLSClientConfig = &tls.Config{RootCAs: pool, ServerName: host, MinVersion: tls.VersionTLS12}
	}
	return &http.Client{Timeout: 2 * time.Second, Transport: tr}
}

func hostWithoutPort(h string) string {
	if i := strings.IndexByte(h, ':'); i >= 0 {
		return h[:i]
	}
	return h
}
