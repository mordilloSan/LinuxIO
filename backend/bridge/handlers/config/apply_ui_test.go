package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func TestApplyUISettingsUpdateUsesReplacementShape(t *testing.T) {
	theme := "DARK"
	navigation := "dock"
	hidden := true
	fontSize := 18
	primary := "#123456"

	replacement := userconfig.DefaultUIPreferences()
	err := applyUISettingsUpdate(&replacement, &apischema.ConfigUISetPayload{
		Theme:            &theme,
		PrimaryColor:     &primary,
		NavigationMode:   &navigation,
		SidebarCollapsed: &hidden,
		TerminalFontSize: &fontSize,
	})
	require.NoError(t, err)
	defaults := userconfig.DefaultUIPreferences()
	require.Equal(t, userconfig.ThemeDark, replacement.Theme)
	require.Equal(t, userconfig.CSSColor(primary), replacement.PrimaryColor)
	require.Equal(t, navigation, replacement.NavigationMode)
	require.True(t, replacement.SidebarCollapsed)
	require.Equal(t, fontSize, replacement.TerminalFontSize)
	require.Equal(t, defaults.ThemeColors, replacement.ThemeColors)
	require.Equal(t, defaults.HiddenCards, replacement.HiddenCards)
	require.Equal(t, defaults.ViewModes, replacement.ViewModes)
}

func TestApplyUISettingsUpdateClearsOmittedOverrides(t *testing.T) {
	theme := "LIGHT"
	replacement := userconfig.DefaultUIPreferences()
	require.NoError(t, applyUISettingsUpdate(&replacement, &apischema.ConfigUISetPayload{Theme: &theme}))
	require.Equal(t, userconfig.ThemeLight, replacement.Theme)
	require.Equal(t, userconfig.CSSColor("#2196f3"), replacement.PrimaryColor)
	require.Equal(t, userconfig.DefaultUIPreferences().ThemeColors, replacement.ThemeColors)
	require.False(t, replacement.SidebarCollapsed)
	require.Empty(t, replacement.HiddenCards)
	require.Empty(t, replacement.CollapsedSections)
	require.Empty(t, replacement.ViewModes)
}

// Nothing collapsed is the default, so a section the backend has never heard
// of starts expanded without a default written for it.
func TestCollapsedSectionsDefaultToNone(t *testing.T) {
	defaults := userconfig.DefaultUIPreferences()
	require.NotNil(t, defaults.CollapsedSections)
	require.Empty(t, defaults.CollapsedSections)
	require.Equal(t, map[string][]string{}, uiConfigToAPI(defaults).CollapsedSections)
}

func TestApplyCollapsedSectionsStoresAnyNamesNormalized(t *testing.T) {
	ui := userconfig.DefaultUIPreferences()
	require.NoError(t, applyUISettingsUpdate(&ui, &apischema.ConfigUISetPayload{
		CollapsedSections: map[string][]string{
			" hardware ":       {"sensors", " pciDevices ", "", "sensors"},
			"a.future.page":    {"anything"},
			"docker.dashboard": {"", "  "},
			"  ":               {"orphan"},
		},
	}))
	want := map[string][]string{
		"hardware":      {"pciDevices", "sensors"},
		"a.future.page": {"anything"},
	}
	require.Equal(t, want, ui.CollapsedSections)
	require.Equal(t, want, uiConfigToAPI(ui).CollapsedSections)
}

func TestApplyCollapsedSectionsRefusesOversizedMaps(t *testing.T) {
	tooManyPages := map[string][]string{}
	for i := range 1000 {
		tooManyPages[fmt.Sprintf("page-%d", i)] = []string{"section"}
	}
	tooManySections := make([]string, 1000)
	for i := range tooManySections {
		tooManySections[i] = fmt.Sprintf("section-%d", i)
	}
	longName := strings.Repeat("x", 1000)

	for name, collapsed := range map[string]map[string][]string{
		"pages":        tooManyPages,
		"sections":     {"hardware": tooManySections},
		"page name":    {longName: {"sensors"}},
		"section name": {"hardware": {longName}},
	} {
		ui := userconfig.DefaultUIPreferences()
		err := applyUISettingsUpdate(&ui, &apischema.ConfigUISetPayload{CollapsedSections: collapsed})
		require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs, name)
		require.Empty(t, ui.CollapsedSections, name)
	}
}
