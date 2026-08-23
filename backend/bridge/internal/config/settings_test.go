package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

func readConfigStrict(path string) (*Settings, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out Settings
	if err := yaml.UnmarshalWithOptions(b, &out, yaml.Strict()); err != nil {
		return nil, err
	}
	return &out, nil
}

func readUIConfigStrict(path string) (*UIPreferences, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out UIPreferences
	if err := yaml.UnmarshalWithOptions(b, &out, yaml.Strict()); err != nil {
		return nil, err
	}
	return &out, nil
}

func TestDefaultSettingsContainFunctionalDefaultsOnly(t *testing.T) {
	base := t.TempDir()
	cfg := DefaultSettings(base)

	require.True(t, cfg.AppSettings.ShowHiddenFiles)
	require.Equal(t, 1, cfg.AppSettings.ChunkSizeMB)
	require.Equal(t, []AbsolutePath{AbsolutePath(filepath.Join(base, "docker"))}, cfg.Docker.Folders)
	ui := DefaultUIPreferences()
	require.Equal(t, ThemeDark, ui.Theme)
	require.Equal(t, CSSColor("#2196f3"), ui.PrimaryColor)
	require.Equal(t, NavigationModeSidebar, ui.NavigationMode)
	require.Equal(t, DockTileColorsAccent, ui.DockTileColors)
	require.Equal(t, 16, ui.TerminalFontSize)
	require.Empty(t, ui.HiddenCards)
	require.Empty(t, ui.ViewModes)
	require.Empty(t, ui.LayoutOrders)
}

func TestCoreExplicitFalseAndZeroSurviveValidatedRoundTrip(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, cfgFileName)
	cfg := DefaultSettings(base)
	cfg.AppSettings.ShowHiddenFiles = false
	cfg.AppSettings.ChunkSizeMB = 0
	cfg.Jobs.HeavyArchiveConcurrency = 0
	require.NoError(t, writeCoreConfig(path, *cfg))

	reloaded, err := readCoreLatest(path, base)
	require.NoError(t, err)
	require.False(t, reloaded.AppSettings.ShowHiddenFiles)
	require.Zero(t, reloaded.AppSettings.ChunkSizeMB)
	require.Zero(t, reloaded.Jobs.HeavyArchiveConcurrency)
}

func TestInitializeCreatesMissingFilesIndependently(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)

	require.NoError(t, initializeLocked(cfgPath, uiPath, base))

	cfg, err := readConfigStrict(cfgPath)
	require.NoError(t, err)
	require.Equal(t, DefaultSettings(base), cfg)
	ui, err := readUIConfigStrict(uiPath)
	require.NoError(t, err)
	require.Empty(t, ui)
	require.Equal(t, DefaultUIPreferences(), *parseUIFromRaw(t, []byte("{}"), uiPath))
	raw, err := os.ReadFile(uiPath)
	require.NoError(t, err)
	require.Equal(t, "{}\n", string(raw))
}

func TestCoreSyntaxFailureResetsToDefaults(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, cfgFileName)
	require.NoError(t, os.WriteFile(path, []byte("docker: [broken"), filePerm))

	replacement, err := readCoreLatest(path, base)
	require.NoError(t, err)
	require.Equal(t, DefaultSettings(base), replacement)
	reloaded, err := readConfigStrict(path)
	require.NoError(t, err)
	require.Equal(t, DefaultSettings(base), reloaded)
}

func TestCoreUnknownFieldResetsWholeDocument(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, cfgFileName)
	raw := []byte("appSettings:\n  showHiddenFiles: false\n" +
		"docker:\n  folders: [" + filepath.Join(base, "projects") + "]\n" +
		"jobs: {}\nunknown: true\n")
	require.NoError(t, os.WriteFile(path, raw, filePerm))

	replacement, err := readCoreLatest(path, base)
	require.NoError(t, err)
	require.Equal(t, DefaultSettings(base), replacement)
	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotContains(t, string(rewritten), "unknown:")
}

