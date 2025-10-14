//go:generate go run ./generator.go

package userconfig

import (
	"path/filepath"

	"github.com/mordilloSan/LinuxIO/backend/common/logger"
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
		logger.Warnf("homedir not available for %q: %v — attempting fallback", username, baseErr)
		b, err := fallbackBase(username)
		if err != nil {
			logger.Errorf("fallback base resolution failed for %q: %v", username, err)
			return err
		}
		base = b
		logger.Infof("using fallback base for %q → %s", username, base)
	} else {
		logger.Debugf("resolved home for %q → %s", username, base)
	}

	cfgPath := filepath.Join(base, cfgFileName)
	logger.Debugf("target path for %q → %s", username, cfgPath)

	// 2) Check existence
	exists, err := CheckConfig(cfgPath)
	if err != nil {
		logger.Errorf("check config failed at %s: %v", cfgPath, err)
		return err
	}

	// 3) Repair or create
	if exists {
		logger.Debugf("existing config detected for %q at %s — validating/repairing", username, cfgPath)
		if err := repairConfig(cfgPath, base); err != nil {
			logger.Errorf("repair failed for %q at %s: %v", username, cfgPath, err)
			return err
		}
		if err := ensureFilePerms(cfgPath, filePerm); err != nil {
			logger.Errorf("chmod existing config %s: %v", cfgPath, err)
			return err
		}
		logger.Debugf("existing config ready for %q at %s", username, cfgPath)
		return nil
	}

	// Create new with defaults (Docker.Folder = <base>/docker)
	logger.Infof("new user detected — generating default config for %q", username)
	if err := writeConfig(cfgPath, base); err != nil {
		logger.Errorf("write default config at %s failed: %v", cfgPath, err)
		return err
	}
	if err := ensureFilePerms(cfgPath, filePerm); err != nil {
		logger.Errorf("chmod new config %s: %v", cfgPath, err)
		return err
	}
	logger.Infof("created default config for %q at %s", username, cfgPath)
	return nil
}

// EnsureConfigReady ensures config exists, repairs it, fixes ownership when root, and logs the final state.
// It **never returns an error** — safe to call directly from main().
func EnsureConfigReady(username string) {
	// Create/repair
	if err := Initialize(username); err != nil {
		logger.Warnf("Initialize failed for %q: %v", username, err)
		return
	}

	// Strict load (determines the actual path, respecting fallbacks)
	cfg, cfgPath, err := Load(username)
	if err != nil {
		logger.Warnf("Load failed for %q: %v", username, err)
		return
	}

	// If root, fix ownership of file and parent dir (no-op otherwise)
	if err := chownIfRoot(filepath.Dir(cfgPath), username); err != nil {
		logger.Warnf("chown dir %s (user=%s) failed: %v", filepath.Dir(cfgPath), username, err)
	} else {
		logger.Debugf("ensured ownership for dir %s (user=%s)", filepath.Dir(cfgPath), username)
	}
	if err := chownIfRoot(cfgPath, username); err != nil {
		logger.Warnf("chown file %s (user=%s) failed: %v", cfgPath, username, err)
	} else {
		logger.Debugf("ensured ownership for file %s (user=%s)", cfgPath, username)
	}

	logger.Infof("Config ready for %s", username)
	logger.Debugf(
		"config details: path=%q theme=%s primary=%s",
		cfgPath, cfg.AppSettings.Theme, cfg.AppSettings.PrimaryColor,
	)
}
