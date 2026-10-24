package config

import (
	"errors"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func applyAppSettingsUpdate(app *userconfig.PersistedAppSettings, payload *apischema.ConfigAppSettingsPayload) error {
	applyOptionalBool(&app.ShowHiddenFiles, payload.ShowHiddenFiles)
	if payload.ChunkSizeMB == nil {
		return nil
	}
	if !userconfig.ValidChunkSizeMB(*payload.ChunkSizeMB) {
		return errors.New("chunkSizeMB must be 0 (default) or between 1 and 32")
	}
	app.ChunkSizeMB = *payload.ChunkSizeMB
	return nil
}
