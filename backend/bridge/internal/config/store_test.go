package config

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
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
		base:       filepath.Dir(cfgPath),
		mode:       StorageModeHome,
	}
	if cfg != nil {
		store.cfg = *cloneSettings(cfg)
	}
	if ui != nil {
		store.ui = *cloneUIPreferences(ui)
	}
	return store
}

func quarantinedCoreFiles(t *testing.T, cfgPath string) []string {
	t.Helper()
	matches, err := filepath.Glob(cfgPath + ".broken-*")
	require.NoError(t, err)
	return matches
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
	require.Empty(t, quarantinedCoreFiles(t, cfgPath))
}

func TestOpenUserStoreStorageFallbacks(t *testing.T) {
	owner := currentProcessFileOwnership()
	targetUID := uint32(os.Geteuid())

	t.Run("home", func(t *testing.T) {
		home := t.TempDir()
		store := openUserStore("miguel", targetUID, owner, home, nil)

		require.Equal(t, StorageModeHome, store.StorageMode())
		require.Equal(t, filepath.Join(home, cfgFileName), store.Path())
	})

	t.Run("persistent fallback", func(t *testing.T) {
		previousFallbackRoot := fallbackConfigRoot
		fallbackConfigRoot = filepath.Join(t.TempDir(), "users")
		t.Cleanup(func() { fallbackConfigRoot = previousFallbackRoot })
		store := openUserStore("miguel", targetUID, owner, "", errors.New("home unavailable"))

		fallbackBase := filepath.Join(fallbackConfigRoot, strconv.Itoa(os.Geteuid()))
		require.Equal(t, StorageModeFallback, store.StorageMode())
		require.Equal(t, filepath.Join(fallbackBase, cfgFileName), store.Path())
		cfg, err := store.Snapshot(context.Background())
		require.NoError(t, err)
		require.Equal(t, []AbsolutePath{AbsolutePath(filepath.Join(fallbackBase, "docker"))}, cfg.Docker.Folders)
		info, err := os.Stat(fallbackBase)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
	})

	t.Run("memory", func(t *testing.T) {
		previousFallbackRoot := fallbackConfigRoot
		fallbackConfigRoot = filepath.Join(t.TempDir(), "not-a-directory")
		t.Cleanup(func() { fallbackConfigRoot = previousFallbackRoot })
		require.NoError(t, os.WriteFile(fallbackConfigRoot, []byte("blocked"), filePerm))
		store := openUserStore("miguel", targetUID, owner, "", errors.New("home unavailable"))
		require.Equal(t, StorageModeMemory, store.StorageMode())
		require.Empty(t, store.Path())
		require.Empty(t, store.UIPath())

		updated, err := store.Update(context.Background(), func(cfg *Settings) error {
			cfg.AppSettings.ShowHiddenFiles = false
			return nil
		})
		require.NoError(t, err)
		require.False(t, updated.AppSettings.ShowHiddenFiles)
		ui := DefaultUIPreferences()
		ui.Theme = ThemeLight
		updatedUI, err := store.ReplaceUI(context.Background(), ui)
		require.NoError(t, err)
		require.Equal(t, ThemeLight, updatedUI.Theme)
	})
}

func TestQuarantineCoreConfigUsesNumberedSuffix(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	timestamp := "20260829T120000Z"
	firstPath := cfgPath + ".broken-" + timestamp
	current := []byte("current invalid config")
	earlier := []byte("earlier invalid config")
	require.NoError(t, os.WriteFile(cfgPath, current, filePerm))
	require.NoError(t, os.WriteFile(firstPath, earlier, filePerm))

	quarantinePath, err := quarantineCoreConfig(cfgPath, timestamp)
	require.NoError(t, err)
	require.Equal(t, firstPath+"(2)", quarantinePath)
	first, err := os.ReadFile(firstPath)
	require.NoError(t, err)
	require.Equal(t, earlier, first)
	second, err := os.ReadFile(quarantinePath)
	require.NoError(t, err)
	require.Equal(t, current, second)
}

func TestLoadCoreOrQuarantineKeepsValidCore(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	cfg := DefaultSettings(base)
	cfg.Docker.Folders = []AbsolutePath{"/srv/linuxio-projects"}
	require.NoError(t, writeCoreConfig(cfgPath, *cfg))
	before, err := os.ReadFile(cfgPath)
	require.NoError(t, err)

	loaded, err := loadCoreOrQuarantineOwned(cfgPath, base, currentProcessFileOwnership())
	require.NoError(t, err)
	require.Equal(t, cfg, loaded)
	after, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Empty(t, quarantinedCoreFiles(t, cfgPath))
}

func TestLoadCoreOrQuarantineReplacesInvalidCore(t *testing.T) {
	tests := map[string]string{
		"syntax error":       "docker: [broken",
		"unknown field":      "docker:\n  folders: [/srv/projects]\nunknown: true\n",
		"semantic failure":   "appSettings:\n  chunkSizeMB: 33\ndocker:\n  folders: [/srv/projects]\n",
		"empty document":     "# comments only\n",
		"typed path failure": "docker:\n  folders: [relative]\n",
		"multiple documents": "{}\n---\n{}\n",
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			cfgPath := filepath.Join(base, cfgFileName)
			require.NoError(t, os.WriteFile(cfgPath, []byte(contents), filePerm))

			loaded, err := loadCoreOrQuarantineOwned(cfgPath, base, currentProcessFileOwnership())
			require.NoError(t, err)
			require.Equal(t, DefaultSettings(base), loaded)

			persisted, err := readConfigStrict(cfgPath)
			require.NoError(t, err)
			require.Equal(t, DefaultSettings(base), persisted)

			quarantined := quarantinedCoreFiles(t, cfgPath)
			require.Len(t, quarantined, 1)
			raw, err := os.ReadFile(quarantined[0])
			require.NoError(t, err)
			require.Equal(t, []byte(contents), raw)
		})
	}
}

func TestLoadCoreOrQuarantineDoesNotTouchNonParseFailures(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		base := t.TempDir()
		cfgPath := filepath.Join(base, cfgFileName)
		target := filepath.Join(base, "real.yaml")
		require.NoError(t, os.WriteFile(target, []byte("docker: [broken"), filePerm))
		require.NoError(t, os.Symlink(target, cfgPath))

		_, err := loadCoreOrQuarantineOwned(cfgPath, base, currentProcessFileOwnership())
		require.ErrorContains(t, err, "symlink")
		info, err := os.Lstat(cfgPath)
		require.NoError(t, err)
		require.NotZero(t, info.Mode()&os.ModeSymlink)
		require.Empty(t, quarantinedCoreFiles(t, cfgPath))
	})
	t.Run("directory", func(t *testing.T) {
		base := t.TempDir()
		cfgPath := filepath.Join(base, cfgFileName)
		require.NoError(t, os.Mkdir(cfgPath, dirPerm))

		_, err := loadCoreOrQuarantineOwned(cfgPath, base, currentProcessFileOwnership())
		require.ErrorContains(t, err, "not a regular file")
		require.Empty(t, quarantinedCoreFiles(t, cfgPath))
	})
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