func TestCoreSemanticFailureResetsWholeDocument(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, cfgFileName)
	raw := []byte("appSettings:\n  chunkSizeMB: 33\n" +
		"docker:\n  folders: [" + filepath.Join(base, "projects") + "]\n" +
		"jobs: {}\n")
	require.NoError(t, os.WriteFile(path, raw, filePerm))

	replacement, err := readCoreLatest(path, base)
	require.NoError(t, err)
	require.Equal(t, DefaultSettings(base), replacement)
}

func TestCoreStrictContentFailuresResetWholeDocument(t *testing.T) {
	tests := map[string]string{
		"empty document":     "# comments only\n",
		"typed path failure": "docker:\n  folders: [relative]\n",
		"multiple documents": "{}\n---\n{}\n",
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			base := t.TempDir()
			path := filepath.Join(base, cfgFileName)
			require.NoError(t, os.WriteFile(path, []byte(contents), filePerm))

			replacement, err := readCoreLatest(path, base)
			require.NoError(t, err)
			require.Equal(t, DefaultSettings(base), replacement)
		})
	}
}

func TestCoreOmittedFieldsUseDefaultsWithoutRewrite(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, cfgFileName)
	raw := []byte("{}\n")
	require.NoError(t, os.WriteFile(path, raw, filePerm))

	loaded, err := readCoreLatest(path, base)
	require.NoError(t, err)
	require.Equal(t, DefaultSettings(base), loaded)
	persisted, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, raw, persisted)
}

func TestCoreResetPreCommitWriteFailurePreservesInvalidDocument(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write through directory permission restrictions")
	}
	base := t.TempDir()
	path := filepath.Join(base, cfgFileName)
	raw := []byte("docker: [broken")
	require.NoError(t, os.WriteFile(path, raw, filePerm))
	require.NoError(t, os.Chmod(base, 0o500))
	t.Cleanup(func() { _ = os.Chmod(base, 0o700) })

	_, err := readCoreLatest(path, base)
	require.ErrorContains(t, err, "invalid core config")
	require.ErrorContains(t, err, "reset core config")
	require.NoError(t, os.Chmod(base, 0o700))
	persisted, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	require.Equal(t, raw, persisted)
}

func TestUIUnknownFieldResetsWholeDocument(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, uiCfgFileName)
	raw := []byte("theme: LIGHT\nunknown: true\n")
	require.NoError(t, os.WriteFile(path, raw, filePerm))

	replacement, err := readUILatest(path)
	require.NoError(t, err)
	require.Equal(t, DefaultUIPreferences(), *replacement)
	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "{}\n", string(rewritten))
}

func TestUISemanticFailureResetsWholeDocument(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, uiCfgFileName)
	require.NoError(t, os.WriteFile(path, []byte("navigationMode: floating\n"), filePerm))

	replacement, err := readUILatest(path)
	require.NoError(t, err)
	require.Equal(t, DefaultUIPreferences(), *replacement)
	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "{}\n", string(rewritten))
}

func TestUIStrictContentFailuresResetWholeDocument(t *testing.T) {
	tests := map[string]string{
		"empty document":      "# comments only\n",
		"typed theme failure": "theme: PURPLE\n",
		"multiple documents":  "{}\n---\n{}\n",
	}
	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), uiCfgFileName)
			require.NoError(t, os.WriteFile(path, []byte(contents), filePerm))

			replacement, err := readUILatest(path)
			require.NoError(t, err)
			require.Equal(t, DefaultUIPreferences(), *replacement)
			rewritten, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, "{}\n", string(rewritten))
		})
	}
}

func TestSparseUIConfigLoadsWithBackendDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), uiCfgFileName)
	raw := []byte("theme: LIGHT\n" +
		"dockerDashboardSections:\n  overview: false\n" +
		"dockAccentGradient:\n  startColor: '#123456'\n")
	require.NoError(t, os.WriteFile(path, raw, filePerm))

	loaded, err := readUILatest(path)
	require.NoError(t, err)
	require.Equal(t, ThemeLight, loaded.Theme)
	require.Equal(t, CSSColor("#2196f3"), loaded.PrimaryColor)
	require.False(t, loaded.SidebarCollapsed)
	require.Equal(t, NavigationModeSidebar, loaded.NavigationMode)
	require.Equal(t, DockTileColorsAccent, loaded.DockTileColors)
	require.Equal(t, 0, loaded.DockAccentGradient.RangeStart)
	require.Equal(t, 100, loaded.DockAccentGradient.RangeEnd)
	require.False(t, loaded.DockerDashboardSections.Overview)
	require.True(t, loaded.DockerDashboardSections.Monitoring)
	require.True(t, loaded.HardwareSections.Overview)
	require.Equal(t, 16, loaded.TerminalFontSize)
	require.Empty(t, loaded.ViewModes)
	require.Empty(t, loaded.LayoutOrders)
}

