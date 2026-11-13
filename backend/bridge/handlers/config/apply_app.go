package config

import (
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func applyAppSettingsUpdate(app *userconfig.PersistedAppSettings, payload *apischema.ConfigAppSettingsPayload) error {
	applyOptionalBool(&app.ShowHiddenFiles, payload.ShowHiddenFiles)
	if payload.ChunkSizeMB == nil {
		return nil
	}
	if !userconfig.ValidChunkSizeMB(*payload.ChunkSizeMB) {
		return fmt.Errorf("%w: chunkSizeMB must be 0 (default) or between 1 and 32", bridgeipc.ErrInvalidArgs)
	}
	app.ChunkSizeMB = *payload.ChunkSizeMB
	return nil
}
