package config

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func applyDockerSettingsUpdate(docker *userconfig.Docker, payload *apischema.ConfigDockerPayload) error {
	if payload.Folders != nil {
		folders, err := userconfig.ParseDockerFolders(payload.Folders)
		if err != nil {
			return err
		}
		docker.Folders = folders
	}
	applyOptionalBool(&docker.RequireMountsForFolders, payload.RequireMountsForFolders)
	return nil
}
