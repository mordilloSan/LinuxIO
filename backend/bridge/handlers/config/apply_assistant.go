package config

import (
	"fmt"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/assistant"
	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func applyAssistantSettingsUpdate(dst *bridgeconfig.Assistant, payload *apischema.ConfigAssistantPayload) error {
	if payload.Agent == nil {
		return nil
	}
	agent := strings.TrimSpace(*payload.Agent)
	if agent != "" {
		if _, ok := assistant.LookupAgent(agent); !ok {
			return fmt.Errorf("unknown assistant agent %q (known: %s)", agent, strings.Join(assistant.AgentIDs(), ", "))
		}
	}
	dst.Agent = agent
	return nil
}
