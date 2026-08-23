package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func TestApplyDockAccentGradientSettingReplacesWholeValue(t *testing.T) {
	app := bridgeconfig.UIPreferences{}
	startColor := " #123456 "
	err := applyDockAccentGradientSetting(&app, &apischema.ConfigDockAccentGradient{
		StartColor: &startColor, RangeStart: 20, RangeEnd: 80,
	})
	require.NoError(t, err)
	require.Equal(t, &bridgeconfig.DockAccentGradient{StartColor: "#123456", RangeStart: 20, RangeEnd: 80}, app.DockAccentGradient)
}

func TestApplyDockAccentGradientSettingRejectsInvalidRangeAndColor(t *testing.T) {
	app := bridgeconfig.UIPreferences{DockAccentGradient: &bridgeconfig.DockAccentGradient{RangeStart: 0, RangeEnd: 100}}
	startColor := "not-a-color"
	err := applyDockAccentGradientSetting(&app, &apischema.ConfigDockAccentGradient{StartColor: &startColor, RangeStart: 90, RangeEnd: 10})
	require.Error(t, err)
	require.Equal(t, &bridgeconfig.DockAccentGradient{RangeStart: 0, RangeEnd: 100}, app.DockAccentGradient)
}
