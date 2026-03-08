package config

import (
	"errors"
	"os"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/mordilloSan/go-logger/logger"
)

// repairConfig loads cfgPath, validates keys/values, and rewrites only if needed.
// If the YAML cannot be parsed at all (including validation errors from custom types),
// it rewrites full defaults using base.
func repairConfig(cfgPath, base string) error {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}

	var cfg Settings
	defaults := DefaultSettings(base)
	changed, err := parseAndSanitizeConfig(raw, cfgPath, base, &cfg)
	if err != nil {
		return err
	}
	changed = repairInvalidConfigValues(&cfg, defaults) || changed
	changed = repairDockerFolderPath(&cfg, defaults) || changed

	if changed {
		return writeConfigFrom(cfgPath, cfg)
	}

	// Config is valid, nothing to repair
	return nil
}

func parseAndSanitizeConfig(raw []byte, cfgPath, base string, cfg *Settings) (bool, error) {
	if err := yaml.UnmarshalWithOptions(raw, cfg, yaml.Strict()); err != nil {
		if permissiveErr := yaml.Unmarshal(raw, cfg); permissiveErr != nil {
			logYAMLError(err, cfgPath)
			logger.Warnf("config validation failed, rewriting defaults")
			return false, writeConfig(cfgPath, base)
		}
		logger.Warnf("config contained unsupported fields; rewriting sanitized config")
		return true, nil
	}
	return false, nil
}

func repairInvalidConfigValues(cfg *Settings, defaults *Settings) bool {
	errs := ValidateConfig(cfg)
	if len(errs) == 0 {
		return false
	}
	logger.Warnf("config validation issues: %s", strings.Join(errs, "; "))

	changed := false
	if cfg.AppSettings.Theme != ThemeLight && cfg.AppSettings.Theme != ThemeDark {
		cfg.AppSettings.Theme = defaults.AppSettings.Theme
		changed = true
	}
	if !IsValidCSSColor(string(cfg.AppSettings.PrimaryColor)) {
		cfg.AppSettings.PrimaryColor = defaults.AppSettings.PrimaryColor
		changed = true
	}
	if themeColorsNeedReset(cfg.AppSettings.ThemeColors) {
		cfg.AppSettings.ThemeColors = nil
		changed = true
	}
	if strings.TrimSpace(string(cfg.Docker.Folder)) == "" {
		cfg.Docker.Folder = defaults.Docker.Folder
		changed = true
	}
	return changed
}

func themeColorsNeedReset(themeColors *ThemeColors) bool {
	if themeColors == nil {
		return false
	}
	for _, ptr := range []*CSSColor{
		themeColors.BackgroundDefault,
		themeColors.BackgroundPaper,
		themeColors.HeaderBackground,
		themeColors.FooterBackground,
		themeColors.SidebarBackground,
		themeColors.CardBackground,
	} {
		if ptr != nil && !IsValidCSSColor(string(*ptr)) {
			return true
		}
	}
	return false
}

func repairDockerFolderPath(cfg *Settings, defaults *Settings) bool {
	fi, err := os.Stat(string(cfg.Docker.Folder))
	if err != nil || fi.IsDir() {
		return false
	}
	logger.Warnf("docker.folder %q exists as file, resetting to default", cfg.Docker.Folder)
	cfg.Docker.Folder = defaults.Docker.Folder
	return true
}

// logYAMLError extracts and logs detailed error information from goccy/go-yaml
func logYAMLError(err error, path string) {
	// Try to extract syntax error with position info
	if syntaxErr, ok := errors.AsType[*yaml.SyntaxError](err); ok {
		if tok := syntaxErr.GetToken(); tok != nil {
			logger.Errorf("config error in %s at line %d, column %d: %s",
				path,
				tok.Position.Line,
				tok.Position.Column,
				syntaxErr.GetMessage())
			return
		}
		// SyntaxError without token
		logger.Errorf("config error in %s: %s", path, syntaxErr.GetMessage())
		return
	}

	// Fallback to generic error
	logger.Errorf("config error in %s: %v", path, err)
}

// ValidateConfig validates a Settings struct and returns detailed errors
func ValidateConfig(cfg *Settings) []string {
	var errs []string

	// Theme validation (already done by custom type, but for manual checks)
	if cfg.AppSettings.Theme != ThemeLight && cfg.AppSettings.Theme != ThemeDark {
		errs = append(errs, "appSettings.theme must be LIGHT or DARK")
	}

	// PrimaryColor validation
	if !IsValidCSSColor(string(cfg.AppSettings.PrimaryColor)) {
		errs = append(errs, "appSettings.primaryColor must be a valid CSS color")
	}

	// ThemeColors validation (all fields optional, but if set must be valid CSS colors)
	if tc := cfg.AppSettings.ThemeColors; tc != nil {
		for key, ptr := range map[string]*CSSColor{
			"backgroundDefault": tc.BackgroundDefault,
			"backgroundPaper":   tc.BackgroundPaper,
			"headerBackground":  tc.HeaderBackground,
			"footerBackground":  tc.FooterBackground,
			"sidebarBackground": tc.SidebarBackground,
			"cardBackground":    tc.CardBackground,
		} {
			if ptr != nil && !IsValidCSSColor(string(*ptr)) {
				errs = append(errs, "appSettings.themeColors."+key+" must be a valid CSS color")
			}
		}
	}

	// Docker.Folder validation
	folder := strings.TrimSpace(string(cfg.Docker.Folder))
	if folder == "" {
		errs = append(errs, "docker.folder cannot be empty")
	}

	return errs
}
