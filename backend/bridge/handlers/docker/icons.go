package docker

import (
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
	"time"
)

const (
	iconCacheDir           = "/run/linuxio/icons"
	dashboardIconsCacheDir = "/run/linuxio/icons/dashboard-icons"
	simpleIconsCacheDir    = "/run/linuxio/icons/simple-icons"
	urlCacheDir            = "/run/linuxio/icons/url-cache"
	userIconsDir           = "/run/linuxio/icons/user"

	// Dashboard Icons repository (homarr-labs). Some icons are SVG, others PNG.
	dashboardIconsRawBase = "https://raw.githubusercontent.com/homarr-labs/dashboard-icons/main"
	// Simple Icons CDN - for brand icons via si: prefix
	simpleIconsCDN = "https://cdn.simpleicons.org"

	iconCacheDuration = 24 * time.Hour
	httpClientTimeout = 10 * time.Second
)

var httpClient = &http.Client{
	Timeout: httpClientTimeout,
}

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

// getCachedIcon checks if an icon is cached and returns its path
func getCachedIcon(iconType IconType, identifier string) (string, bool) {
	var cachePath string

	switch iconType {
	case IconTypeDashboardIcon:
		return getDashboardCachedIcon(identifier)
	case IconTypeSimpleIcon:
		cachePath = filepath.Join(simpleIconsCacheDir, identifier+".svg")
	case IconTypeURL:
		// Hash the URL to create a filename
		hash := sha256.Sum256([]byte(identifier))
		hashStr := fmt.Sprintf("%x", hash[:8])
		cachePath = filepath.Join(urlCacheDir, hashStr+".svg")
	case IconTypeFile:
		cachePath = filepath.Join(userIconsDir, identifier)
	case IconTypeDerived:
		return getDashboardCachedIcon(identifier)
	default:
		return "", false
	}

	// Check if file exists and is not too old
	info, err := os.Stat(cachePath)
	if err != nil {
		return "", false
	}

	// Check cache age
	if time.Since(info.ModTime()) > iconCacheDuration {
		return "", false
	}

	return cachePath, true
}

func getDashboardCachedIcon(identifier string) (string, bool) {
	for _, ext := range []string{".svg", ".png", ".webp"} {
		cachePath := filepath.Join(dashboardIconsCacheDir, identifier+ext)
		info, err := os.Stat(cachePath)
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) > iconCacheDuration {
			continue
		}
		return cachePath, true
	}
	return "", false
}

// fetchDashboardIcon downloads an icon from Dashboard Icons CDN (homarr-labs)
func fetchDashboardIcon(name string) ([]byte, error) {
	var errs []error
	candidates := []string{
		fmt.Sprintf("%s/svg/%s.svg", dashboardIconsRawBase, name),
		fmt.Sprintf("%s/png/%s.png", dashboardIconsRawBase, name),
	}

	for _, url := range candidates {
		slog.Debug("fetching dashboard icon", "name", name, "url", url)

		resp, err := httpClient.Get(url)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to fetch %s: %w", url, err))
			continue
		}

		data, readErr := func() ([]byte, error) {
			defer resp.Body.Close()

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

	return nil, errors.Join(errs...)
}

// fetchSimpleIcon downloads an icon from Simple Icons CDN
func fetchSimpleIcon(name string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s", simpleIconsCDN, name)
	slog.Debug("fetching simple icon", "name", name, "url", url)

	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch icon: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("icon not found: status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read icon data: %w", err)
	}

	return data, nil
}

// fetchURLIcon downloads an icon from an arbitrary URL
func fetchURLIcon(url string) ([]byte, error) {
	slog.Debug("fetching icon from URL", "url", url)

	resp, err := httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch icon: %w", err)
	}
	defer resp.Body.Close()

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
		hash := sha256.Sum256([]byte(identifier))
		hashStr := fmt.Sprintf("%x", hash[:8])
		cachePath = filepath.Join(urlCacheDir, hashStr+".svg")
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
	slog.Debug("cached icon", "type", iconType, "identifier", identifier, "path", cachePath)
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
func GetIcon(identifier string) ([]byte, error) {
	if identifier == "" {
		return nil, fmt.Errorf("empty icon identifier")
	}

	// Initialize cache directories
	if err := initIconCache(); err != nil {
		slog.Warn("failed to initialize icon cache", "component", "docker", "subsystem", "icons", "identifier", identifier, "error", err)
	}

	// Parse identifier
	iconType, value := parseIconIdentifier(identifier)

	// Check cache first
	if data, found := readCachedIcon(iconType, value, identifier); found {
		return data, nil
	}

	data, iconType, value, err := fetchIconData(iconType, value)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch icon: %w", err)
	}

	// Cache the icon
	if cacheErr := cacheIcon(iconType, value, data); cacheErr != nil {
		slog.Warn("failed to cache icon", "component", "docker", "subsystem", "icons", "identifier", identifier, "error", cacheErr)
	}

	return data, nil
}

