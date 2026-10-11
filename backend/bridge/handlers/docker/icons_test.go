package docker

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDerivedIconUsesSimpleIconCache(t *testing.T) {
	withTempIconCacheDirs(t)

	if err := os.MkdirAll(simpleIconsCacheDir, 0755); err != nil {
		t.Fatalf("MkdirAll(simpleIconsCacheDir) error = %v", err)
	}
	cachedPath := filepath.Join(simpleIconsCacheDir, "postgresql.svg")
	if err := os.WriteFile(cachedPath, []byte("<svg></svg>"), 0644); err != nil {
		t.Fatalf("WriteFile(simple icon cache) error = %v", err)
	}

	gotPath, ok := getCachedIcon(context.Background(), IconTypeDerived, "postgresql", false)
	if !ok {
		t.Fatalf("getCachedIcon(derived simple icon) found = false, want true")
	}
	if gotPath != cachedPath {
		t.Fatalf("getCachedIcon(derived simple icon) path = %q, want %q", gotPath, cachedPath)
	}
}

func TestGetIconServesStaleCacheWhenOffline(t *testing.T) {
	withTempIconCacheDirs(t)

	oldClient := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}
	t.Cleanup(func() { httpClient = oldClient })

	if err := os.MkdirAll(dashboardIconsCacheDir, 0755); err != nil {
		t.Fatalf("MkdirAll(dashboardIconsCacheDir) error = %v", err)
	}
	cachedPath := filepath.Join(dashboardIconsCacheDir, "nginx.svg")
	if err := os.WriteFile(cachedPath, []byte("<svg>nginx</svg>"), 0644); err != nil {
		t.Fatalf("WriteFile(dashboard icon cache) error = %v", err)
	}
	expired := time.Now().Add(-2 * iconCacheDuration)
	if err := os.Chtimes(cachedPath, expired, expired); err != nil {
		t.Fatalf("Chtimes(dashboard icon cache) error = %v", err)
	}

	data, err := GetIcon(context.Background(), "nginx")
	if err != nil {
		t.Fatalf("GetIcon(nginx) error = %v, want stale cache", err)
	}
	if string(data) != "<svg>nginx</svg>" {
		t.Fatalf("GetIcon(nginx) = %q, want stale cached icon", data)
	}

	if _, err := GetIcon(context.Background(), "uncached"); err == nil {
		t.Fatalf("GetIcon(uncached) error = nil, want fetch error")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func withTempIconCacheDirs(t *testing.T) {
	t.Helper()

	oldIconCacheDir := iconCacheDir
	oldDashboardIconsCacheDir := dashboardIconsCacheDir
	oldSimpleIconsCacheDir := simpleIconsCacheDir
	oldURLCacheDir := urlCacheDir
	oldUserIconsDir := userIconsDir

	base := t.TempDir()
	iconCacheDir = filepath.Join(base, "icons")
	dashboardIconsCacheDir = filepath.Join(iconCacheDir, "dashboard-icons")
	simpleIconsCacheDir = filepath.Join(iconCacheDir, "simple-icons")
	urlCacheDir = filepath.Join(iconCacheDir, "url-cache")
	userIconsDir = filepath.Join(iconCacheDir, "user")

	t.Cleanup(func() {
		iconCacheDir = oldIconCacheDir
		dashboardIconsCacheDir = oldDashboardIconsCacheDir
		simpleIconsCacheDir = oldSimpleIconsCacheDir
		urlCacheDir = oldURLCacheDir
		userIconsDir = oldUserIconsDir
	})
}
