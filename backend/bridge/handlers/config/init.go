//go:generate go run ./generator.go

package config

import (
	"log/slog"
	"path/filepath"
)

const (
	cfgFileName = ".linuxio-config.yaml"
	filePerm    = 0o644 // file:  rw-r--r--
	dirPerm     = 0o755 // dir:   rwxr-xr-x
)

// Initialize prepares the per-user LinuxIO configuration file for `username`.
//
// Flow:
//  1. Resolve a base folder: Homedir(username) or writable fallback.
//  2. Build <base>/.linuxio-config.yaml.
//  3. If the file exists: repair in place. If not: create defaults.
//  4. Ensure file permissions to 0o644.
//  5. All logging happens here.
func Initialize(username string) error {
	// 1) Resolve base dir
	base, baseErr := Homedir(username)
	if baseErr != nil {
		slog.Warn("homedir not available, attempting fallback", "user", username, "error", baseErr)
		b, err := fallbackBase(username)
		if err != nil {
			slog.Error("fallback base resolution failed", "user", username, "error", err)
			return err
		}
		base = b
		slog.Info("using fallback config base", "user", username, "path", base)
	} else {
		slog.Debug("resolved home directory", "user", username, "path", base)
	}

	cfgPath := filepath.Join(base, cfgFileName)
	slog.Debug("resolved config path", "user", username, "path", cfgPath)

	// 2) Check existence
	exists, err := CheckConfig(cfgPath)
	if err != nil {
		slog.Error("check config failed", "path", cfgPath, "error", err)
		return err
	}

	// 3) Repair or create
	if exists {
		slog.Debug("existing config detected, validating", "user", username, "path", cfgPath)
		if err := repairConfig(cfgPath, base); err != nil {
			slog.Error("config repair failed", "user", username, "path", cfgPath, "error", err)
			return err
		}
		if err := ensureFilePerms(cfgPath, filePerm); err != nil {
			slog.Error("failed to set existing config permissions", "path", cfgPath, "error", err)
			return err
		}
		slog.Debug("existing config ready", "user", username, "path", cfgPath)
		return nil
	}
	// Create new with defaults (Docker.Folder = <base>/docker).
	slog.Info("new user detected, generating default config", "user", username)
	if err := writeConfig(cfgPath, base); err != nil {
		slog.Error("write default config failed", "path", cfgPath, "error", err)
		return err
	}
	if err := ensureFilePerms(cfgPath, filePerm); err != nil {
		slog.Error("failed to set new config permissions", "path", cfgPath, "error", err)
		return err
	}
	slog.Info("created default config", "user", username, "path", cfgPath)
	return nil
}

// EnsureConfigReady ensures config exists, repairs it, fixes ownership when root, and logs the final state.
// It **never returns an error** — safe to call directly from main().
func EnsureConfigReady(username string) {
	// Create/repair
	if err := Initialize(username); err != nil {
		slog.Warn("initialize config failed", "user", username, "error", err)
		return
	}

	// Strict load (determines the actual path, respecting fallbacks)
	cfg, cfgPath, err := Load(username)
	if err != nil {
		slog.Warn("load config failed", "user", username, "error", err)
		return
	}

	// If root, fix ownership of file and parent dir (no-op otherwise)
	if err := chownIfRoot(filepath.Dir(cfgPath), username); err != nil {
		slog.Warn("failed to ensure config directory ownership", "path", filepath.Dir(cfgPath), "user", username, "error", err)
	} else {
		slog.Debug("ensured config directory ownership", "path", filepath.Dir(cfgPath), "user", username)
	}
	if err := chownIfRoot(cfgPath, username); err != nil {
		slog.Warn("failed to ensure config file ownership", "path", cfgPath, "user", username, "error", err)
	} else {
		slog.Debug("ensured config file ownership", "path", cfgPath, "user", username)
	}
	slog.Info("config ready", "user", username)
	slog.Debug("config details",
		"path", cfgPath,
		"theme", cfg.AppSettings.Theme,
		"primary", cfg.AppSettings.PrimaryColor,
		"show_hidden", cfg.AppSettings.ShowHiddenFiles)
}
