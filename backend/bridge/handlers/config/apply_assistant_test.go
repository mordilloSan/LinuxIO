package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func TestApplyAssistantSettingsUpdate(t *testing.T) {
	var cfg bridgeconfig.Settings

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
	cfg := bridgeconfig.Settings{Assistant: bridgeconfig.Assistant{Agent: "gemini"}}
	api := appConfigToAPI(cfg, bridgeconfig.StorageMode("home"))
	require.Equal(t, "gemini", api.Assistant.Agent)
}
