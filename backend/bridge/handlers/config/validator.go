package config

import (
	"errors"
	"os"
	"strings"

	"github.com/goccy/go-yaml"

	"github.com/mordilloSan/go_logger/v2/logger"
)

// repairConfig loads cfgPath, validates keys/values, and rewrites only if needed.
// If the YAML cannot be parsed at all (including validation errors from custom types),
// it rewrites full defaults using base.
func repairConfig(cfgPath, base string) error {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}

	// Try strict parsing - custom types handle validation automatically
	var cfg Settings
	if err := yaml.UnmarshalWithOptions(raw, &cfg, yaml.Strict()); err != nil {
		// Extract detailed error info from goccy/go-yaml
		logYAMLError(err, cfgPath)
		logger.Warnf("config validation failed, rewriting defaults")
		return writeConfig(cfgPath, base)
	}

	// Check if Docker.Folder exists but is a file (edge case)
	if fi, statErr := os.Stat(string(cfg.Docker.Folder)); statErr == nil && !fi.IsDir() {
		logger.Warnf("docker.folder %q exists as file, resetting to default", cfg.Docker.Folder)
		cfg.Docker.Folder = DefaultDocker(base).Folder
		return writeConfigFrom(cfgPath, cfg)
	}

	// Config is valid, nothing to repair
	return nil
}

// logYAMLError extracts and logs detailed error information from goccy/go-yaml
func logYAMLError(err error, path string) {
	// Try to extract syntax error with position info
	var syntaxErr *yaml.SyntaxError
	if errors.As(err, &syntaxErr) {
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

	// Docker.Folder validation
	folder := strings.TrimSpace(string(cfg.Docker.Folder))
	if folder == "" {
		errs = append(errs, "docker.folder cannot be empty")
	}

	return errs
}
