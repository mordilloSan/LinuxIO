package utils

import (
	embed "go-backend"
	"go-backend/internal/logger"
	"os"
	"path/filepath"
)

// Ensures file at `path` exists; if not, writes `defaultContent` to it.
func EnsureDefaultFile(path string, defaultContent []byte) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			logger.Errorf("Failed to create directory for %s: %v", path, err)
			return err
		}
		if err := os.WriteFile(path, defaultContent, 0644); err != nil {
			logger.Errorf("Failed to write default file %s: %v", path, err)
			return err
		}
		logger.Infof("✅ Generated default file: %s", path)
	}
	return nil
}

func EnsureStartupDefaults() error {
	home, err := GetUserHome()
	if err != nil {
		return err
	}

	themePath := filepath.Join(home, ".linuxio-theme.yaml")
	if err := EnsureDefaultFile(themePath, embed.DefaultThemeConfig); err != nil {
		return err
	}

	dockerPath := filepath.Join(home, ".linuxio-docker.yaml")
	if err := EnsureDefaultFile(dockerPath, embed.DefaultDockerConfig); err != nil {
		return err
	}

	// ...add more files as needed, always under home...
	return nil
}
