package config

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/internal/logger"
	"gopkg.in/yaml.v3"
)

// repairConfig loads cfgPath, validates keys/values, and rewrites only if needed.
// If the YAML cannot be parsed at all, it rewrites full defaults using base.
func repairConfig(cfgPath, base string) error {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}

	// 1) Attempt to parse into struct (lenient; we’ll detect unknown keys separately)
	var cfg Settings
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		logger.Warnf("config parse failed, rewriting defaults: %v", err)
		return writeConfig(cfgPath, base)
	}

	// 2) Detect unknown keys (top-level + nested) for logging; they will be dropped on save.
	unknown := detectUnknownKeys(raw)

	// 3) Sanitize fields (repair invalid/missing values to defaults)
	changed, fixes := sanitizeSettings(&cfg, base)

	// 4) If anything changed or unknown keys exist, rewrite repaired config
	if changed || len(unknown) > 0 {
		if len(unknown) > 0 {
			logger.Warnf("unknown config keys will be removed on rewrite: %s", strings.Join(unknown, ", "))
		}
		if len(fixes) > 0 {
			logger.Warnf("config repairs applied: %s", strings.Join(fixes, "; "))
		}
		return writeConfigFrom(cfgPath, cfg)
	}

	// Nothing to change
	return nil
}

// sanitizeSettings fixes invalid/missing fields in-place and returns whether anything changed.
func sanitizeSettings(cfg *Settings, base string) (changed bool, fixes []string) {
	// Defaults used for repair
	def := DefaultSettings(base)

	// Theme: LIGHT or DARK
	t := strings.ToUpper(strings.TrimSpace(cfg.AppSettings.Theme))
	if t != "LIGHT" && t != "DARK" {
		cfg.AppSettings.Theme = def.AppSettings.Theme
		changed = true
		fixes = append(fixes, "themeSettings.theme=default(DARK)")
	}

	// PrimaryColor: must be valid CSS color
	if !IsValidCSSColor(cfg.AppSettings.PrimaryColor) {
		cfg.AppSettings.PrimaryColor = def.AppSettings.PrimaryColor
		changed = true
		fixes = append(fixes, "themeSettings.primaryColor=default(#2196f3)")
	}

	// SidebarCollapsed is bool; zero-value is fine (no repair).

	// Docker.Folder: must be absolute; if empty/invalid, set default <base>/docker
	if p := strings.TrimSpace(cfg.Docker.Folder); p == "" || !filepath.IsAbs(p) {
		cfg.Docker.Folder = def.Docker.Folder
		changed = true
		fixes = append(fixes, "docker.folder=default(<base>/docker)")
	} else {
		// normalize for consistency
		cleaned := filepath.Clean(p)
		if cleaned != cfg.Docker.Folder {
			cfg.Docker.Folder = cleaned
			changed = true
			fixes = append(fixes, "docker.folder normalized")
		}
	}

	// Optional strictness: if Docker.Folder exists AND is a file, repair
	if fi, err := os.Stat(cfg.Docker.Folder); err == nil && !fi.IsDir() {
		cfg.Docker.Folder = def.Docker.Folder
		changed = true
		fixes = append(fixes, "docker.folder existed as file → default(<base>/docker)")
	}

	return changed, fixes
}

// detectUnknownKeys returns a list of unknown keys (top-level and nested) for logging.
// Unknown keys are effectively dropped when we re-marshal from the typed struct.
func detectUnknownKeys(raw []byte) []string {
	type anyMap = map[string]any

	var m anyMap
	if err := yaml.Unmarshal(raw, &m); err != nil {
		// parsing already handled by caller; return no unknowns here
		return nil
	}

	unknown := []string{}
	// Allowed schema
	topAllowed := map[string]struct{}{
		"themeSettings": {},
		"docker":        {},
	}
	themeAllowed := map[string]struct{}{
		"theme":            {},
		"primaryColor":     {},
		"sidebarCollapsed": {},
	}
	dockerAllowed := map[string]struct{}{
		"folder": {},
	}

	for k, v := range m {
		if _, ok := topAllowed[k]; !ok {
			unknown = append(unknown, k)
			continue
		}
		switch k {
		case "themeSettings":
			if mm, ok := v.(map[string]any); ok {
				for kk := range mm {
					if _, ok := themeAllowed[kk]; !ok {
						unknown = append(unknown, "themeSettings."+kk)
					}
				}
			} else {
				unknown = append(unknown, "themeSettings (expected map)")
			}
		case "docker":
			if mm, ok := v.(map[string]any); ok {
				for kk := range mm {
					if _, ok := dockerAllowed[kk]; !ok {
						unknown = append(unknown, "docker."+kk)
					}
				}
			} else {
				unknown = append(unknown, "docker (expected map)")
			}
		}
	}
	return unknown
}
