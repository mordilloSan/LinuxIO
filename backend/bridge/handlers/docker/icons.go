package docker

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const (
	// Dashboard Icons repository (homarr-labs). Some icons are SVG, others PNG.
	dashboardIconsRawBase = "https://raw.githubusercontent.com/homarr-labs/dashboard-icons/main"
	// Simple Icons CDN - for brand icons via si: prefix
	simpleIconsCDN = "https://cdn.simpleicons.org"

	// A cached icon (or a remembered miss) is re-checked upstream once it is
	// older than this; a failed re-check keeps serving the cached answer.
	iconRefreshInterval = 7 * 24 * time.Hour
	// Cache entries not served for this long are deleted by the sweep.
	iconUnusedTTL     = 30 * 24 * time.Hour
	iconSweepInterval = 24 * time.Hour
	// Marker file extension recording that no upstream source has the icon.
	missingIconExt = ".missing"

	httpClientTimeout = 10 * time.Second
)

var httpClient = &http.Client{
	Timeout: httpClientTimeout,
}

// errIconNotFound marks a definite upstream 404, as opposed to a network
// failure; only this outcome is remembered as a miss.
var errIconNotFound = errors.New("icon not found")

// Unix time of the last cache sweep in this process.
var lastIconSweep atomic.Int64

// The cache lives under the persistent data dir so icons survive reboots and
// stay available offline; the packaged tmpfiles policy creates these dirs.
var (
	iconCacheDir           = filepath.Join(version.DataDir, "icons")
	dashboardIconsCacheDir = filepath.Join(iconCacheDir, "dashboard-icons")
	simpleIconsCacheDir    = filepath.Join(iconCacheDir, "simple-icons")
	urlCacheDir            = filepath.Join(iconCacheDir, "url-cache")
	userIconsDir           = filepath.Join(iconCacheDir, "user")
)

type IconType string

const (
	IconTypeSimpleIcon    IconType = "simple-icon"
	IconTypeDashboardIcon IconType = "dashboard-icon"
	IconTypeURL           IconType = "url"
	IconTypeFile          IconType = "file"
	IconTypeDerived       IconType = "derived"
	IconTypeUnknown       IconType = "unknown"
)

// IconInfo contains metadata about an icon
type IconInfo struct {
	Type       IconType `json:"type"`
	Identifier string   `json:"identifier"`
	Cached     bool     `json:"cached"`
}

// initIconCache ensures the icon cache directories exist
func initIconCache() error {
	dirs := []string{
		iconCacheDir,
		dashboardIconsCacheDir,
		simpleIconsCacheDir,
		urlCacheDir,
		userIconsDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create icon cache directory %s: %w", dir, err)
		}
	}

	return nil
}

// parseIconIdentifier determines the type and value of an icon identifier
func parseIconIdentifier(identifier string) (IconType, string) {
	if identifier == "" {
		return IconTypeUnknown, ""
	}

	// Check for simple-icon prefix (si:nginx)
	if name, ok := strings.CutPrefix(identifier, "si:"); ok {
		return IconTypeSimpleIcon, name
	}
	if name, ok := strings.CutPrefix(identifier, "simple-icon:"); ok {
		return IconTypeSimpleIcon, name
	}

	// Check for dashboard-icon prefix (di:nginx)
	if name, ok := strings.CutPrefix(identifier, "di:"); ok {
		return IconTypeDashboardIcon, name
	}
	if name, ok := strings.CutPrefix(identifier, "dashboard-icon:"); ok {
		return IconTypeDashboardIcon, name
	}

	// Check for HTTP(S) URL
	if strings.HasPrefix(identifier, "http://") || strings.HasPrefix(identifier, "https://") {
		return IconTypeURL, identifier
	}

	// Check for file path (ends with image extension)
	lowerIdent := strings.ToLower(identifier)
	if strings.HasSuffix(lowerIdent, ".svg") || strings.HasSuffix(lowerIdent, ".png") ||
		strings.HasSuffix(lowerIdent, ".jpg") || strings.HasSuffix(lowerIdent, ".jpeg") ||
		strings.HasSuffix(lowerIdent, ".webp") {
		return IconTypeFile, identifier
	}

	// Otherwise, treat as a name to derive (will try dashboard-icons first)
	return IconTypeDerived, identifier
}

// cacheEntry is a cached icon or a remembered miss. mtime is when the answer
// was last confirmed upstream; atime is when it was last served.
type cacheEntry struct {
	path    string
	modTime time.Time
	missing bool
	stale   bool
}

// urlCacheName hashes a URL into a cache filename stem.
func urlCacheName(url string) string {
	hash := sha256.Sum256([]byte(url))
	return fmt.Sprintf("%x", hash[:8])
}