func readCachedIcon(iconType IconType, value, identifier string) ([]byte, bool) {
	cachePath, found := getCachedIcon(iconType, value)
	if !found {
		return nil, false
	}

	data, err := os.ReadFile(cachePath)
	if err != nil {
		slog.Warn("failed to read cached icon", "component", "docker", "subsystem", "icons", "identifier", identifier, "path", cachePath, "error", err)
		return nil, false
	}
	slog.Debug("serving cached icon", "type", iconType, "identifier", identifier)
	return data, true
}

func fetchIconData(iconType IconType, value string) ([]byte, IconType, string, error) {
	switch iconType {
	case IconTypeDashboardIcon:
		return fetchDashboardIconWithFallback(value)
	case IconTypeSimpleIcon:
		return fetchSimpleIconWithFallback(value)
	case IconTypeURL:
		return fetchURLIconWithFallback(value)
	case IconTypeFile:
		data, err := os.ReadFile(filepath.Join(userIconsDir, value))
		return data, iconType, value, err
	case IconTypeDerived:
		return fetchDerivedIcon(value)
	default:
		return nil, iconType, value, fmt.Errorf("unknown icon type: %s", iconType)
	}
}

func fetchDashboardIconWithFallback(value string) ([]byte, IconType, string, error) {
	data, err := fetchDashboardIcon(value)
	if err == nil {
		return data, IconTypeDashboardIcon, value, nil
	}
	slog.Debug("dashboard icon not found, falling back to docker icon", "identifier", value)
	return fetchDockerFallback(err)
}

func fetchSimpleIconWithFallback(value string) ([]byte, IconType, string, error) {
	data, err := fetchSimpleIcon(value)
	if err == nil {
		return data, IconTypeSimpleIcon, value, nil
	}
	slog.Debug("simple icon not found, falling back to docker icon", "identifier", value)
	return fetchDockerFallback(err)
}

func fetchURLIconWithFallback(value string) ([]byte, IconType, string, error) {
	data, err := fetchURLIcon(value)
	if err == nil {
		return data, IconTypeURL, value, nil
	}
	slog.Debug("url icon fetch failed, falling back to docker icon", "url", value, "error", err)
	return fetchDockerFallback(err)
}

func fetchDerivedIcon(value string) ([]byte, IconType, string, error) {
	data, err := fetchDashboardIcon(value)
	if err == nil {
		return data, IconTypeDerived, value, nil
	}
	slog.Debug("dashboard icon not found, trying simple icons", "identifier", value)
	data, err = fetchSimpleIcon(value)
	if err == nil {
		return data, IconTypeSimpleIcon, value, nil
	}
	slog.Debug("simple icon not found, falling back to docker icon", "identifier", value)
	return fetchDockerFallback(err)
}

func fetchDockerFallback(sourceErr error) ([]byte, IconType, string, error) {
	data, err := fetchDashboardIcon("docker")
	if err != nil {
		return nil, IconTypeDashboardIcon, "docker", errors.Join(sourceErr, err)
	}
	return data, IconTypeDashboardIcon, "docker", nil
}

// GetIconURI retrieves an icon and returns it as a base64 data URI
func GetIconURI(identifier string) (string, error) {
	data, err := GetIcon(identifier)
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
func GetIconInfo(identifier string) IconInfo {
	iconType, value := parseIconIdentifier(identifier)
	_, cached := getCachedIcon(iconType, value)

	return IconInfo{
		Type:       iconType,
		Identifier: value,
		Cached:     cached,
	}
}

// ClearIconCache removes all cached icons
func ClearIconCache() error {
	slog.Info("clearing icon cache")

	dirs := []string{dashboardIconsCacheDir, simpleIconsCacheDir, urlCacheDir}
	for _, dir := range dirs {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("failed to remove cache directory %s: %w", dir, err)
		}
	}

	// Recreate directories
	return initIconCache()
}

// ResolveIconIdentifier resolves an icon identifier with fallback to image name, then service/container name
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
