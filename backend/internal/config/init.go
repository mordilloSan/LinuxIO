//go:generate go run ./generator.go

package config

import (
	"path/filepath"

	"github.com/mordilloSan/LinuxIO/internal/logger"
)

const (
	cfgFileName = ".linuxio-config.yaml"
	filePerm    = 0o644 // file:  rw-r-r--
	dirPerm     = 0o755 // dir:   rwxrr-x
)

// Initialize prepares the per-user LinuxIO configuration file for `username`.
//
// Flow:
//  1. Resolve a base folder: try Homedir(username); if that fails, choose a
//     writable POSIX/XDG fallback (no root required).
//  2. Build <base>/.linuxio-config.yaml.
//  3. If the file exists: repair in place (keep valid fields, fix only bad ones).
//     If the file does not exist: create defaults.
//  4. Ensure file permissions to 0o664.
func Initialize(username string) error {
	base, baseErr := Homedir(username)
	if baseErr != nil {
		logger.Warnf("homedir not available for %q: %v — using fallback", username, baseErr)
		b, err := fallbackBase(username)
		if err != nil {
			logger.Errorf("fallback base resolution failed: %v", err)
			return err
		}
		base = b
	}
	cfgPath := filepath.Join(base, cfgFileName)

	exists, err := CheckConfig(cfgPath)
	if err != nil {
		logger.Errorf("check config: %v", err)
		return err
	}

	if exists {
		if err := repairConfig(cfgPath, base); err != nil {
			return err
		}
		if err := ensureFilePerms(cfgPath, filePerm); err != nil {
			logger.Errorf("chmod existing config: %v", err)
			return err
		}
		logger.Debugf("Loaded config from %s", cfgPath)
		return nil
	}

	// Create new with defaults (Docker.Folder = <base>/docker)
	logger.Infof("New user detected - Generating default config for: %v", username)
	if err := writeConfig(cfgPath, base); err != nil {
		logger.Errorf("write default config: %v", err)
		return err
	}
	logger.Infof("Created default config at %s", cfgPath)
	return nil
}

// InitializeAndLoad ensures config exists, fixes it, corrects ownership (if root), then loads it.
func InitializeAndLoad(username string) (*Settings, string, error) {
	// 1) Create/repair
	if err := Initialize(username); err != nil {
		return nil, "", err
	}
	// 2) Strict load (this also determines the real path, respecting fallbacks)
	cfg, cfgPath, err := Load(username)
	if err != nil {
		return nil, "", err
	}
	// 3) If root, fix ownership of file and parent dir
	_ = chownIfRoot(filepath.Dir(cfgPath), username)
	_ = chownIfRoot(cfgPath, username)
	return cfg, cfgPath, nil
}
