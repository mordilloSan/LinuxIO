package config

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func TestApplyThemeColorOverridesBuildsPerModeColors(t *testing.T) {
	ui := userconfig.DefaultUIPreferences()
	err := applyThemeColorOverrides(&ui, &apischema.ConfigThemeColorsByModePayload{
		Dark: &apischema.ConfigThemeColorsPayload{ChartRx: new("#123456")},
	})
	require.NoError(t, err)
	require.Nil(t, ui.ThemeColors.Light)
	require.Equal(t, userconfig.CSSColor("#123456"), *ui.ThemeColors.Dark.ChartRx)

	err = applyThemeColorOverrides(&ui, &apischema.ConfigThemeColorsByModePayload{
		Light: &apischema.ConfigThemeColorsPayload{CodeText: new("nope")},
	})
	require.EqualError(t, err, "invalid themeColors.light.codeText")
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

	err = applyJobSettingsUpdate(&jobs, &apischema.ConfigJobSettingsPayload{HeavyArchiveConcurrency: new(0)})
	require.EqualError(t, err, "jobs.heavyArchiveConcurrency must be > 0")

	require.NoError(t, applyJobSettingsUpdate(&jobs, &apischema.ConfigJobSettingsPayload{ProgressMinIntervalMs: new(10)}))
	require.Equal(t, 10, jobs.ProgressMinIntervalMs)
}
