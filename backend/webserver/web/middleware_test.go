package web

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
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
		`msg="http request completed"`,
		"request_kind=api_http",
		"method=POST",
		"path=/auth/login",
		"status=200",
		"duration=",
	)
	requireNotContains(t, output, "next=", "?next=", "outcome=")
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
		`msg="http request completed"`,
		"request_kind=asset",
		"method=GET",
		"path=/assets/*",
		"status=200",
		"duration=",
	)
	requireNotContains(t, output, "cache=1", "?cache=1", "outcome=")
}

func TestLoggerMiddlewareLevels(t *testing.T) {
	// Swaps slog.Default through captureWebLogs, so it must not run in parallel.
	for _, tt := range []struct {
		status int
		want   []string
	}{
		{http.StatusOK, []string{"level=DEBUG", `msg="http request completed"`}},
		{http.StatusNotFound, []string{"level=WARN", `msg="http request failed"`}},
		{http.StatusInternalServerError, []string{"level=ERROR", `msg="http request failed"`}},
	} {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			logs := captureWebLogs(t)

			handler := LoggerMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			}))
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/x", nil))

			output := logs.String()
			if got := strings.Count(output, "\n"); got != 1 {
				t.Fatalf("expected exactly one log record, got %d: %q", got, output)
			}
			requireContains(t, output, append(tt.want, "status="+strconv.Itoa(tt.status))...)
		})
	}
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
