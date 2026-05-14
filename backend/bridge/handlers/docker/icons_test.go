package docker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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

	gotPath, ok := getCachedIcon(context.Background(), IconTypeDerived, "postgresql")
	if !ok {
		t.Fatalf("getCachedIcon(derived simple icon) found = false, want true")
	}
	if gotPath != cachedPath {
		t.Fatalf("getCachedIcon(derived simple icon) path = %q, want %q", gotPath, cachedPath)
	}
}

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
