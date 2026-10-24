package appupdate

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/webserver/sessions"
)

func newRouterForTests(sm *sessions.Manager) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("GET /api/update-info", sm.RequireSession(http.HandlerFunc(handleUpdateInfo)))
	return mux
}

func doJSON(r http.Handler, method, path string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		if err := json.MarshalWrite(&buf, body); err != nil {
			panic(fmt.Sprintf("encode test request body: %v", err))
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func sessionCookie(t *testing.T, sm *sessions.Manager, privileged bool) *http.Cookie {
	t.Helper()
	id, err := sm.NewSessionID()
	if err != nil {
		t.Fatalf("new session ID: %v", err)
	}
	if _, err := sm.CreateSession(id, session.User{Username: "test", UID: 1000, GID: 1000}, privileged); err != nil {
		t.Fatalf("create session: %v", err)
	}
	w := httptest.NewRecorder()
	sm.WriteCookie(w, id)
	for _, c := range w.Result().Cookies() {
		if c.Name == sm.CookieName() {
			return c
		}
	}
	t.Fatalf("cookie %q not set; headers=%v", sm.CookieName(), w.Result().Header)
	return nil
}

func TestUpdateInfo_MissingSessionReturns401ThroughMiddleware(t *testing.T) {
	sm := sessions.NewManager(sessions.DefaultConfig)
	t.Cleanup(sm.Close)
	mux := http.NewServeMux()
	RegisterRoutes(mux, sm)

	w := doJSON(mux, http.MethodGet, "/api/update-info", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateInfo_UnprivilegedReturns204WithoutChecking(t *testing.T) {
	oldCheck := checkForUpdate
	t.Cleanup(func() { checkForUpdate = oldCheck })
	called := false
	checkForUpdate = func(context.Context) *UpdateInfo {
		called = true
		return &UpdateInfo{Available: true}
	}

	sm := sessions.NewManager(sessions.DefaultConfig)
	t.Cleanup(sm.Close)
	w := doJSON(newRouterForTests(sm), http.MethodGet, "/api/update-info", nil, sessionCookie(t, sm, false))
	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if called {
		t.Fatal("unprivileged update request invoked checkForUpdate")
	}
	if w.Body.Len() != 0 {
		t.Fatalf("want empty 204 body, got %q", w.Body.String())
	}
}

func TestUpdateInfo_PrivilegedNilReturns204(t *testing.T) {
	oldCheck := checkForUpdate
	t.Cleanup(func() { checkForUpdate = oldCheck })
	called := false
	checkForUpdate = func(ctx context.Context) *UpdateInfo {
		called = true
		if ctx == nil {
			t.Fatal("checkForUpdate received nil context")
		}
		return nil
	}

	sm := sessions.NewManager(sessions.DefaultConfig)
	t.Cleanup(sm.Close)
	w := doJSON(newRouterForTests(sm), http.MethodGet, "/api/update-info", nil, sessionCookie(t, sm, true))
	if w.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", w.Code, w.Body.String())
	}
	if !called {
		t.Fatal("privileged update request did not invoke checkForUpdate")
	}
	if w.Body.Len() != 0 {
		t.Fatalf("want empty 204 body, got %q", w.Body.String())
	}
}

func TestUpdateInfo_PrivilegedResultReturnsExactJSON(t *testing.T) {
	oldCheck := checkForUpdate
	t.Cleanup(func() { checkForUpdate = oldCheck })
	want := &UpdateInfo{
		Available:      true,
		CurrentVersion: "v1.2.3",
		LatestVersion:  "v1.3.0",
		ReleaseURL:     "https://example.test/linuxio/releases/v1.3.0",
	}
	checkForUpdate = func(context.Context) *UpdateInfo { return want }

	sm := sessions.NewManager(sessions.DefaultConfig)
	t.Cleanup(sm.Close)
	w := doJSON(newRouterForTests(sm), http.MethodGet, "/api/update-info", nil, sessionCookie(t, sm, true))
	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", w.Code, w.Body.String())
	}
	var got UpdateInfo
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if got != *want {
		t.Fatalf("update response = %+v, want %+v", got, *want)
	}
}
