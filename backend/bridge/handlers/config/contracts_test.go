package config

import (
	"encoding/json"
	"reflect"
	"testing"

	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func TestAppConfigToAPIPreservesPersistedJSONShape(t *testing.T) {
	light := bridgeconfig.CSSColor("#112233")
	dark := bridgeconfig.CSSColor("rgb(4, 5, 6)")
	value := bridgeconfig.Settings{
		AppSettings: bridgeconfig.PersistedAppSettings{
			Theme:              bridgeconfig.ThemeDark,
			PrimaryColor:       bridgeconfig.CSSColor("#abcdef"),
			DockAccentGradient: bridgeconfig.DockAccentGradient{StartColor: "#112233", EndColor: "#aabbcc", RangeStart: 15, RangeEnd: 85},
			SidebarCollapsed:   true,
			ShowHiddenFiles:    true,
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
			ChunkSizeMB:      8,
			TerminalFontSize: 18,
		},
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

	want := decodeConfigJSON(t, value)
	got := decodeConfigJSON(t, appConfigToAPI(value))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("converted config JSON differs:\n got: %#v\nwant: %#v", got, want)
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
