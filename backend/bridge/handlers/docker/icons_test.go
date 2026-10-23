package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDerivedIconUsesSimpleIconCache(t *testing.T) {
	withTempIconCacheDirs(t)

	cachedPath := writeCacheFile(t, filepath.Join(simpleIconsCacheDir, "postgresql.svg"), "<svg></svg>")

	entry, ok := lookupCachedIcon(context.Background(), IconTypeDerived, "postgresql")
	if !ok {
		t.Fatalf("lookupCachedIcon(derived simple icon) found = false, want true")
	}
	if entry.path != cachedPath || entry.missing || entry.stale {
		t.Fatalf("lookupCachedIcon(derived simple icon) = %+v, want fresh icon at %q", entry, cachedPath)
	}
}

func TestGetIconServesStaleCacheWhenOffline(t *testing.T) {
	withTempIconCacheDirs(t)
	requested := stubIconTransport(t, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})

	cachedPath := writeCacheFile(t, filepath.Join(dashboardIconsCacheDir, "nginx.svg"), "<svg>nginx</svg>")
	expired := time.Now().Add(-2 * iconRefreshInterval)
	setFileTimes(t, cachedPath, expired, expired)

	data, err := GetIcon(context.Background(), "nginx")
	if err != nil {
		t.Fatalf("GetIcon(nginx) error = %v, want stale cache", err)
	}
	if string(data) != "<svg>nginx</svg>" {
		t.Fatalf("GetIcon(nginx) = %q, want stale cached icon", data)
	}
	// Serving records the use without re-confirming the stale entry.
	info, err := os.Stat(cachedPath)
	if err != nil {
		t.Fatalf("Stat(cached icon) error = %v", err)
	}
	if !info.ModTime().Equal(expired) {
		t.Fatalf("stale icon mtime = %v, want unchanged %v", info.ModTime(), expired)
	}
	if lastUsed(info).Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("stale icon last used = %v, want touched on serve", lastUsed(info))
	}

	// An outage is not a miss: no marker, and no Docker fallback download.
	if _, err := GetIcon(context.Background(), "uncached"); err == nil {
		t.Fatalf("GetIcon(uncached) error = nil, want fetch error")
	}
	if _, err := os.Stat(missingMarkerPath(IconTypeDerived, "uncached")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("miss marker after outage: Stat error = %v, want not exist", err)
	}
	for _, url := range *requested {
		if strings.Contains(url, "/docker") {
			t.Fatalf("GetIcon(uncached) fetched docker fallback %q", url)
		}
	}
}

func TestGetIconRemembersMissingIcon(t *testing.T) {
	withTempIconCacheDirs(t)
	requested := stubIconTransport(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader(""))}, nil
	})

	if _, err := GetIcon(context.Background(), "ghost"); !errors.Is(err, errIconNotFound) {
		t.Fatalf("GetIcon(ghost) error = %v, want errIconNotFound", err)
	}
	if _, err := os.Stat(missingMarkerPath(IconTypeDerived, "ghost")); err != nil {
		t.Fatalf("miss marker after 404: Stat error = %v", err)
	}
	if GetIconInfo(context.Background(), "ghost").Cached {
		t.Fatalf("GetIconInfo(ghost).Cached = true, want false for a miss")
	}

	before := len(*requested)
	if _, err := GetIcon(context.Background(), "ghost"); !errors.Is(err, errIconNotFound) {
		t.Fatalf("GetIcon(ghost) again error = %v, want errIconNotFound", err)
	}
	if len(*requested) != before {
		t.Fatalf("GetIcon(ghost) again made %d requests, want 0", len(*requested)-before)
	}

	// Once the icon exists upstream, a stale miss is re-checked and forgotten.
	marker := missingMarkerPath(IconTypeDerived, "ghost")
	expired := time.Now().Add(-2 * iconRefreshInterval)
	setFileTimes(t, marker, expired, expired)
	stubIconTransport(t, func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("<svg>ghost</svg>"))}, nil
	})
	data, err := GetIcon(context.Background(), "ghost")
	if err != nil || string(data) != "<svg>ghost</svg>" {
		t.Fatalf("GetIcon(ghost) after publish = %q, %v; want icon", data, err)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("miss marker after download: Stat error = %v, want not exist", err)
	}
}

