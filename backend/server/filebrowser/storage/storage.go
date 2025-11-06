package storage

import (
	"github.com/gtsteffaniak/go-logger/logger"
)

// Initialize creates config-based storage (default)
func Initialize() (Store, error) {
	logger.Infof("Using config-based storage (user configs in ~/.config/filebrowser/)")
	store, err := InitializeConfigStore()
	return store, err
}
