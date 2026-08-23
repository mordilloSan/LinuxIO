package config

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func newUserStore(username, cfgPath, uiPath string, cfg *Settings, ui *UIPreferences) *UserStore {
	store := &UserStore{
		username:   username,
		path:       cfgPath,
		uiPath:     uiPath,
		lockPath:   cfgPath + ".lock",
		uiLockPath: uiPath + ".lock",
		owner:      currentProcessFileOwnership(),
	}
	if cfg != nil {
		store.cfg = *cloneSettings(cfg)
	}
	if ui != nil {
		store.ui = *cloneUIPreferences(ui)
	}
	return store
}

func TestUserStoreSnapshotReturnsIsolatedCopies(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	cfg := DefaultSettings(base)
	ui := UIPreferences{ViewModes: map[string]string{"accounts.users": "card"}}
	store := newUserStore("miguel", cfgPath, uiPath, cfg, &ui)

	core, err := store.Snapshot(context.Background())
	require.NoError(t, err)
	core.Docker.Folders[0] = "/tmp/mutated"
	uiSnapshot, err := store.UISnapshot(context.Background())
	require.NoError(t, err)
	uiSnapshot.ViewModes["accounts.users"] = "table"

	nextCore, err := store.Snapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, cfg.Docker.Folders, nextCore.Docker.Folders)
	nextUI, err := store.UISnapshot(context.Background())
	require.NoError(t, err)
	require.Equal(t, "card", nextUI.ViewModes["accounts.users"])
}

func TestUpdateForUserAndUIForUser(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	cfg := DefaultSettings(base)
	ui := DefaultUIPreferences()
	require.NoError(t, writeCoreConfig(cfgPath, *cfg))
	require.NoError(t, writeUIConfig(uiPath, ui))
	store := newUserStore("miguel", cfgPath, uiPath, cfg, &ui)

	updated, path, err := UpdateForUser(context.Background(), "miguel", store, func(value *Settings) error {
		value.AppSettings.ShowHiddenFiles = false
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, cfgPath, path)
	require.False(t, updated.AppSettings.ShowHiddenFiles)

	replacement := DefaultUIPreferences()
	replacement.Theme = ThemeDark
	updatedUI, uiFile, err := ReplaceUIForUser(context.Background(), "miguel", store, replacement)
	require.NoError(t, err)
	require.Equal(t, uiPath, uiFile)
	require.Equal(t, ThemeDark, updatedUI.Theme)
}

func TestUserStoreRejectsInvalidCoreAndUIUpdates(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	cfg := DefaultSettings(base)
	ui := DefaultUIPreferences()
	require.NoError(t, writeCoreConfig(cfgPath, *cfg))
	require.NoError(t, writeUIConfig(uiPath, ui))
	store := newUserStore("miguel", cfgPath, uiPath, cfg, &ui)
	coreBefore, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	uiBefore, err := os.ReadFile(uiPath)
	require.NoError(t, err)

	_, err = store.Update(context.Background(), func(value *Settings) error {
		value.AppSettings.ChunkSizeMB = 33
		return nil
	})
	require.Error(t, err)
	invalidUI := DefaultUIPreferences()
	invalidUI.Theme = PersistedTheme("PURPLE")
	_, err = store.ReplaceUI(context.Background(), invalidUI)
	require.Error(t, err)
	invalidUI = DefaultUIPreferences()
	invalidUI.ViewModes = map[string]string{"accounts.users": "grid"}
	_, err = store.ReplaceUI(context.Background(), invalidUI)
	require.Error(t, err)
	coreAfter, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	uiAfter, err := os.ReadFile(uiPath)
	require.NoError(t, err)
	require.Equal(t, coreBefore, coreAfter)
	require.Equal(t, uiBefore, uiAfter)
}

func TestUserStoreCoreUpdateReadsLatestDiskAndUIReplacementDoesNot(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	cfg := DefaultSettings(base)
	ui := DefaultUIPreferences()
	require.NoError(t, writeCoreConfig(cfgPath, *cfg))
	require.NoError(t, writeUIConfig(uiPath, ui))
	store := newUserStore("miguel", cfgPath, uiPath, cfg, &ui)

	externalCore := cloneSettings(cfg)
	externalCore.AppSettings.ChunkSizeMB = 8
	require.NoError(t, writeCoreConfig(cfgPath, *externalCore))
	externalUI := cloneUIPreferences(&ui)
	externalUI.Theme = ThemeLight
	require.NoError(t, writeUIConfig(uiPath, *externalUI))

	updatedCore, err := store.Update(context.Background(), func(value *Settings) error {
		value.AppSettings.ShowHiddenFiles = false
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 8, updatedCore.AppSettings.ChunkSizeMB)
	require.False(t, updatedCore.AppSettings.ShowHiddenFiles)

	replacement := DefaultUIPreferences()
	replacement.PrimaryColor = "#123456"
	updatedUI, err := store.ReplaceUI(context.Background(), replacement)
	require.NoError(t, err)
	require.Equal(t, replacement.Theme, updatedUI.Theme)
	require.Equal(t, CSSColor("#123456"), updatedUI.PrimaryColor)
}

func TestUserStoreUIReplacementDoesNotReadMalformedOldSnapshot(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	cfg := DefaultSettings(base)
	replacement := DefaultUIPreferences()
	replacement.Theme = ThemeDark
	require.NoError(t, writeCoreConfig(cfgPath, *cfg))
	require.NoError(t, os.WriteFile(uiPath, []byte("theme: [broken"), filePerm))
	store := newUserStore("miguel", cfgPath, uiPath, cfg, nil)

	updated, err := store.ReplaceUI(context.Background(), replacement)
	require.NoError(t, err)
	require.Equal(t, ThemeDark, updated.Theme)
	loaded, err := readUILatest(uiPath)
	require.NoError(t, err)
	require.Equal(t, ThemeDark, loaded.Theme)
}

func TestUserStoreMutationRejectsMalformedCoreWithoutRewriting(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	coreRaw := []byte("docker: [broken")
	require.NoError(t, os.WriteFile(cfgPath, coreRaw, filePerm))
	require.NoError(t, writeUIConfig(uiPath, DefaultUIPreferences()))
	store := newUserStore("miguel", cfgPath, uiPath, DefaultSettings(base), nil)

	_, err := store.Update(context.Background(), func(value *Settings) error {
		value.AppSettings.ShowHiddenFiles = false
		return nil
	})
	require.Error(t, err)
	rewritten, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, coreRaw, rewritten)
}

func TestUserStoresSerializeConcurrentCoreUpdates(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	cfg := DefaultSettings(base)
	ui := DefaultUIPreferences()
	require.NoError(t, writeCoreConfig(cfgPath, *cfg))
	require.NoError(t, writeUIConfig(uiPath, ui))
	first := newUserStore("miguel", cfgPath, uiPath, cfg, &ui)
	second := newUserStore("miguel", cfgPath, uiPath, cfg, &ui)

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, err := first.Update(context.Background(), func(value *Settings) error {
			value.AppSettings.ShowHiddenFiles = false
			return nil
		})
		errs <- err
	}()
	go func() {
		defer wg.Done()
		<-start
		_, err := second.Update(context.Background(), func(value *Settings) error {
			value.AppSettings.ChunkSizeMB = 8
			return nil
		})
		errs <- err
	}()
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	loaded, err := readCoreLatest(cfgPath, base)
	require.NoError(t, err)
	require.False(t, loaded.AppSettings.ShowHiddenFiles)
	require.Equal(t, 8, loaded.AppSettings.ChunkSizeMB)
}