// missingMarkerPath is where a miss for the identifier is recorded.
func missingMarkerPath(iconType IconType, identifier string) string {
	switch iconType {
	case IconTypeSimpleIcon:
		return filepath.Join(simpleIconsCacheDir, identifier+missingIconExt)
	case IconTypeURL:
		return filepath.Join(urlCacheDir, urlCacheName(identifier)+missingIconExt)
	default:
		return filepath.Join(dashboardIconsCacheDir, identifier+missingIconExt)
	}
}

// cacheCandidates lists the paths that may hold the identifier, icons first
// and the miss marker last.
func cacheCandidates(iconType IconType, identifier string) []string {
	var dashboard []string
	for _, ext := range []string{".svg", ".png", ".webp"} {
		dashboard = append(dashboard, filepath.Join(dashboardIconsCacheDir, identifier+ext))
	}
	simple := filepath.Join(simpleIconsCacheDir, identifier+".svg")

	switch iconType {
	case IconTypeDashboardIcon:
		return append(dashboard, missingMarkerPath(iconType, identifier))
	case IconTypeSimpleIcon:
		return []string{simple, missingMarkerPath(iconType, identifier)}
	case IconTypeURL:
		return []string{filepath.Join(urlCacheDir, urlCacheName(identifier)+".svg"), missingMarkerPath(iconType, identifier)}
	case IconTypeDerived:
		return append(dashboard, simple, missingMarkerPath(iconType, identifier))
	default:
		return nil
	}
}

// lookupCachedIcon finds the cached answer for the identifier, if any.
func lookupCachedIcon(ctx context.Context, iconType IconType, identifier string) (cacheEntry, bool) {
	if ctx.Err() != nil {
		return cacheEntry{}, false
	}
	for _, path := range cacheCandidates(iconType, identifier) {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		return cacheEntry{
			path:    path,
			modTime: info.ModTime(),
			missing: strings.HasSuffix(path, missingIconExt),
			stale:   time.Since(info.ModTime()) > iconRefreshInterval,
		}, true
	}
	return cacheEntry{}, false
}

// touchCacheEntry records that the entry was served now (atime) while keeping
// its confirmation time (mtime); explicit utimes works on noatime mounts.
func touchCacheEntry(entry cacheEntry) {
	if err := os.Chtimes(entry.path, time.Now(), entry.modTime); err != nil {
		slog.Debug("failed to touch cached icon", "path", entry.path, "error", err)
	}
}

// lastUsed is when the file was last served or downloaded, whichever is later.
func lastUsed(info os.FileInfo) time.Time {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		if atime := time.Unix(st.Atim.Sec, st.Atim.Nsec); atime.After(info.ModTime()) {
			return atime
		}
	}
	return info.ModTime()
}

func remoteIconCacheDirs() []string {
	return []string{dashboardIconsCacheDir, simpleIconsCacheDir, urlCacheDir}
}

// sweepIconCache deletes remote cache entries not served since cutoff.
func sweepIconCache(ctx context.Context, cutoff time.Time) {
	for _, dir := range remoteIconCacheDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || !lastUsed(info).Before(cutoff) {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if err := os.Remove(path); err != nil {
				slog.Warn("failed to remove unused icon", "component", "docker", "subsystem", "icons", "path", path, "error", err)
				continue
			}
			slog.Debug("removed unused icon", "path", path)
		}
	}
}

// maybeSweepIconCache runs the sweep at most once per iconSweepInterval per
// process, on the request path so no goroutine is needed.
func maybeSweepIconCache(ctx context.Context) {
	now := time.Now()
	last := lastIconSweep.Load()
	if now.Unix()-last < int64(iconSweepInterval/time.Second) || !lastIconSweep.CompareAndSwap(last, now.Unix()) {
		return
	}
	sweepIconCache(ctx, now.Add(-iconUnusedTTL))
}

// fetchDashboardIcon downloads an icon from Dashboard Icons CDN (homarr-labs)
func fetchDashboardIcon(ctx context.Context, name string) ([]byte, error) {
	var errs []error
	candidates := []string{
		fmt.Sprintf("%s/svg/%s.svg", dashboardIconsRawBase, name),
		fmt.Sprintf("%s/png/%s.png", dashboardIconsRawBase, name),
	}

	for _, url := range candidates {
		slog.Debug("fetching dashboard icon", "name", name, "url", url)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to build %s request: %w", url, err))
			continue
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to fetch %s: %w", url, err))
			continue
		}

		data, readErr := func() ([]byte, error) {
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusNotFound {
				return nil, fmt.Errorf("%s: %w", url, errIconNotFound)
			}
			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("%s returned status %d", url, resp.StatusCode)
			}

			data, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to read %s: %w", url, err)
			}

			return data, nil
		}()
		if readErr != nil {
			errs = append(errs, readErr)
			continue
		}

		return data, nil
	}

	return nil, fmt.Errorf("dashboard icon %q: %w", name, joinFetchErrors(errs...))
}

