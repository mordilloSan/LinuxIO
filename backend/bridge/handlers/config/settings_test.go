package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExampleDefaultsUseRuntimeDefaultsWithNeutralBase(t *testing.T) {
	require.Equal(t, *DefaultSettings("/var/lib/linuxio"), ExampleDefaults())
	require.Equal(t, []AbsolutePath{"/var/lib/linuxio/docker"}, ExampleDefaults().Docker.Folders)
}

func TestDefaultSettingsIncludeCompleteAppDefaults(t *testing.T) {
	cfg := DefaultSettings("/home/miguel")
	app := cfg.AppSettings

	require.Equal(t, ThemeDark, app.Theme)
	require.Equal(t, CSSColor("#2196f3"), app.PrimaryColor)
	require.True(t, app.ShowHiddenFiles)
	require.Equal(t, []string{"overview", "system", "cpu", "memory", "docker", "nic", "fs", "mb", "gpu", "drive"}, app.DashboardOrder)
	require.Equal(t, 1, app.ChunkSizeMB)
	require.Equal(t, []AbsolutePath{"/home/miguel/docker"}, cfg.Docker.Folders)

	require.NotNil(t, app.ThemeColors)
	require.NotNil(t, app.ThemeColors.Light)
	require.NotNil(t, app.ThemeColors.Dark)
	require.Equal(t, cssColor("#F7F9FC"), app.ThemeColors.Light.BackgroundDefault)
	require.Equal(t, cssColor("#1B2635"), app.ThemeColors.Dark.BackgroundDefault)

	require.NotNil(t, app.DockerDashboardSections)
	require.True(t, app.DockerDashboardSections.Overview)
	require.True(t, app.DockerDashboardSections.Daemon)
	require.True(t, app.DockerDashboardSections.Resources)

	require.NotNil(t, app.HardwareSections)
	require.True(t, app.HardwareSections.Overview)
	require.True(t, app.HardwareSections.Hardware)
	require.True(t, app.HardwareSections.Sensors)
	require.True(t, app.HardwareSections.SystemInfo)
	require.True(t, app.HardwareSections.GPU)
	require.True(t, app.HardwareSections.PCIDevices)
	require.True(t, app.HardwareSections.MemoryModules)

	require.Len(t, app.ViewModes, 12)
	require.Equal(t, "card", app.ViewModes["accounts.users"])
	require.Equal(t, "card", app.ViewModes["docker.containers"])
	require.Equal(t, "card", app.ViewModes["services.list"])
	require.Equal(t, "card", app.ViewModes["shares.mounts"])
}
