package userconfig

import (
	"os"
	"path/filepath"
	"reflect"
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
	cfg := defaultSettings(base)

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
	cfg := defaultSettings(base)
	cfg.AppSettings.ShowHiddenFiles = false
	cfg.AppSettings.ChunkSizeMB = 0
	cfg.Jobs.ProgressMinBytesMB = 0
	require.NoError(t, writeCoreConfig(path, *cfg))

	reloaded, err := readCoreLatest(path, base)
	require.NoError(t, err)
	require.False(t, reloaded.AppSettings.ShowHiddenFiles)
	require.Zero(t, reloaded.AppSettings.ChunkSizeMB)
	require.Zero(t, reloaded.Jobs.ProgressMinBytesMB)
}

func TestParseCoreConfigAcceptsRemovedHeavyArchiveConcurrency(t *testing.T) {
	base := t.TempDir()
	cfg, err := parseCoreConfig([]byte("jobs:\n  heavyArchiveConcurrency: 4\n  progressMinBytesMB: 3\n"), base)
	require.NoError(t, err)
	require.Equal(t, 3, cfg.Jobs.ProgressMinBytesMB)
	require.Nil(t, cfg.Jobs.LegacyHeavyArchiveConcurrency)

	out, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(out), "heavyArchiveConcurrency")
}

func TestInitializeCreatesMissingFilesIndependently(t *testing.T) {
	base := t.TempDir()
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)

	require.NoError(t, initializeLocked(cfgPath, uiPath, base))

	cfg, err := readConfigStrict(cfgPath)
	require.NoError(t, err)
	require.Equal(t, defaultSettings(base), cfg)
	ui, err := readUIConfigStrict(uiPath)
	require.NoError(t, err)
	require.Empty(t, ui)
	require.Equal(t, DefaultUIPreferences(), *parseUIFromRaw(t, []byte("{}")))
	raw, err := os.ReadFile(uiPath)
	require.NoError(t, err)
	require.Equal(t, "{}\n", string(raw))
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
	require.Equal(t, defaultSettings(base), loaded)
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
		"empty document":          "# comments only\n",
		"typed theme failure":     "theme: PURPLE\n",
		"multiple documents":      "{}\n---\n{}\n",
		"removed section toggles": "theme: LIGHT\nhardwareSections:\n  sensors: false\n",
		"blank section name":      "collapsedSections:\n  hardware: ['']\n",
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
		"collapsedSections:\n  docker.dashboard: [overview]\n" +
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
	require.Equal(t, map[string][]string{"docker.dashboard": {"overview"}}, loaded.CollapsedSections)
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

	require.NoError(t, initializeLocked(cfgPath, uiPath, base))
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
	cfg := defaultSettings(base)
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
	cfg := defaultSettings(base)
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
	cfg := defaultSettings(base)
	cfg.Docker.Folders = []AbsolutePath{validMissing}
	require.Empty(t, validateSettings(cfg))

	for _, folders := range [][]AbsolutePath{
		{}, {"relative"}, {"/"}, {validMissing, validMissing},
	} {
		cfg.Docker.Folders = folders
		require.NotEmpty(t, validateSettings(cfg), folders)
	}
}

// nonDefaultUI changes every UIPreferences field from its default.
func nonDefaultUI() UIPreferences {
	ui := DefaultUIPreferences()
	ui.Theme = ThemeLight
	ui.PrimaryColor = "#123456"
	ui.ThemeColors.Dark["codeText"] = "#111111"
	ui.ThemeColors.Light["codeInsetBackground"] = "#222222"
	ui.SidebarCollapsed = true
	ui.NavigationMode = NavigationModeDock
	ui.DockTileColors = DockTileColorsMono
	ui.DockAccentGradient = &DockAccentGradient{StartColor: "#333333", RangeStart: 10, RangeEnd: 90}
	ui.HiddenCards = []string{"cpu"}
	ui.CollapsedSections = map[string][]string{"hardware": {"sensors"}}
	ui.ViewModes = map[string]string{"docker.stacks": ViewModeTable}
	ui.LayoutOrders = map[string][]string{"dashboard": {"memory", "cpu"}}
	ui.TerminalFontSize = 20
	return ui
}

// The saved file holds only what differs from the defaults, and reading it
// over the defaults gives the same preferences back.
func TestUIPreferencesAreStoredAsDifferencesFromTheDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), uiCfgFileName)

	require.NoError(t, writeUIConfig(path, DefaultUIPreferences()))
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "{}\n", string(raw))
	reloaded, err := readUI(path)
	require.NoError(t, err)
	require.Equal(t, DefaultUIPreferences(), *reloaded)

	ui := DefaultUIPreferences()
	ui.Theme = ThemeLight
	ui.ThemeColors.Dark["codeText"] = "#111111"
	require.NoError(t, writeUIConfig(path, ui))
	raw, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "theme: LIGHT\nthemeColors:\n  dark:\n    codeText: \"#111111\"\n", string(raw))
	reloaded, err = readUI(path)
	require.NoError(t, err)
	require.Equal(t, ui, *reloaded)
}

// Every field is written when it differs: a field added to UIPreferences that
// the writer dropped would fail here. The first test keeps this one honest by
// requiring nonDefaultUI to change every field.
func TestEveryUIPreferenceSurvivesAWriteAndRead(t *testing.T) {
	defaults, changed := reflect.ValueOf(DefaultUIPreferences()), reflect.ValueOf(nonDefaultUI())
	for i := range defaults.NumField() {
		name := defaults.Type().Field(i).Name
		require.False(t, reflect.DeepEqual(defaults.Field(i).Interface(), changed.Field(i).Interface()), "nonDefaultUI leaves %s at its default", name)
	}

	path := filepath.Join(t.TempDir(), uiCfgFileName)
	ui := nonDefaultUI()
	require.NoError(t, writeUIConfig(path, ui))
	reloaded, err := readUI(path)
	require.NoError(t, err)
	require.Equal(t, ui, *reloaded)
}

func parseUIFromRaw(t *testing.T, raw []byte) *UIPreferences {
	t.Helper()
	ui, err := parseUIConfig(raw)
	require.NoError(t, err)
	return ui
}
