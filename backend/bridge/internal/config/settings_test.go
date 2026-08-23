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

func TestInitializeMigratesLegacyCombinedConfigWithoutResettingCoreState(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	legacy := "appSettings:\n" +
		"  theme: LIGHT\n" +
		"  primaryColor: '#123456'\n" +
		"  sidebarCollapsed: true\n" +
		"  navigationMode: dock\n" +
		"  dockTileColors: vibrant\n" +
		"  dockAccentGradient:\n" +
		"    startColor: '#101010'\n" +
		"    endColor: '#fefefe'\n" +
		"    rangeStart: 10\n" +
		"    rangeEnd: 90\n" +
		"  showHiddenFiles: false\n" +
		"  hiddenCards: [updates]\n" +
		"  dockerDashboardSections:\n" +
		"    overview: false\n" +
		"    monitoring: true\n" +
		"    daemon: false\n" +
		"    resources: true\n" +
		"  hardwareSections:\n" +
		"    overview: false\n" +
		"    hardware: true\n" +
		"    sensors: false\n" +
		"    systemInfo: true\n" +
		"    gpu: false\n" +
		"    pciDevices: true\n" +
		"    memoryModules: false\n" +
		"  viewModes:\n" +
		"    accounts.users: card\n" +
		"    docker.stacks: table\n" +
		"  dashboardOrder: [docker, system]\n" +
		"  containerOrder: [beta, alpha]\n" +
		"  chunkSizeMB: 8\n" +
		"  terminalFontSize: 20\n" +
		"docker:\n" +
		"  folders: [/srv/compose, /mnt/stacks]\n" +
		"  requireMountsForFolders: true\n" +
		"  proxy:\n" +
		"    caddyEnabled: true\n" +
		"    baseDomain: apps.example.test\n" +
		"    tlsEmail: admin@example.test\n" +
		"jobs:\n" +
		"  progressMinIntervalMs: 400\n" +
		"  notificationMinIntervalMs: 1200\n" +
		"  progressMinBytesMB: 32\n" +
		"  heavyArchiveConcurrency: 2\n" +
		"dismissals:\n" +
		"  uncleanShutdownBootId: boot-1\n" +
		"  failedLoginAlertId: login-1\n"
	require.NoError(t, os.WriteFile(cfgPath, []byte(legacy), filePerm))

	require.NoError(t, initializeLocked(cfgPath, uiPath, base))
	core, err := readConfigStrict(cfgPath)
	require.NoError(t, err)
	require.Equal(t, []AbsolutePath{"/srv/compose", "/mnt/stacks"}, core.Docker.Folders)
	require.True(t, core.Docker.RequireMountsForFolders)
	require.Equal(t, 8, core.AppSettings.ChunkSizeMB)
	require.False(t, core.AppSettings.ShowHiddenFiles)
	require.Equal(t, 400, core.Jobs.ProgressMinIntervalMs)
	require.Equal(t, "boot-1", core.Dismissals.UncleanShutdownBootID)

	ui, err := readUILatest(uiPath)
	require.NoError(t, err)
	require.Equal(t, ThemeLight, ui.Theme)
	require.Equal(t, CSSColor("#123456"), ui.PrimaryColor)
	require.NotNil(t, ui.ThemeColors)
	require.Equal(t, CSSColor("#D4D4D4"), *ui.ThemeColors.Dark.CodeText)
	require.True(t, ui.SidebarCollapsed)
	require.Equal(t, NavigationModeDock, ui.NavigationMode)
	require.Equal(t, []string{"updates"}, ui.HiddenCards)
	require.NotContains(t, ui.ViewModes, "accounts.users", "legacy card defaults must remain inherited")
	require.Equal(t, "table", ui.ViewModes["docker.stacks"])
	require.Equal(t, []string{"docker", "system"}, ui.LayoutOrders["dashboard"])
	require.Equal(t, []string{"beta", "alpha"}, ui.LayoutOrders["docker.containers"])
	require.Equal(t, 20, ui.TerminalFontSize)

	uiRaw, err := os.ReadFile(uiPath)
	require.NoError(t, err)
	require.NotEqual(t, "{}\n", string(uiRaw), "migration must preserve UI preferences")
}

func TestInitializeCompletesInterruptedLegacyConversionWithoutReplacingUI(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	legacy := []byte("appSettings:\n  theme: LIGHT\n" +
		"docker:\n  folders: [/srv/compose]\n" +
		"jobs: {}\n")
	existingUI := DefaultUIPreferences()
	existingUI.Theme = ThemeDark
	existingUI.PrimaryColor = "#123456"
	require.NoError(t, os.WriteFile(cfgPath, legacy, filePerm))
	require.NoError(t, writeUIConfig(uiPath, existingUI))
	uiBefore, err := os.ReadFile(uiPath)
	require.NoError(t, err)

	require.NoError(t, initializeLocked(cfgPath, uiPath, base))
	core, err := readConfigStrict(cfgPath)
	require.NoError(t, err)
	require.Equal(t, []AbsolutePath{"/srv/compose"}, core.Docker.Folders)
	uiAfter, err := os.ReadFile(uiPath)
	require.NoError(t, err)
	require.Equal(t, uiBefore, uiAfter)
}

