package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func applyDockerSettingsUpdate(docker *bridgeconfig.Docker, payload *apischema.ConfigDockerPayload) error {
	if err := applyDockerFoldersSetting(docker, payload.Folders); err != nil {
		return err
	}
	applyOptionalBool(&docker.RequireMountsForFolders, payload.RequireMountsForFolders)
	return nil
}

func applyDockerFoldersSetting(docker *bridgeconfig.Docker, folderValues []string) error {
	if folderValues == nil {
		return nil
	}
	if len(folderValues) == 0 {
		return fmt.Errorf("docker folders cannot be empty")
	}

	folders := make([]bridgeconfig.AbsolutePath, 0, len(folderValues))
	seen := make(map[string]struct{}, len(folderValues))
	for _, folderValue := range folderValues {
		folderInput := strings.TrimSpace(folderValue)
		if folderInput == "" {
			return fmt.Errorf("docker folders cannot include an empty path")
		}
		folder := filepath.Clean(folderInput)
		if !filepath.IsAbs(folder) {
			return fmt.Errorf("docker folder must be an absolute path")
		}
		if folder == string(filepath.Separator) {
			return fmt.Errorf("docker folder cannot be root")
		}
		if _, exists := seen[folder]; exists {
			return fmt.Errorf("docker folders cannot include duplicates")
		}
		seen[folder] = struct{}{}
		folders = append(folders, bridgeconfig.AbsolutePath(folder))
	}

	docker.Folders = folders
	return nil
}
