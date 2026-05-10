package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserStoreSnapshotReturnsIsolatedCopy(t *testing.T) {
	cfgPath, cfg := writeTestConfig(t)
	store := newUserStore("miguel", cfgPath, cfg)

	snapshot, err := store.Snapshot()
	require.NoError(t, err)
	snapshot.AppSettings.Theme = ThemeLight
	snapshot.AppSettings.DashboardOrder[0] = "mutated"
	snapshot.AppSettings.ViewModes["accounts.users"] = "table"
	*snapshot.AppSettings.ThemeColors.Dark.BackgroundDefault = "#ffffff"
	snapshot.Docker.Folders[0] = "/tmp/mutated"

	next, err := store.Snapshot()
	require.NoError(t, err)
	require.Equal(t, ThemeDark, next.AppSettings.Theme)
	require.Equal(t, "overview", next.AppSettings.DashboardOrder[0])
	require.Equal(t, "card", next.AppSettings.ViewModes["accounts.users"])
	require.Equal(t, cssColor("#1B2635"), next.AppSettings.ThemeColors.Dark.BackgroundDefault)
	require.Equal(t, cfg.Docker.Folders, next.Docker.Folders)
}

func TestUserStoreUpdatePersistsAndRefreshesMemory(t *testing.T) {
	cfgPath, cfg := writeTestConfig(t)
	store := newUserStore("miguel", cfgPath, cfg)

	updated, err := store.Update(func(settings *Settings) error {
		settings.AppSettings.Theme = ThemeLight
		settings.Docker.AutoUpdateStacks = append(settings.Docker.AutoUpdateStacks, "app")
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, ThemeLight, updated.AppSettings.Theme)
	require.Equal(t, []string{"app"}, updated.Docker.AutoUpdateStacks)

	snapshot, err := store.Snapshot()
	require.NoError(t, err)
	require.Equal(t, ThemeLight, snapshot.AppSettings.Theme)
	require.Equal(t, []string{"app"}, snapshot.Docker.AutoUpdateStacks)

	onDisk, err := readConfigStrict(cfgPath)
	require.NoError(t, err)
	require.Equal(t, ThemeLight, onDisk.AppSettings.Theme)
	require.Equal(t, []string{"app"}, onDisk.Docker.AutoUpdateStacks)
	require.FileExists(t, cfgPath+".lock")
}

func TestUserStoreUpdateStartsFromLatestDiskConfig(t *testing.T) {
	cfgPath, cfg := writeTestConfig(t)
	store := newUserStore("miguel", cfgPath, cfg)

	external := cloneSettings(cfg)
	external.AppSettings.PrimaryColor = "#00ff00"
	require.NoError(t, writeConfigFrom(cfgPath, *external))

	updated, err := store.Update(func(settings *Settings) error {
		settings.AppSettings.ShowHiddenFiles = false
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, CSSColor("#00ff00"), updated.AppSettings.PrimaryColor)
	require.False(t, updated.AppSettings.ShowHiddenFiles)

	snapshot, err := store.Snapshot()
	require.NoError(t, err)
	require.Equal(t, CSSColor("#00ff00"), snapshot.AppSettings.PrimaryColor)
	require.False(t, snapshot.AppSettings.ShowHiddenFiles)
}

func TestUserStoreUpdateRejectsInvalidConfig(t *testing.T) {
	cfgPath, cfg := writeTestConfig(t)
	store := newUserStore("miguel", cfgPath, cfg)

	_, err := store.Update(func(settings *Settings) error {
		settings.AppSettings.PrimaryColor = "nope"
		return nil
	})
	require.Error(t, err)

	snapshot, snapErr := store.Snapshot()
	require.NoError(t, snapErr)
	require.Equal(t, CSSColor("#2196f3"), snapshot.AppSettings.PrimaryColor)

	onDisk, readErr := readConfigStrict(cfgPath)
	require.NoError(t, readErr)
	require.Equal(t, CSSColor("#2196f3"), onDisk.AppSettings.PrimaryColor)
}

func writeTestConfig(t *testing.T) (string, *Settings) {
	t.Helper()

	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	cfg := DefaultSettings(base)
	require.NoError(t, os.MkdirAll(filepath.Dir(cfgPath), dirPerm))
	require.NoError(t, writeConfigFrom(cfgPath, *cfg))
	return cfgPath, cfg
}