func TestLegacyConversionResetsMalformedUISidecarWithoutResettingCore(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	legacy := []byte("appSettings:\n  theme: LIGHT\n" +
		"docker:\n  folders: [/srv/compose]\n" +
		"jobs: {}\n")
	require.NoError(t, os.WriteFile(cfgPath, legacy, filePerm))
	require.NoError(t, os.WriteFile(uiPath, []byte("theme: [broken"), filePerm))

	require.NoError(t, initializeLocked(cfgPath, uiPath, base))
	core, err := readConfigStrict(cfgPath)
	require.NoError(t, err)
	require.Equal(t, []AbsolutePath{"/srv/compose"}, core.Docker.Folders)
	uiAfter, err := os.ReadFile(uiPath)
	require.NoError(t, err)
	require.Equal(t, "{}\n", string(uiAfter))
}

func TestInvalidLegacyCombinedConfigIsNotRewritten(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	raw := []byte("appSettings:\n  theme: LIGHT\ndocker:\n  folders: [relative]\n")
	require.NoError(t, os.WriteFile(cfgPath, raw, filePerm))

	err := initializeLocked(cfgPath, uiPath, base)
	require.Error(t, err)
	rewritten, readErr := os.ReadFile(cfgPath)
	require.NoError(t, readErr)
	require.Equal(t, raw, rewritten)
	_, readErr = os.Lstat(uiPath)
	require.Error(t, readErr)
}

func TestAmbiguousLegacyConfigWithoutDockerIsNotRewritten(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	raw := []byte("appSettings:\n  theme: LIGHT\njobs: {}\n")
	require.NoError(t, os.WriteFile(cfgPath, raw, filePerm))

	err := initializeLocked(cfgPath, uiPath, base)
	require.Error(t, err)
	rewritten, readErr := os.ReadFile(cfgPath)
	require.NoError(t, readErr)
	require.Equal(t, raw, rewritten)
	_, readErr = os.Lstat(uiPath)
	require.ErrorIs(t, readErr, os.ErrNotExist)
}

func TestCoreSyntaxFailureDoesNotRewrite(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, cfgFileName)
	raw := []byte("docker: [broken")
	require.NoError(t, os.WriteFile(path, raw, filePerm))

	_, err := readCoreLatest(path, base)
	require.Error(t, err)
	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, raw, rewritten)
}

func TestCoreUnknownFieldDoesNotRewrite(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, cfgFileName)
	raw := []byte("appSettings:\n  showHiddenFiles: false\n" +
		"docker:\n  folders: [" + filepath.Join(base, "projects") + "]\n" +
		"jobs: {}\nunknown: true\n")
	require.NoError(t, os.WriteFile(path, raw, filePerm))

	_, err := readCoreLatest(path, base)
	require.Error(t, err)
	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, raw, rewritten)
}

func TestCoreSemanticFailureDoesNotRewrite(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, cfgFileName)
	raw := []byte("appSettings:\n  chunkSizeMB: 33\n" +
		"docker:\n  folders: [" + filepath.Join(base, "projects") + "]\n" +
		"jobs: {}\n")
	require.NoError(t, os.WriteFile(path, raw, filePerm))

	_, err := readCoreLatest(path, base)
	require.Error(t, err)
	rewritten, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, raw, rewritten)
}

func TestCoreStrictContentFailuresDoNotRewrite(t *testing.T) {
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

			_, err := readCoreLatest(path, base)
			require.Error(t, err)
			rewritten, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, []byte(contents), rewritten)
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

func TestCoreReadFailurePreservesInvalidDocument(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, cfgFileName)
	raw := []byte("docker: [broken")
	require.NoError(t, os.WriteFile(path, raw, filePerm))

	_, err := readCoreLatest(path, base)
	require.ErrorContains(t, err, "invalid core config")
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

func TestUIViewModesEqualToBackendDefaultRemainInherited(t *testing.T) {
	path := filepath.Join(t.TempDir(), uiCfgFileName)
	raw := []byte("viewModes:\n  accounts.users: card\n  docker.stacks: table\n")
	require.NoError(t, os.WriteFile(path, raw, filePerm))

	loaded, err := readUILatest(path)
	require.NoError(t, err)
	require.NotContains(t, loaded.ViewModes, "accounts.users")
	require.Equal(t, "table", loaded.ViewModes["docker.stacks"])
}

func TestInvalidCoreDoesNotClobberValidUI(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	require.NoError(t, os.WriteFile(cfgPath, []byte("unknown: true\n"), filePerm))
	persistedUI := DefaultUIPreferences()
	persistedUI.Theme = ThemeLight
	require.NoError(t, writeUIConfig(uiPath, persistedUI))

	require.Error(t, initializeLocked(cfgPath, uiPath, base))
	_, err := readCoreLatest(cfgPath, base)
	require.Error(t, err)
	ui, err := readUIConfigStrict(uiPath)
	require.NoError(t, err)
	require.Equal(t, ThemeLight, ui.Theme)
	rewritten, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Equal(t, []byte("unknown: true\n"), rewritten)
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
	loaded, err := readConfigStrict(cfgPath)
	require.NoError(t, err)
	require.Equal(t, cfg.Docker.Folders, loaded.Docker.Folders)
	_, err = readUILatest(uiPath)
	require.NoError(t, err)
	uiRaw, err := os.ReadFile(uiPath)
	require.NoError(t, err)
	require.Equal(t, "{}\n", string(uiRaw))
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
