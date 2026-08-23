package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func TestApplyUISettingsUpdateUsesReplacementShape(t *testing.T) {
	theme := "DARK"
	navigation := "dock"
	hidden := true
	fontSize := 18
	primary := "#123456"

	replacement := bridgeconfig.DefaultUIPreferences()
	err := applyUISettingsUpdate(&replacement, &apischema.ConfigUISetPayload{
		Theme:            &theme,
		PrimaryColor:     &primary,
		NavigationMode:   &navigation,
		SidebarCollapsed: &hidden,
		TerminalFontSize: &fontSize,
	})
	require.NoError(t, err)
	defaults := bridgeconfig.DefaultUIPreferences()
	require.Equal(t, bridgeconfig.ThemeDark, replacement.Theme)
	require.Equal(t, bridgeconfig.CSSColor(primary), replacement.PrimaryColor)
	require.Equal(t, navigation, replacement.NavigationMode)
	require.True(t, replacement.SidebarCollapsed)
	require.Equal(t, fontSize, replacement.TerminalFontSize)
	require.Equal(t, defaults.ThemeColors, replacement.ThemeColors)
	require.Equal(t, defaults.HiddenCards, replacement.HiddenCards)
	require.Equal(t, defaults.ViewModes, replacement.ViewModes)
}

func TestApplyUISettingsUpdateClearsOmittedOverrides(t *testing.T) {
	theme := "LIGHT"
	replacement := bridgeconfig.DefaultUIPreferences()
	require.NoError(t, applyUISettingsUpdate(&replacement, &apischema.ConfigUISetPayload{Theme: &theme}))
	require.Equal(t, bridgeconfig.ThemeLight, replacement.Theme)
	require.Equal(t, bridgeconfig.CSSColor("#2196f3"), replacement.PrimaryColor)
	require.Equal(t, bridgeconfig.DefaultUIPreferences().ThemeColors, replacement.ThemeColors)
	require.False(t, replacement.SidebarCollapsed)
	require.Empty(t, replacement.HiddenCards)
	require.Empty(t, replacement.ViewModes)
}
