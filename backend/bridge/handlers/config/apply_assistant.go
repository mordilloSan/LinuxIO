package config

import (
	"fmt"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func applyAssistantSettingsUpdate(dst *userconfig.Assistant, payload *apischema.ConfigAssistantPayload) error {
	if payload.Agent == nil {
		return nil
	}
	agent := strings.TrimSpace(*payload.Agent)
	if agent != "" {
		if _, ok := acpagents.Lookup(agent); !ok {
			return fmt.Errorf("unknown assistant agent %q (known: %s)", agent, strings.Join(acpagents.IDs(), ", "))
		}
	}
	dst.Agent = agent
	return nil
}
