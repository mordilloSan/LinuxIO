package docker

import (
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mordilloSan/go-logger/logger"
)

const (
	iconCacheDir           = "/run/linuxio/icons"
	dashboardIconsCacheDir = "/run/linuxio/icons/dashboard-icons"
	simpleIconsCacheDir    = "/run/linuxio/icons/simple-icons"
	urlCacheDir            = "/run/linuxio/icons/url-cache"
	userIconsDir           = "/run/linuxio/icons/user"

	// Dashboard Icons CDN (homarr-labs)
	dashboardIconsCDN = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons@main/svg"
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
	if strings.HasPrefix(identifier, "si:") || strings.HasPrefix(identifier, "simple-icon:") {
		name := strings.TrimPrefix(identifier, "si:")
		name = strings.TrimPrefix(name, "simple-icon:")
		return IconTypeSimpleIcon, name
	}

	// Check for dashboard-icon prefix (di:nginx)
	if strings.HasPrefix(identifier, "di:") || strings.HasPrefix(identifier, "dashboard-icon:") {
		name := strings.TrimPrefix(identifier, "di:")
		name = strings.TrimPrefix(name, "dashboard-icon:")
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
		cachePath = filepath.Join(dashboardIconsCacheDir, identifier+".svg")
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
		// Try dashboard-icons cache first
		cachePath = filepath.Join(dashboardIconsCacheDir, identifier+".svg")
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

// fetchDashboardIcon downloads an icon from Dashboard Icons CDN (homarr-labs)
func fetchDashboardIcon(name string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s.svg", dashboardIconsCDN, name)
	logger.DebugKV("fetching dashboard icon", "name", name, "url", url)

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

// fetchSimpleIcon downloads an icon from Simple Icons CDN
func fetchSimpleIcon(name string) ([]byte, error) {
	url := fmt.Sprintf("%s/%s", simpleIconsCDN, name)
	logger.DebugKV("fetching simple icon", "name", name, "url", url)

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
	logger.DebugKV("fetching icon from URL", "url", url)

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
		cachePath = filepath.Join(dashboardIconsCacheDir, identifier+".svg")
	case IconTypeSimpleIcon:
		cachePath = filepath.Join(simpleIconsCacheDir, identifier+".svg")
	case IconTypeURL:
		hash := sha256.Sum256([]byte(identifier))
		hashStr := fmt.Sprintf("%x", hash[:8])
		cachePath = filepath.Join(urlCacheDir, hashStr+".svg")
	case IconTypeDerived:
		cachePath = filepath.Join(dashboardIconsCacheDir, identifier+".svg")
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

	logger.DebugKV("cached icon", "type", iconType, "identifier", identifier, "path", cachePath)
	return nil
}

// GetIcon retrieves an icon by identifier, fetching and caching if necessary
func GetIcon(identifier string) ([]byte, error) {
	if identifier == "" {
		return nil, fmt.Errorf("empty icon identifier")
	}

	// Initialize cache directories
	if err := initIconCache(); err != nil {
		logger.Warnf("failed to initialize icon cache: %v", err)
	}

	// Parse identifier
	iconType, value := parseIconIdentifier(identifier)

	// Check cache first
	if cachePath, found := getCachedIcon(iconType, value); found {
		data, err := os.ReadFile(cachePath)
		if err == nil {
			logger.DebugKV("serving cached icon", "type", iconType, "identifier", identifier)
			return data, nil
		}
		logger.Warnf("failed to read cached icon: %v", err)
	}

	// Fetch icon based on type
	var data []byte
	var err error

	switch iconType {
	case IconTypeDashboardIcon:
		// Explicit dashboard icon request
		data, err = fetchDashboardIcon(value)
		if err != nil {
			// Fall back to Docker icon
			logger.DebugKV("dashboard icon not found, falling back to docker icon", "identifier", value)
			data, err = fetchDashboardIcon("docker")
			if err == nil {
				value = "docker"
			}
		}

	case IconTypeSimpleIcon:
		// Explicit simple icon request
		data, err = fetchSimpleIcon(value)
		if err != nil {
			// Fall back to Docker icon from dashboard-icons
			logger.DebugKV("simple icon not found, falling back to docker icon", "identifier", value)
			data, err = fetchDashboardIcon("docker")
			if err == nil {
				iconType = IconTypeDashboardIcon
				value = "docker"
			}
		}

	case IconTypeURL:
		data, err = fetchURLIcon(value)
		if err != nil {
			// Fall back to Docker icon
			logger.DebugKV("url icon fetch failed, falling back to docker icon", "url", value, "error", err)
			data, err = fetchDashboardIcon("docker")
			if err == nil {
				iconType = IconTypeDashboardIcon
				value = "docker"
			}
		}

	case IconTypeFile:
		// Read from user icons directory
		filePath := filepath.Join(userIconsDir, value)
		data, err = os.ReadFile(filePath)

	case IconTypeDerived:
		// Try Dashboard Icons first (best for container names)
		data, err = fetchDashboardIcon(value)
		if err != nil {
			logger.DebugKV("dashboard icon not found, trying simple icons", "identifier", value)
			// Try Simple Icons as fallback
			data, err = fetchSimpleIcon(value)
			if err != nil {
				// Final fallback: Docker icon
				logger.DebugKV("simple icon not found, falling back to docker icon", "identifier", value)
				data, err = fetchDashboardIcon("docker")
				if err == nil {
					value = "docker"
				}
			} else {
				// Found in Simple Icons - cache as simple icon
				iconType = IconTypeSimpleIcon
			}
		}

	default:
		return nil, fmt.Errorf("unknown icon type: %s", iconType)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to fetch icon: %w", err)
	}

	// Cache the icon
	if cacheErr := cacheIcon(iconType, value, data); cacheErr != nil {
		logger.Warnf("failed to cache icon: %v", cacheErr)
	}

	return data, nil
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
	logger.InfoKV("clearing icon cache")

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
