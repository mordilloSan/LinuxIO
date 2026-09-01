package config

import (
	"fmt"
	"log/slog"
	"path/filepath"
)

const (
	cfgFileName   = "config.yaml"
	uiCfgFileName = "ui.yaml"
	filePerm      = 0o644 // file:  rw-r--r--
	dirPerm       = 0o755 // dir:   rwxr-xr-x
)

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
		slog.Info("wrote default core config", "component", "config", "path", cfgPath)
	} else if err := owner.ensureFile(cfgPath); err != nil {
		return fmt.Errorf("own core config: %w", err)
	}
	if !uiExists {
		if err := writeEmptyUIConfigOwned(uiPath, owner); err != nil {
			return fmt.Errorf("write default UI config: %w", err)
		}
		slog.Info("wrote empty UI config", "component", "config", "path", uiPath)
	} else if err := owner.ensureFile(uiPath); err != nil {
		return fmt.Errorf("own UI config: %w", err)
	}
	return nil
}
