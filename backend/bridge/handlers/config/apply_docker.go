package config

import (
	"fmt"
	"path/filepath"
	"strings"

	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func applyDockerSettingsUpdate(docker *bridgeconfig.Docker, payload *configDockerPayload) error {
	if err := applyDockerFoldersSetting(docker, payload.Folders); err != nil {
		return err
	}
	if payload.Proxy != nil {
		applyDockerProxyUpdate(&docker.Proxy, payload.Proxy)
	}
	return nil
}

func applyDockerProxyUpdate(proxy *bridgeconfig.DockerProxy, payload *configDockerProxyPayload) {
	if payload.CaddyEnabled != nil {
		proxy.CaddyEnabled = *payload.CaddyEnabled
	}
	if payload.BaseDomain != nil {
		proxy.BaseDomain = strings.TrimSpace(*payload.BaseDomain)
	}
	if payload.TLSEmail != nil {
		proxy.TLSEmail = strings.TrimSpace(*payload.TLSEmail)
	}
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
