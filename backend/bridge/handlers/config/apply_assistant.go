package config

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/acpagents"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

// maxAssistantOptionValue bounds a stored option value; the agent's own ids
// ("opus", "high", "acceptEdits") are far shorter.
const maxAssistantOptionValue = 128

func applyAssistantSettingsUpdate(dst *userconfig.Assistant, payload *apischema.ConfigAssistantPayload) error {
	if payload.Agent != nil {
		agent := strings.TrimSpace(*payload.Agent)
		if agent != "" {
			if err := requireKnownAgent(agent); err != nil {
				return err
			}
		}
		dst.Agent = agent
	}
	if payload.Defaults != nil {
		defaults := make(map[string]userconfig.AssistantDefaults, len(payload.Defaults))
		for agent, value := range payload.Defaults {
			if err := requireKnownAgent(agent); err != nil {
				return err
			}
			for _, field := range []string{value.Model, value.Effort, value.Mode} {
				if !validAssistantOptionValue(field) {
					return fmt.Errorf("%w: invalid assistant default %q for agent %q", bridgeipc.ErrInvalidArgs, field, agent)
				}
			}
			defaults[agent] = userconfig.AssistantDefaults(value)
		}
		dst.Defaults = defaults
	}
	return nil
}

func requireKnownAgent(agent string) error {
	if _, ok := acpagents.Lookup(agent); !ok {
		return fmt.Errorf("%w: unknown assistant agent %q (known: %s)", bridgeipc.ErrInvalidArgs, agent, strings.Join(acpagents.IDs(), ", "))
	}
	return nil
}

func validAssistantOptionValue(value string) bool {
	return len(value) <= maxAssistantOptionValue && !strings.ContainsFunc(value, unicode.IsControl)
}
