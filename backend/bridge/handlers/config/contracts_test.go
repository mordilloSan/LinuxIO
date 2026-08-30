package config

import (
	"encoding/json"
	"reflect"
	"testing"

	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func TestConfigConversionsPreservePersistedJSONShapes(t *testing.T) {
	light := bridgeconfig.CSSColor("#112233")
	dark := bridgeconfig.CSSColor("rgb(4, 5, 6)")
	core := bridgeconfig.Settings{
		AppSettings: bridgeconfig.PersistedAppSettings{ShowHiddenFiles: true, ChunkSizeMB: 8},
		Docker: bridgeconfig.Docker{
			Folders:                 []bridgeconfig.AbsolutePath{"/srv/docker", "/mnt/apps"},
			RequireMountsForFolders: true,
			Proxy: bridgeconfig.DockerProxy{
				CaddyEnabled: true,
				BaseDomain:   "apps.example.test",
				TLSEmail:     "admin@example.test",
			},
		},
		Jobs: bridgeconfig.PersistedJobSettings{
			ProgressMinIntervalMs:     100,
			NotificationMinIntervalMs: 200,
			ProgressMinBytesMB:        3,
			HeavyArchiveConcurrency:   4,
			ArchiveCompressionWorkers: 5,
			ArchiveExtractWorkers:     6,
		},
		Dismissals: &bridgeconfig.PersistedDismissals{
			UncleanShutdownBootID: "boot-id",
			FailedLoginAlertID:    "login-id",
		},
	}
	ui := bridgeconfig.UIPreferences{
		Theme:              bridgeconfig.ThemeDark,
		PrimaryColor:       bridgeconfig.CSSColor("#abcdef"),
		NavigationMode:     bridgeconfig.NavigationModeSidebar,
		DockTileColors:     bridgeconfig.DockTileColorsAccent,
		DockAccentGradient: &bridgeconfig.DockAccentGradient{StartColor: "#112233", EndColor: "#aabbcc", RangeStart: 15, RangeEnd: 85},
		SidebarCollapsed:   true,
		HiddenCards:        []string{"updates"},
		LayoutOrders: map[string][]string{
			"dashboard":         {"system", "docker"},
			"docker.containers": {"alpha", "beta"},
		},
		DockerDashboardSections: &bridgeconfig.DockerDashboardSections{
			Overview: true, Daemon: false, Resources: true,
		},
		HardwareSections: &bridgeconfig.HardwareSections{
			Overview: true, Hardware: false, Sensors: true, SystemInfo: false,
			GPU: true, PCIDevices: false, MemoryModules: true,
		},
		ThemeColors: &bridgeconfig.ThemeColorsByMode{
			Light: &bridgeconfig.ThemeColors{BackgroundDefault: &light},
			Dark:  &bridgeconfig.ThemeColors{CodeText: &dark},
		},
		ViewModes:        map[string]string{"docker": "table", "storage": "card"},
		TerminalFontSize: 18,
	}

	wantCore := decodeConfigJSON(t, core)
	wantCore["storageMode"] = "home"
	wantUI := decodeConfigJSON(t, ui)
	wantUI["viewModeDefault"] = "card"
	if got := decodeConfigJSON(t, appConfigToAPI(core, bridgeconfig.StorageModeHome)); !reflect.DeepEqual(got, wantCore) {
		t.Fatalf("converted core config JSON differs:\n got: %#v\nwant: %#v", got, wantCore)
	}
	if got := decodeConfigJSON(t, uiConfigToAPI(ui)); !reflect.DeepEqual(got, wantUI) {
		t.Fatalf("converted UI config JSON differs:\n got: %#v\nwant: %#v", got, wantUI)
	}
}

func TestUIConfigToAPIIncludesBackendDefaults(t *testing.T) {
	got := decodeConfigJSON(t, uiConfigToAPI(bridgeconfig.DefaultUIPreferences()))
	if got["theme"] != "DARK" || got["primaryColor"] != "#2196f3" {
		t.Fatalf("backend UI defaults missing: %#v", got)
	}
	if got["viewModeDefault"] != "card" {
		t.Fatalf("view mode default missing: %#v", got)
	}
	themeColors, ok := got["themeColors"].(map[string]any)
	if !ok {
		t.Fatalf("backend theme-color defaults missing: %#v", got)
	}
	dark, ok := themeColors["dark"].(map[string]any)
	if !ok || dark["codeText"] != "#D4D4D4" {
		t.Fatalf("backend dark theme-color defaults missing: %#v", themeColors)
	}
}

func TestUIConfigToAPIHandlesIncompleteValue(t *testing.T) {
	got := uiConfigToAPI(bridgeconfig.UIPreferences{})
	if got.Theme != "DARK" || got.NavigationMode != "sidebar" || got.ViewModeDefault != "card" {
		t.Fatalf("incomplete UI value did not receive safe defaults: %#v", got)
	}
}

func decodeConfigJSON(t *testing.T, value any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}