// joinFetchErrors combines per-source fetch errors so the result matches
// errIconNotFound only when every source reported a definite 404; otherwise
// the misses are dropped and only the outages are kept.
func joinFetchErrors(errs ...error) error {
	var outages []error
	for _, err := range errs {
		if !errors.Is(err, errIconNotFound) {
			outages = append(outages, err)
		}
	}
	if len(outages) == 0 {
		return errors.Join(errs...)
	}
	return errors.Join(outages...)
}

// fetchSimpleIcon downloads an icon from Simple Icons CDN
func fetchSimpleIcon(ctx context.Context, name string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s", simpleIconsCDN, name)
	slog.Debug("fetching simple icon", "name", name, "url", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build icon request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch icon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("simple icon %q: %w", name, errIconNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("simple icon %q: status %d", name, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read icon data: %w", err)
	}

	return data, nil
}

// fetchURLIcon downloads an icon from an arbitrary URL
func fetchURLIcon(ctx context.Context, url string) ([]byte, error) {
	slog.Debug("fetching icon from URL", "url", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build icon request: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch icon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("%s: %w", url, errIconNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch icon: status %d", resp.StatusCode)
	}

	// Read data with size limit (prevent huge downloads)
	limitedReader := io.LimitReader(resp.Body, 5*1024*1024) // 5MB max
	data, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read icon data: %w", err)
	}

	return data, nil
}

// cacheIcon saves icon data to the cache
func cacheIcon(iconType IconType, identifier string, data []byte) error {
	var cachePath string

	switch iconType {
	case IconTypeDashboardIcon:
		cachePath = filepath.Join(dashboardIconsCacheDir, identifier+detectIconExtension(data))
	case IconTypeSimpleIcon:
		cachePath = filepath.Join(simpleIconsCacheDir, identifier+".svg")
	case IconTypeURL:
		cachePath = filepath.Join(urlCacheDir, urlCacheName(identifier)+".svg")
	case IconTypeDerived:
		cachePath = filepath.Join(dashboardIconsCacheDir, identifier+detectIconExtension(data))
	default:
		return fmt.Errorf("cannot cache icon of type %s", iconType)
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Write to cache
	if err := os.WriteFile(cachePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}
	// The icon exists now, so forget any earlier miss.
	_ = os.Remove(missingMarkerPath(iconType, identifier))
	slog.Debug("cached icon", "type", iconType, "identifier", identifier, "path", cachePath)
	return nil
}

// cacheMissingIcon remembers that no upstream source has the identifier.
func cacheMissingIcon(iconType IconType, identifier string) error {
	path := missingMarkerPath(iconType, identifier)
	if err := os.WriteFile(path, nil, 0644); err != nil {
		return fmt.Errorf("failed to write miss marker: %w", err)
	}
	slog.Debug("cached missing icon", "type", iconType, "identifier", identifier, "path", path)
	return nil
}

func detectIconExtension(data []byte) string {
	snippet := string(data[:min(512, len(data))])
	if strings.Contains(snippet, "<svg") {
		return ".svg"
	}

	switch http.DetectContentType(data) {
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	default:
		return ".svg"
	}
}

// GetIcon retrieves an icon by identifier, fetching and caching if necessary
func GetIcon(ctx context.Context, identifier string) ([]byte, error) {
	if identifier == "" {
		return nil, fmt.Errorf("empty icon identifier")
	}

	// Initialize cache directories
	if err := initIconCache(); err != nil {
		slog.Warn("failed to initialize icon cache", "component", "docker", "subsystem", "icons", "identifier", identifier, "error", err)
	}

	maybeSweepIconCache(ctx)

	// Parse identifier
	iconType, value := parseIconIdentifier(identifier)
	if iconType == IconTypeFile {
		// User files are never fetched or cached.
		data, err := os.ReadFile(filepath.Join(userIconsDir, value))
		if err != nil {
			return nil, fmt.Errorf("failed to read user icon: %w", err)
		}
		return data, nil
	}

	// A fresh cached answer, icon or miss, is served without any network.
	entry, found := lookupCachedIcon(ctx, iconType, value)
	if found && !entry.stale {
		if data, err := readCachedIcon(entry, identifier); err == nil {
			return data, nil
		} else if entry.missing {
			return nil, err
		}
	}

	// No network fallback: on error the frontend renders its bundled Docker icon.
	data, fetchedType, err := fetchIconData(ctx, iconType, value)
	if err == nil {
		if cacheErr := cacheIcon(fetchedType, value, data); cacheErr != nil {
			slog.Warn("failed to cache icon", "component", "docker", "subsystem", "icons", "identifier", identifier, "error", cacheErr)
		}
		return data, nil
	}

	notFound := errors.Is(err, errIconNotFound)
	if found {
		// Upstream gone or unreachable: the last answer beats none. A definite
		// 404 re-confirms it for another interval; an outage leaves it stale.
		if notFound {
			entry.modTime = time.Now()
		}
		slog.Debug("serving stale cached icon", "identifier", identifier, "error", err)
		return readCachedIcon(entry, identifier)
	}
	if notFound {
		if cacheErr := cacheMissingIcon(iconType, value); cacheErr != nil {
			slog.Warn("failed to cache missing icon", "component", "docker", "subsystem", "icons", "identifier", identifier, "error", cacheErr)
		}
	}
	return nil, fmt.Errorf("failed to fetch icon: %w", err)
}

// readCachedIcon serves a cache entry and records the use; a miss marker
// yields errIconNotFound.
func readCachedIcon(entry cacheEntry, identifier string) ([]byte, error) {
	touchCacheEntry(entry)
	if entry.missing {
		return nil, fmt.Errorf("icon %q: %w", identifier, errIconNotFound)
	}
	data, err := os.ReadFile(entry.path)
	if err != nil {
		return nil, fmt.Errorf("failed to read cached icon %s: %w", entry.path, err)
	}
	slog.Debug("serving cached icon", "identifier", identifier, "path", entry.path)
	return data, nil
}

// fetchIconData downloads the icon; the returned type is where it is cached.
func fetchIconData(ctx context.Context, iconType IconType, value string) ([]byte, IconType, error) {
	switch iconType {
	case IconTypeDashboardIcon:
		data, err := fetchDashboardIcon(ctx, value)
		return data, iconType, err
	case IconTypeSimpleIcon:
		data, err := fetchSimpleIcon(ctx, value)
		return data, iconType, err
	case IconTypeURL:
		data, err := fetchURLIcon(ctx, value)
		return data, iconType, err
	case IconTypeDerived:
		return fetchDerivedIcon(ctx, value)
	default:
		return nil, iconType, fmt.Errorf("unknown icon type: %s", iconType)
	}
}

func fetchDerivedIcon(ctx context.Context, value string) ([]byte, IconType, error) {
	data, dashboardErr := fetchDashboardIcon(ctx, value)
	if dashboardErr == nil {
		return data, IconTypeDerived, nil
	}
	slog.Debug("dashboard icon not found, trying simple icons", "identifier", value)
	data, simpleErr := fetchSimpleIcon(ctx, value)
	if simpleErr == nil {
		return data, IconTypeSimpleIcon, nil
	}
	return nil, IconTypeDerived, fmt.Errorf("icon %q: %w", value, joinFetchErrors(dashboardErr, simpleErr))
}

// GetIconURI retrieves an icon and returns it as a base64 data URI
func GetIconURI(ctx context.Context, identifier string) (string, error) {
	data, err := GetIcon(ctx, identifier)
	if err != nil {
		return "", err
	}

	// Detect content type from data
	contentType := http.DetectContentType(data)

	// If it's SVG, use svg+xml
	if strings.Contains(string(data[:min(512, len(data))]), "<svg") {
		contentType = "image/svg+xml"
	}

	// Encode as base64
	encoded := base64.StdEncoding.EncodeToString(data)

	// Return data URI
	return fmt.Sprintf("data:%s;base64,%s", contentType, encoded), nil
}

// GetIconInfo returns metadata about an icon without fetching it
func GetIconInfo(ctx context.Context, identifier string) IconInfo {
	iconType, value := parseIconIdentifier(identifier)
	entry, found := lookupCachedIcon(ctx, iconType, value)

	return IconInfo{
		Type:       iconType,
		Identifier: value,
		Cached:     found && !entry.missing,
	}
}

// ClearIconCache removes all cached icons
func ClearIconCache(ctx context.Context) error {
	slog.Info("clearing icon cache")

	for _, dir := range remoteIconCacheDirs() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("failed to remove cache directory %s: %w", dir, err)
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	// Recreate directories
	return initIconCache()
}

// ResolveIconIdentifier resolves an explicit icon identifier, then falls back to the service/container name.
func ResolveIconIdentifier(iconValue, serviceName string) string {
	// If icon is explicitly set, use it
	if iconValue != "" {
		return iconValue
	}

	// Fallback to service/container name
	if serviceName != "" {
		return strings.ToLower(serviceName)
	}

	return ""
}
