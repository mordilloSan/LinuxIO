package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	commonpcpapi "github.com/mordilloSan/LinuxIO/backend/common/pcpapi"
)

func TestProtectAllowsPublicEndpointWithoutToken(t *testing.T) {
	cfg := commonpcpapi.DefaultConfig()
	cfg.Enabled = true
	cfg.Exposure.Categories["cpu"] = commonpcpapi.ExposurePublic

	app := newApp(nil, cfg, "secret-token")
	called := false
	handler := app.protect("/api/v1/cpu", func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cpu", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
	if !called {
		t.Fatal("expected wrapped handler to be called")
	}
}

func TestProtectRejectsPrivateEndpointWithoutToken(t *testing.T) {
	cfg := commonpcpapi.DefaultConfig()
	cfg.Enabled = true

	app := newApp(nil, cfg, "secret-token")
	handler := app.protect("/api/v1/cpu", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cpu", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, rec.Code)
	}
	if got := rec.Header().Get("WWW-Authenticate"); got == "" {
		t.Fatal("expected WWW-Authenticate header to be set")
	}
}

func TestProtectAllowsPrivateEndpointWithValidBearerToken(t *testing.T) {
	cfg := commonpcpapi.DefaultConfig()
	cfg.Enabled = true

	app := newApp(nil, cfg, "secret-token")
	handler := app.protect("/api/v1/cpu", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cpu", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
}

func TestProtectAllowsPrivateEndpointWhenAuthDisabled(t *testing.T) {
	cfg := commonpcpapi.DefaultConfig()
	cfg.Enabled = true
	cfg.Auth.Enabled = false

	app := newApp(nil, cfg, "secret-token")
	handler := app.protect("/api/v1/cpu", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cpu", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d", http.StatusNoContent, rec.Code)
	}
}

func TestProtectRejectsWhenServiceDisabled(t *testing.T) {
	cfg := commonpcpapi.DefaultConfig()
	cfg.Enabled = false

	app := newApp(nil, cfg, "secret-token")
	handler := app.protect("/api/v1/cpu", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cpu", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestProtectRejectsNonGETRequests(t *testing.T) {
	cfg := commonpcpapi.DefaultConfig()
	cfg.Enabled = true
	cfg.Exposure.Categories["cpu"] = commonpcpapi.ExposurePublic

	app := newApp(nil, cfg, "secret-token")
	handler := app.protect("/api/v1/cpu", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/cpu", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestHealthAndVersionRemainPublic(t *testing.T) {
	cfg := commonpcpapi.DefaultConfig()

	app := newApp(nil, cfg, "secret-token")
	handler := app.routes()

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRec := httptest.NewRecorder()
	handler.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("expected healthz status %d, got %d", http.StatusOK, healthRec.Code)
	}

	versionReq := httptest.NewRequest(http.MethodGet, "/version", nil)
	versionRec := httptest.NewRecorder()
	handler.ServeHTTP(versionRec, versionReq)
	if versionRec.Code != http.StatusOK {
		t.Fatalf("expected version status %d, got %d", http.StatusOK, versionRec.Code)
	}
}