func TestInvalidCoreDoesNotClobberValidUI(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	require.NoError(t, os.WriteFile(cfgPath, []byte("unknown: true\n"), filePerm))
	persistedUI := DefaultUIPreferences()
	persistedUI.Theme = ThemeLight
	require.NoError(t, writeUIConfig(uiPath, persistedUI))

	require.NoError(t, initializeLocked(cfgPath, uiPath, base))
	_, err := readCoreLatest(cfgPath, base)
	require.NoError(t, err)
	_, err = readUILatest(uiPath)
	require.NoError(t, err)
	cfg, err := readConfigStrict(cfgPath)
	require.NoError(t, err)
	require.Equal(t, DefaultSettings(base), cfg)
	ui, err := readUIConfigStrict(uiPath)
	require.NoError(t, err)
	require.Equal(t, ThemeLight, ui.Theme)
}

func TestInvalidUIDoesNotClobberValidCore(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	cfg := DefaultSettings(base)
	cfg.Docker.Folders = []AbsolutePath{"/srv/linuxio-projects"}
	require.NoError(t, writeCoreConfig(cfgPath, *cfg))
	require.NoError(t, os.WriteFile(uiPath, []byte("theme: PURPLE\n"), filePerm))

	require.NoError(t, initializeLocked(cfgPath, uiPath, base))
	_, err := readCoreLatest(cfgPath, base)
	require.NoError(t, err)
	_, err = readUILatest(uiPath)
	require.NoError(t, err)
	loaded, err := readConfigStrict(cfgPath)
	require.NoError(t, err)
	require.Equal(t, cfg.Docker.Folders, loaded.Docker.Folders)
	ui, err := readUIConfigStrict(uiPath)
	require.NoError(t, err)
	require.Empty(t, ui)
}

func TestDockerFoldersAreNotRepairedFromFilesystemState(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, cfgFileName)
	folder := filepath.Join(base, "docker-file")
	require.NoError(t, os.WriteFile(folder, []byte("keep this file"), filePerm))
	cfg := DefaultSettings(base)
	cfg.Docker.Folders = []AbsolutePath{AbsolutePath(folder)}
	require.NoError(t, writeCoreConfig(path, *cfg))

	loaded, err := readCoreLatest(path, base)
	require.NoError(t, err)
	require.Equal(t, []AbsolutePath{AbsolutePath(folder)}, loaded.Docker.Folders)
	_, err = os.Stat(folder)
	require.NoError(t, err)
}

func TestValidateConfigDockerFoldersIsStructuralOnly(t *testing.T) {
	base := t.TempDir()
	validMissing := AbsolutePath(filepath.Join(base, "does-not-exist"))
	cfg := DefaultSettings(base)
	cfg.Docker.Folders = []AbsolutePath{validMissing}
	require.Empty(t, ValidateConfig(cfg))

	for _, folders := range [][]AbsolutePath{
		{}, {"relative"}, {"/"}, {validMissing, validMissing},
	} {
		cfg.Docker.Folders = folders
		require.NotEmpty(t, ValidateConfig(cfg), folders)
	}
}

func TestUIPreferencesMarshalWritesCompleteSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), uiCfgFileName)
	ui := DefaultUIPreferences()
	require.NoError(t, writeUIConfig(path, ui))

	reloaded, err := readUIConfigStrict(path)
	require.NoError(t, err)
	require.Equal(t, ui, *reloaded)
}

func parseUIFromRaw(t *testing.T, raw []byte, path string) *UIPreferences {
	t.Helper()
	ui, err := parseUIConfig(raw, path)
	require.NoError(t, err)
	return ui
}
