package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func TestApplyThemeColorOverridesBuildsPerModeColors(t *testing.T) {
	ui := userconfig.DefaultUIPreferences()
	err := applyThemeColorOverrides(&ui, &apischema.ConfigThemeColorsByModePayload{
		Dark: apischema.ConfigThemeColorsPayload{"chartRx": new("#123456"), "codeText": nil},
	})
	require.NoError(t, err)
	require.Nil(t, ui.ThemeColors.Light)
	require.Equal(t, userconfig.ThemeColors{"chartRx": "#123456"}, ui.ThemeColors.Dark)

	err = applyThemeColorOverrides(&ui, &apischema.ConfigThemeColorsByModePayload{
		Light: apischema.ConfigThemeColorsPayload{"codeText": new("nope")},
	})
	require.EqualError(t, err, "invalid themeColors.light.codeText")

	// The decoder takes any map key, so an unknown colour is refused here.
	err = applyThemeColorOverrides(&ui, &apischema.ConfigThemeColorsByModePayload{
		Dark: apischema.ConfigThemeColorsPayload{"nope": new("#000000"), "chartRx": new("#123456")},
	})
	require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs)
	require.ErrorContains(t, err, "unknown themeColors.dark.nope")

	err = applyThemeColorOverrides(&ui, &apischema.ConfigThemeColorsByModePayload{
		Light: apischema.ConfigThemeColorsPayload{}, Dark: apischema.ConfigThemeColorsPayload{"codeText": nil},
	})
	require.NoError(t, err)
	require.Nil(t, ui.ThemeColors)
}

func TestThemeColorsDecodeFromTheWire(t *testing.T) {
	decode := bridgeipc.JSONRequestDecoder[apischema.ConfigUISetPayload]()
	apply := func(body string) (*userconfig.ThemeColorsByMode, error) {
		request, err := decode([]byte(body))
		if err != nil {
			return nil, err
		}
		payload, ok := request.(apischema.ConfigUISetPayload)
		require.True(t, ok)
		ui := userconfig.DefaultUIPreferences()
		if err := applyThemeColorOverrides(&ui, payload.ThemeColors); err != nil {
			return nil, err
		}
		return ui.ThemeColors, nil
	}

	// A null colour is unset, as it was when each colour was a pointer field.
	colors, err := apply(`{"themeColors":{"dark":{"codeText":null,"chartRx":" #123456 "}}}`)
	require.NoError(t, err)
	require.Equal(t, &userconfig.ThemeColorsByMode{Dark: userconfig.ThemeColors{"chartRx": "#123456"}}, colors)

	colors, err = apply(`{"themeColors":{"light":null,"dark":{}}}`)
	require.NoError(t, err)
	require.Nil(t, colors)

	_, err = apply(`{"themeColors":{"dark":{"nope":"#123456"}}}`)
	require.ErrorIs(t, err, bridgeipc.ErrInvalidArgs)
	_, err = apply(`{"themeColors":{"sepia":{"chartRx":"#123456"}}}`)
	require.Error(t, err)
	_, err = apply(`{"themeColors":{"dark":{"chartRx":""}}}`)
	require.EqualError(t, err, "invalid themeColors.dark.chartRx")
}

func TestThemeColorsToAPIKeepsEveryColor(t *testing.T) {
	defaults := userconfig.DefaultUIPreferences().ThemeColors
	api := themeColorsToAPI(defaults)
	require.Len(t, api.Dark, len(defaults.Dark))
	require.Len(t, api.Light, len(defaults.Light))
	for key, color := range defaults.Dark {
		require.Equal(t, string(color), *api.Dark[apischema.ThemeColorKey(key)], key)
	}
	for key, color := range defaults.Light {
		require.Equal(t, string(color), *api.Light[apischema.ThemeColorKey(key)], key)
	}
	require.Nil(t, themeColorsToAPI(nil))
	require.Nil(t, themeColorsToAPI(&userconfig.ThemeColorsByMode{}).Light)
}

func TestApplyDockerSettingsUpdateValidatesFolders(t *testing.T) {
	docker := userconfig.Docker{Folders: []userconfig.AbsolutePath{"/srv/docker"}}
	require.NoError(t, applyDockerSettingsUpdate(&docker, &apischema.ConfigDockerPayload{Folders: []string{"/opt/a", "/opt/b/"}}))
	require.Equal(t, []userconfig.AbsolutePath{"/opt/a", "/opt/b"}, docker.Folders)

	err := applyDockerSettingsUpdate(&docker, &apischema.ConfigDockerPayload{Folders: []string{"/"}})
	require.EqualError(t, err, "docker folder cannot be root")
	require.Equal(t, []userconfig.AbsolutePath{"/opt/a", "/opt/b"}, docker.Folders)
}

func TestApplyJobSettingsUpdateRejectsInvalidValuesWithoutPartialApply(t *testing.T) {
	jobs := userconfig.DefaultJobSettings()
	err := applyJobSettingsUpdate(&jobs, &apischema.ConfigJobSettingsPayload{ProgressMinIntervalMs: new(10), ArchiveExtractWorkers: new(-1)})
	require.EqualError(t, err, "jobs.archiveExtractWorkers must be >= 0")
	require.Equal(t, userconfig.DefaultJobSettings(), jobs)

	require.NoError(t, applyJobSettingsUpdate(&jobs, &apischema.ConfigJobSettingsPayload{ProgressMinIntervalMs: new(10)}))
	require.Equal(t, 10, jobs.ProgressMinIntervalMs)
}
