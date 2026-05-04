package web

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLoggerMiddlewareLogsAPIRequest(t *testing.T) {
	logs := captureWebLogs(t)

	handler := LoggerMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/auth/login?next=%2Fdashboard", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	output := logs.String()
	requireContains(t, output,
		"api http succeeded: POST /auth/login",
		"request_kind=api_http",
		"path=/auth/login",
		"status=200",
		"outcome=success",
		"duration=",
	)
	requireNotContains(t, output, "next=", "?next=")
}

func TestLoggerMiddlewareLogsAssetRequest(t *testing.T) {
	logs := captureWebLogs(t)

	handler := LoggerMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/assets/index-A1b2C3.js?cache=1", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	output := logs.String()
	requireContains(t, output,
		"asset request succeeded: GET /assets/*",
		"request_kind=asset",
		"path=/assets/*",
		"status=200",
		"outcome=success",
		"duration=",
	)
	requireNotContains(t, output, "cache=1", "?cache=1")
}

func captureWebLogs(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	previous := slog.Default()
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)
	t.Cleanup(func() {
		slog.SetDefault(previous)
	})
	return &buf
}

func requireContains(t *testing.T, output string, needles ...string) {
	t.Helper()

	for _, needle := range needles {
		if !strings.Contains(output, needle) {
			t.Fatalf("expected log output to contain %q, got %q", needle, output)
		}
	}
}

func requireNotContains(t *testing.T, output string, needles ...string) {
	t.Helper()

	for _, needle := range needles {
		if strings.Contains(output, needle) {
			t.Fatalf("expected log output not to contain %q, got %q", needle, output)
		}
	}
}
