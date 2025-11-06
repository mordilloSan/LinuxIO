package storage

import (
	"github.com/mordilloSan/filebrowser/backend/auth"
	"github.com/mordilloSan/filebrowser/backend/auth/users"
	"github.com/mordilloSan/filebrowser/backend/common/settings"
)

// Store is the common interface for all storage backends
// Both BoltStore and ConfigBasedStore implement this interface
type Store interface {
	GetUsers() *users.Storage
	GetAuth() *auth.Storage
	GetSettings() *settings.Storage
}

// Ensure both implementations satisfy the interface
var _ Store = (*ConfigBasedStore)(nil)

// GetUsers returns the users storage
func (s *ConfigBasedStore) GetUsers() *users.Storage {
	return s.Users
}

// GetAuth returns the auth storage
func (s *ConfigBasedStore) GetAuth() *auth.Storage {
	return s.Auth
}

// GetSettings returns the settings storage
func (s *ConfigBasedStore) GetSettings() *settings.Storage {
	return s.Settings
}
