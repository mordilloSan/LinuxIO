package userconfig

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Value rules shared by disk validation and API payload application.

// Accepted UIPreferences.ViewModes values.
const (
	ViewModeCard  = "card"
	ViewModeTable = "table"
)

// ParseTheme normalizes s and reports whether it is LIGHT or DARK.
func ParseTheme(s string) (PersistedTheme, bool) {
	theme := PersistedTheme(strings.ToUpper(strings.TrimSpace(s)))
	return theme, theme == ThemeLight || theme == ThemeDark
}

// ParseNavigationMode normalizes s and reports whether it is a known mode.
func ParseNavigationMode(s string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(s))
	return mode, mode == NavigationModeSidebar || mode == NavigationModeDock
}

// ParseDockTileColors normalizes s and reports whether it is a known palette.
func ParseDockTileColors(s string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(s))
	return mode, isValidDockTileColors(mode)
}

// ParseViewMode normalizes s and reports whether it is card or table.
func ParseViewMode(s string) (string, bool) {
	mode := strings.ToLower(strings.TrimSpace(s))
	return mode, mode == ViewModeCard || mode == ViewModeTable
}

// ValidChunkSizeMB reports whether v is 0 (default) or 1-32 MiB.
func ValidChunkSizeMB(v int) bool { return v == 0 || (v >= 1 && v <= 32) }

// ValidTerminalFontSize reports whether v is within the supported range.
func ValidTerminalFontSize(v int) bool { return v >= 10 && v <= 28 }

// ParseDockerFolders cleans and validates Docker folder paths. They must be
// structurally valid (absolute, not root, unique) but need not exist.
func ParseDockerFolders[S ~string](values []S) ([]AbsolutePath, error) {
	if len(values) == 0 {
		return nil, errors.New("docker folders cannot be empty")
	}
	folders := make([]AbsolutePath, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		input := strings.TrimSpace(string(value))
		if input == "" {
			return nil, errors.New("docker folders cannot include an empty path")
		}
		folder := filepath.Clean(input)
		if !filepath.IsAbs(folder) {
			return nil, errors.New("docker folder must be an absolute path")
		}
		if folder == string(filepath.Separator) {
			return nil, errors.New("docker folder cannot be root")
		}
		if _, exists := seen[folder]; exists {
			return nil, errors.New("docker folders cannot include duplicates")
		}
		seen[folder] = struct{}{}
		folders = append(folders, AbsolutePath(folder))
	}
	return folders, nil
}

// ValidateJobSettings returns an error message per negative job setting.
func ValidateJobSettings(jobs PersistedJobSettings) []string {
	var errs []string
	for _, field := range []struct {
		name  string
		value int
	}{
		{"jobs.progressMinIntervalMs", jobs.ProgressMinIntervalMs},
		{"jobs.notificationMinIntervalMs", jobs.NotificationMinIntervalMs},
		{"jobs.progressMinBytesMB", jobs.ProgressMinBytesMB},
		{"jobs.heavyArchiveConcurrency", jobs.HeavyArchiveConcurrency},
		{"jobs.archiveCompressionWorkers", jobs.ArchiveCompressionWorkers},
		{"jobs.archiveExtractWorkers", jobs.ArchiveExtractWorkers},
	} {
		if field.value < 0 {
			errs = append(errs, fmt.Sprintf("%s must be >= 0", field.name))
		}
	}
	return errs
}
