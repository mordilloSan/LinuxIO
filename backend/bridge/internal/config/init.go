package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

const (
	cfgFileName   = ".linuxio-config.yaml"
	uiCfgFileName = ".linuxio-ui.yaml"
	filePerm      = 0o644 // file:  rw-r--r--
	dirPerm       = 0o755 // dir:   rwxr-xr-x
)

func configBase(username string) (string, error) {
	base, err := Homedir(username)
	if err != nil {
		slog.Error("home directory resolution failed", "user", username, "error", err)
		return "", err
	}
	return base, nil
}

func initializeLockedOwned(cfgPath, uiPath, base string, owner fileOwnership) error {
	if err := owner.ensureDirectory(filepath.Dir(cfgPath)); err != nil {
		return err
	}

	coreExists, err := CheckConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("check core config: %w", err)
	}
	uiExists, err := CheckConfig(uiPath)
	if err != nil {
		return fmt.Errorf("check UI config: %w", err)
	}

	if !coreExists {
		if err := writeCoreConfigOwned(cfgPath, *DefaultSettings(base), owner); err != nil {
			return fmt.Errorf("write default core config: %w", err)
		}
	} else if err := owner.ensureFile(cfgPath); err != nil {
		return fmt.Errorf("own core config: %w", err)
	}
	// A pre-split install has UI keys inside the core document. Recognize and
	// convert that one shape before the normal strict core read. Invalid current
	// or legacy documents are left untouched and reported to the caller.
	if coreExists {
		migrated, err := migrateLegacyIfNeeded(cfgPath, uiPath, base, owner, uiExists)
		if err != nil {
			return err
		}
		if migrated {
			uiExists = true
		}
	}
	if !uiExists {
		if err := writeEmptyUIConfigOwned(uiPath, owner); err != nil {
			return fmt.Errorf("write default UI config: %w", err)
		}
	} else if err := owner.ensureFile(uiPath); err != nil {
		return fmt.Errorf("own UI config: %w", err)
	}
	return nil
}

func migrateLegacyIfNeeded(cfgPath, uiPath, base string, owner fileOwnership, uiExists bool) (bool, error) {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return false, fmt.Errorf("read core config during initialization: %w", err)
	}
	if _, parseErr := decodeCoreConfig(raw, base); parseErr == nil {
		return false, nil
	} else {
		migrated, migrationErr := migrateLegacyConfigLocked(cfgPath, uiPath, base, owner, uiExists)
		if migrationErr != nil {
			return false, migrationErr
		}
		if !migrated {
			return false, fmt.Errorf("invalid core config: %w", parseErr)
		}
		slog.Info("converted legacy user configuration", "component", "config", "path", cfgPath, "uiPath", uiPath)
		return true, nil
	}
}
