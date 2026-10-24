package config

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func TestApplyAssistantSettingsUpdate(t *testing.T) {
	var cfg userconfig.Settings

	claude := " claude "
	require.NoError(t, applyConfigPayload(&cfg, &apischema.ConfigSetPayload{Assistant: &apischema.ConfigAssistantPayload{Agent: &claude}}))
	require.Equal(t, "claude", cfg.Assistant.Agent)

	require.NoError(t, applyConfigPayload(&cfg, &apischema.ConfigSetPayload{Assistant: &apischema.ConfigAssistantPayload{}}))
	require.Equal(t, "claude", cfg.Assistant.Agent, "nil agent leaves the value alone")

	empty := ""
	require.NoError(t, applyConfigPayload(&cfg, &apischema.ConfigSetPayload{Assistant: &apischema.ConfigAssistantPayload{Agent: &empty}}))
	require.Empty(t, cfg.Assistant.Agent, "empty string clears the choice")

	bogus := "opencode"
	require.Error(t, applyConfigPayload(&cfg, &apischema.ConfigSetPayload{Assistant: &apischema.ConfigAssistantPayload{Agent: &bogus}}))
}

func TestAppConfigToAPIIncludesAssistant(t *testing.T) {
	cfg := userconfig.Settings{Assistant: userconfig.Assistant{Agent: "gemini"}}
	api := appConfigToAPI(cfg, userconfig.StorageMode("home"))
	require.Equal(t, "gemini", api.Assistant.Agent)
}

func TestApplyAssistantDefaults(t *testing.T) {
	cfg := userconfig.Settings{Assistant: userconfig.Assistant{Agent: "claude"}}

	defaults := map[string]apischema.AssistantChatDefaults{
		"claude": {Model: "sonnet", Effort: "low", Mode: "plan"},
		"codex":  {},
	}
	require.NoError(t, applyConfigPayload(&cfg, &apischema.ConfigSetPayload{Assistant: &apischema.ConfigAssistantPayload{Defaults: defaults}}))
	require.Equal(t, "claude", cfg.Assistant.Agent, "defaults leave the agent alone")
	require.Equal(t, map[string]userconfig.AssistantDefaults{
		"claude": {Model: "sonnet", Effort: "low", Mode: "plan"},
		"codex":  {},
	}, cfg.Assistant.Defaults)

	for name, bad := range map[string]map[string]apischema.AssistantChatDefaults{
		"unknown agent":  {"opencode": {}},
		"control char":   {"claude": {Model: "opus\n"}},
		"oversize value": {"claude": {Mode: strings.Repeat("a", maxAssistantOptionValue+1)}},
	} {
		err := applyConfigPayload(&cfg, &apischema.ConfigSetPayload{Assistant: &apischema.ConfigAssistantPayload{Defaults: bad}})
		require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs, name)
	}

	// An agent the user never configured gets LinuxIO's starting point; a
	// configured one, even all empty, keeps exactly what was saved.
	effective := userconfig.EffectiveAssistantDefaults(map[string]userconfig.AssistantDefaults{"codex": {}})
	require.Equal(t, userconfig.AssistantDefaults{Model: "opus", Effort: "high", Mode: "auto"}, effective["claude"])
	require.Equal(t, userconfig.AssistantDefaults{}, effective["codex"])
	require.Equal(t, userconfig.AssistantDefaults{}, userconfig.EffectiveAssistantDefaults(map[string]userconfig.AssistantDefaults{"claude": {}})["claude"])
}