func TestSweepIconCacheRemovesUnusedEntries(t *testing.T) {
	withTempIconCacheDirs(t)

	old := time.Now().Add(-2 * iconUnusedTTL)
	unused := writeCacheFile(t, filepath.Join(dashboardIconsCacheDir, "old.svg"), "<svg/>")
	setFileTimes(t, unused, old, old)
	unusedMarker := writeCacheFile(t, filepath.Join(simpleIconsCacheDir, "old"+missingIconExt), "")
	setFileTimes(t, unusedMarker, old, old)
	// Downloaded long ago but served recently: atime keeps it alive.
	served := writeCacheFile(t, filepath.Join(urlCacheDir, "served.svg"), "<svg/>")
	setFileTimes(t, served, time.Now(), old)
	recent := writeCacheFile(t, filepath.Join(dashboardIconsCacheDir, "new.svg"), "<svg/>")
	user := writeCacheFile(t, filepath.Join(userIconsDir, "mine.svg"), "<svg/>")
	setFileTimes(t, user, old, old)

	sweepIconCache(context.Background(), time.Now().Add(-iconUnusedTTL))

	for _, path := range []string{unused, unusedMarker} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Stat(%s) error = %v, want removed", path, err)
		}
	}
	for _, path := range []string{served, recent, user} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("Stat(%s) error = %v, want kept", path, err)
		}
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// stubIconTransport replaces the icon HTTP client for the test and returns
// the list of requested URLs.
func stubIconTransport(t *testing.T, handle func(*http.Request) (*http.Response, error)) *[]string {
	t.Helper()
	requested := &[]string{}
	oldClient := httpClient
	httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		*requested = append(*requested, r.URL.String())
		return handle(r)
	})}
	t.Cleanup(func() { httpClient = oldClient })
	return requested
}

func writeCacheFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	return path
}

func setFileTimes(t *testing.T, path string, atime, mtime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, atime, mtime); err != nil {
		t.Fatalf("Chtimes(%s) error = %v", path, err)
	}
}

func withTempIconCacheDirs(t *testing.T) {
	t.Helper()

	oldIconCacheDir := iconCacheDir
	oldDashboardIconsCacheDir := dashboardIconsCacheDir
	oldSimpleIconsCacheDir := simpleIconsCacheDir
	oldURLCacheDir := urlCacheDir
	oldUserIconsDir := userIconsDir
	oldSweep := lastIconSweep.Load()

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
		lastIconSweep.Store(oldSweep)
	})
}

func TestJoinFetchErrorsOnlyMissesWhenAllSourcesMiss(t *testing.T) {
	miss := fmt.Errorf("svg: %w", errIconNotFound)
	outage := errors.New("timeout")

	if err := joinFetchErrors(miss, fmt.Errorf("png: %w", errIconNotFound)); !errors.Is(err, errIconNotFound) {
		t.Fatalf("joinFetchErrors(miss, miss) = %v, want errIconNotFound", err)
	}
	if err := joinFetchErrors(miss, outage); errors.Is(err, errIconNotFound) || !errors.Is(err, outage) {
		t.Fatalf("joinFetchErrors(miss, outage) = %v, want only the outage", err)
	}
}

func TestParseIconIdentifierRejectsUnsafeNames(t *testing.T) {
	for _, tc := range []struct {
		identifier string
		wantType   IconType
		wantValue  string
	}{
		{"nginx", IconTypeDerived, "nginx"},
		{"si:react", IconTypeSimpleIcon, "react"},
		{"dashboard-icon:plex", IconTypeDashboardIcon, "plex"},
		{"my-icon.svg", IconTypeFile, "my-icon.svg"},
		{"https://example.com/icon.svg", IconTypeURL, "https://example.com/icon.svg"},
		{"../../../../etc/x.png", IconTypeUnknown, ""},
		{"si:../x", IconTypeUnknown, ""},
		{"a/b", IconTypeUnknown, ""},
		{".hidden.svg", IconTypeUnknown, ""},
		{"", IconTypeUnknown, ""},
	} {
		gotType, gotValue := parseIconIdentifier(tc.identifier)
		if gotType != tc.wantType || gotValue != tc.wantValue {
			t.Errorf("parseIconIdentifier(%q) = (%s, %q), want (%s, %q)", tc.identifier, gotType, gotValue, tc.wantType, tc.wantValue)
		}
	}
}

func TestRefuseLocalDial(t *testing.T) {
	for _, address := range []string{"127.0.0.1:80", "[::1]:443", "169.254.169.254:80", "0.0.0.0:80"} {
		if err := refuseLocalDial("tcp", address, nil); err == nil {
			t.Errorf("refuseLocalDial(%q) = nil, want error", address)
		}
	}
	for _, address := range []string{"93.184.216.34:443", "192.168.1.10:80", "[2606:4700::1]:443"} {
		if err := refuseLocalDial("tcp", address, nil); err != nil {
			t.Errorf("refuseLocalDial(%q) = %v, want nil", address, err)
		}
	}
}
