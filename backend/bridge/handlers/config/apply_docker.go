package config

import (
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

// applyDockerSettingsUpdate applies the payload to a copy and stores it only
// when every field is valid, so a refused update changes nothing.
func applyDockerSettingsUpdate(docker *userconfig.Docker, payload *apischema.ConfigDockerPayload) error {
	next := *docker
	if payload.Folders != nil {
		folders, err := userconfig.ParseDockerFolders(payload.Folders)
		if err != nil {
			return fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
		}
		next.Folders = folders
	}
	applyOptionalBool(&next.RequireMountsForFolders, payload.RequireMountsForFolders)
	if payload.HostIPs != nil {
		ips, err := userconfig.ParseDockerHostIPs(payload.HostIPs)
		if err != nil {
			return fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
		}
		next.HostIPs = ips
	}
	applyOptionalBool(&next.WaitForHostIPs, payload.WaitForHostIPs)
	if err := userconfig.ValidateDockerHostIPWait(next); err != nil {
		return fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	*docker = next
	return nil
}
