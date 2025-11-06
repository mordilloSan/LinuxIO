package storage

import (
	"github.com/gtsteffaniak/go-logger/logger"

	"github.com/mordilloSan/filebrowser/backend/auth"
	"github.com/mordilloSan/filebrowser/backend/auth/users"
	"github.com/mordilloSan/filebrowser/backend/common/settings"
	"github.com/mordilloSan/filebrowser/backend/common/utils"
)

// ConfigBasedStore is a storage implementation using config files
type ConfigBasedStore struct {
	Users    *users.Storage
	Auth     *auth.Storage
	Settings *settings.Storage
}

// InitializeConfigStore creates a new config-based storage
func InitializeConfigStore() (*ConfigBasedStore, error) {
	// Create config-based user store
	configBackend := users.NewConfigStore()
	userStore := users.NewStorage(configBackend)

	// Create auth storage (uses in-memory backend since auth config is in global config)
	authBackend := &noOpAuthBackend{}
	authStore, err := auth.NewStorage(authBackend, userStore)
	if err != nil {
		return nil, err
	}

	// Create settings storage (uses global config)
	settingsBackend := &configSettingsBackend{}
	settingsStore := settings.NewStorage(settingsBackend)

	// Generate auth key if not set
	if settings.Config.Auth.Key == "" {
		settings.Config.Auth.Key = utils.GenerateKey()
		logger.Infof("Generated new auth key")
	}

	store := &ConfigBasedStore{
		Users:    userStore,
		Auth:     authStore,
		Settings: settingsStore,
	}

	logger.Infof("Initialized config-based storage")
	return store, nil
}

// noOpAuthBackend is a no-op auth backend for config-based storage
// Auth configuration is managed via global config file, not stored separately
type noOpAuthBackend struct{}

func (n *noOpAuthBackend) Get(method string) (auth.Auther, error) {
	// Auth methods are configured in global config
	switch method {
	case "pam":
		return &auth.PAMAuth{}, nil
	default:
		return &auth.PAMAuth{}, nil
	}
}

func (n *noOpAuthBackend) Save(a auth.Auther) error {
	// No-op: auth configuration is managed via global config
	return nil
}

// configSettingsBackend implements settings storage using the global Config variable
type configSettingsBackend struct{}

func (c *configSettingsBackend) Get() (*settings.Settings, error) {
	return &settings.Config, nil
}

func (c *configSettingsBackend) Save(s *settings.Settings) error {
	settings.Config = *s
	return nil
}

func (c *configSettingsBackend) GetServer() (*settings.Server, error) {
	return &settings.Config.Server, nil
}

func (c *configSettingsBackend) SaveServer(s *settings.Server) error {
	settings.Config.Server = *s
	return nil
}
