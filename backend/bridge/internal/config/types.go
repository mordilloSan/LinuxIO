package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// Settings holds the important per-user configuration persisted in the core
// file. UI preferences live in UIPreferences and are intentionally kept in a
// separate file owned by the same UserStore.
type Settings struct {
	AppSettings PersistedAppSettings `json:"appSettings" yaml:"appSettings"`
	Docker      Docker               `json:"docker" yaml:"docker"`
	Jobs        PersistedJobSettings `json:"jobs" yaml:"jobs"`
	Dismissals  *PersistedDismissals `json:"dismissals,omitempty" yaml:"dismissals,omitempty"`
}

// UIPreferences holds effective backend-owned UI preferences. The on-disk
// empty-document sentinel is represented separately from this runtime value.
type UIPreferences struct {
	Theme                   PersistedTheme           `json:"theme" yaml:"theme"`
	PrimaryColor            CSSColor                 `json:"primaryColor" yaml:"primaryColor"`
	ThemeColors             *ThemeColorsByMode       `json:"themeColors,omitempty" yaml:"themeColors,omitempty"`
	SidebarCollapsed        bool                     `json:"sidebarCollapsed" yaml:"sidebarCollapsed"`
	NavigationMode          string                   `json:"navigationMode" yaml:"navigationMode"`
	DockTileColors          string                   `json:"dockTileColors" yaml:"dockTileColors"`
	DockAccentGradient      *DockAccentGradient      `json:"dockAccentGradient" yaml:"dockAccentGradient"`
	HiddenCards             []string                 `json:"hiddenCards" yaml:"hiddenCards"`
	DockerDashboardSections *DockerDashboardSections `json:"dockerDashboardSections" yaml:"dockerDashboardSections"`
	HardwareSections        *HardwareSections        `json:"hardwareSections" yaml:"hardwareSections"`
	ViewModes               map[string]string        `json:"viewModes" yaml:"viewModes"`
	LayoutOrders            map[string][]string      `json:"layoutOrders" yaml:"layoutOrders"`
	TerminalFontSize        int                      `json:"terminalFontSize" yaml:"terminalFontSize"`
}

// PersistedDismissals records per-user acknowledgements of one-shot health signals.
// The identifier is matched against the live signal — a new event produces a
// different identifier and re-flags automatically.
type PersistedDismissals struct {
	UncleanShutdownBootID string `json:"uncleanShutdownBootId,omitempty" yaml:"uncleanShutdownBootId,omitempty"`
	FailedLoginAlertID    string `json:"failedLoginAlertId,omitempty" yaml:"failedLoginAlertId,omitempty"`
}

// DockerDashboardSections holds the collapsed state of each Docker dashboard section
type DockerDashboardSections struct {
	Overview   bool `json:"overview" yaml:"overview"`
	Monitoring bool `json:"monitoring" yaml:"monitoring"`
	Daemon     bool `json:"daemon" yaml:"daemon"`
	Resources  bool `json:"resources" yaml:"resources"`
}

// HardwareSections holds the visibility state of each hardware dashboard section
type HardwareSections struct {
	Overview      bool `json:"overview" yaml:"overview"`
	Hardware      bool `json:"hardware" yaml:"hardware"`
	Sensors       bool `json:"sensors" yaml:"sensors"`
	SystemInfo    bool `json:"systemInfo" yaml:"systemInfo"`
	GPU           bool `json:"gpu" yaml:"gpu"`
	PCIDevices    bool `json:"pciDevices" yaml:"pciDevices"`
	MemoryModules bool `json:"memoryModules" yaml:"memoryModules"`
}

// ThemeColorsByMode holds separate color overrides for light and dark mode
type ThemeColorsByMode struct {
	Light *ThemeColors `json:"light,omitempty" yaml:"light,omitempty"`
	Dark  *ThemeColors `json:"dark,omitempty" yaml:"dark,omitempty"`
}

// ThemeColors holds optional per-field color overrides for the UI theme
type ThemeColors struct {
	BackgroundDefault               *CSSColor `json:"backgroundDefault,omitempty" yaml:"backgroundDefault,omitempty"`
	BackgroundPaper                 *CSSColor `json:"backgroundPaper,omitempty" yaml:"backgroundPaper,omitempty"`
	HeaderBackground                *CSSColor `json:"headerBackground,omitempty" yaml:"headerBackground,omitempty"`
	FooterBackground                *CSSColor `json:"footerBackground,omitempty" yaml:"footerBackground,omitempty"`
	SidebarBackground               *CSSColor `json:"sidebarBackground,omitempty" yaml:"sidebarBackground,omitempty"`
	CardBackground                  *CSSColor `json:"cardBackground,omitempty" yaml:"cardBackground,omitempty"`
	DialogBorder                    *CSSColor `json:"dialogBorder,omitempty" yaml:"dialogBorder,omitempty"`
	DialogGlow                      *CSSColor `json:"dialogGlow,omitempty" yaml:"dialogGlow,omitempty"`
	DialogBackdrop                  *CSSColor `json:"dialogBackdrop,omitempty" yaml:"dialogBackdrop,omitempty"`
	CodeBackground                  *CSSColor `json:"codeBackground,omitempty" yaml:"codeBackground,omitempty"`
	CodeText                        *CSSColor `json:"codeText,omitempty" yaml:"codeText,omitempty"`
	ChartRx                         *CSSColor `json:"chartRx,omitempty" yaml:"chartRx,omitempty"`
	ChartTx                         *CSSColor `json:"chartTx,omitempty" yaml:"chartTx,omitempty"`
	ChartNeutral                    *CSSColor `json:"chartNeutral,omitempty" yaml:"chartNeutral,omitempty"`
	FileBrowserSurface              *CSSColor `json:"fileBrowserSurface,omitempty" yaml:"fileBrowserSurface,omitempty"`
	FileBrowserChrome               *CSSColor `json:"fileBrowserChrome,omitempty" yaml:"fileBrowserChrome,omitempty"`
	FileBrowserBreadcrumbBackground *CSSColor `json:"fileBrowserBreadcrumbBackground,omitempty" yaml:"fileBrowserBreadcrumbBackground,omitempty"`
	FileBrowserBreadcrumbText       *CSSColor `json:"fileBrowserBreadcrumbText,omitempty" yaml:"fileBrowserBreadcrumbText,omitempty"`
}

// DockAccentGradient controls the generated accent palette used by dock tiles.
// Empty colors derive their values from the active theme accent.
type DockAccentGradient struct {
	StartColor CSSColor `json:"startColor,omitempty" yaml:"startColor,omitempty"`
	EndColor   CSSColor `json:"endColor,omitempty" yaml:"endColor,omitempty"`
	RangeStart int      `json:"rangeStart" yaml:"rangeStart"`
	RangeEnd   int      `json:"rangeEnd" yaml:"rangeEnd"`
}

// PersistedAppSettings holds the important app settings. Presentation fields
// are in UIPreferences and persisted separately.
type PersistedAppSettings struct {
	ShowHiddenFiles bool `json:"showHiddenFiles" yaml:"showHiddenFiles"`
	// ChunkSizeMB is the file-transfer chunk size in MiB (1–32). 0 = use default (1 MiB).
	ChunkSizeMB int `json:"chunkSizeMB" yaml:"chunkSizeMB"`
}

// DockerProxy holds Caddy reverse proxy configuration
type DockerProxy struct {
	CaddyEnabled bool   `json:"caddyEnabled" yaml:"caddyEnabled"`
	BaseDomain   string `json:"baseDomain,omitempty" yaml:"baseDomain,omitempty"` // empty = use .localhost
	TLSEmail     string `json:"tlsEmail,omitempty" yaml:"tlsEmail,omitempty"`
}

// Docker holds Docker-related settings
type Docker struct {
	Folders                 []AbsolutePath `json:"folders" yaml:"folders"`
	RequireMountsForFolders bool           `json:"requireMountsForFolders" yaml:"requireMountsForFolders"`
	Proxy                   DockerProxy    `json:"proxy" yaml:"proxy,omitempty"`
}

// PersistedJobSettings holds job progress and worker tuning settings.
type PersistedJobSettings struct {
	ProgressMinIntervalMs     int `json:"progressMinIntervalMs" yaml:"progressMinIntervalMs"`
	NotificationMinIntervalMs int `json:"notificationMinIntervalMs" yaml:"notificationMinIntervalMs"`
	ProgressMinBytesMB        int `json:"progressMinBytesMB" yaml:"progressMinBytesMB"`
	HeavyArchiveConcurrency   int `json:"heavyArchiveConcurrency" yaml:"heavyArchiveConcurrency"`
	ArchiveCompressionWorkers int `json:"archiveCompressionWorkers" yaml:"archiveCompressionWorkers"`
	ArchiveExtractWorkers     int `json:"archiveExtractWorkers" yaml:"archiveExtractWorkers"`
}

// Accepted UIPreferences.NavigationMode values.
const (
	NavigationModeSidebar = "sidebar"
	NavigationModeDock    = "dock"
)

// Accepted UIPreferences.DockTileColors values. These pick how the dock
// derives its tile colors; only the palette changes, never the tile geometry.
const (
	// DockTileColorsAccent fans the tiles across a narrow band of hues around
	// the theme accent.
	DockTileColorsAccent = "accent"
	// DockTileColorsMono paints every tile the theme accent.
	DockTileColorsMono = "mono"
	// DockTileColorsNeutral paints the tiles a neutral surface tone and gives
	// the accent to the active route alone.
	DockTileColorsNeutral = "neutral"
	// DockTileColorsVibrant keeps the fixed per-route palette, which owes
	// nothing to the theme.
	DockTileColorsVibrant = "vibrant"
)

// IsValidDockTileColors reports whether s is a valid stored dock palette.
func IsValidDockTileColors(s string) bool {
	switch s {
	case DockTileColorsAccent, DockTileColorsMono, DockTileColorsNeutral, DockTileColorsVibrant:
		return true
	default:
		return false
	}
}

// ValidateDockAccentGradient returns validation errors for a dock gradient.
func ValidateDockAccentGradient(value DockAccentGradient) []string {
	var errs []string
	if value.StartColor != "" && !IsValidCSSColor(string(value.StartColor)) {
		errs = append(errs, "dockAccentGradient.startColor must be a valid CSS color or empty")
	}
	if value.EndColor != "" && !IsValidCSSColor(string(value.EndColor)) {
		errs = append(errs, "dockAccentGradient.endColor must be a valid CSS color or empty")
	}
	if value.RangeStart < 0 || value.RangeStart > 100 {
		errs = append(errs, "dockAccentGradient.rangeStart must be between 0 and 100")
	}
	if value.RangeEnd < 0 || value.RangeEnd > 100 {
		errs = append(errs, "dockAccentGradient.rangeEnd must be between 0 and 100")
	}
	if value.RangeStart > value.RangeEnd {
		errs = append(errs, "dockAccentGradient.rangeStart must not exceed rangeEnd")
	}
	return errs
}

// PersistedTheme represents a validated theme value (LIGHT or DARK).
type PersistedTheme string

const (
	ThemeLight PersistedTheme = "LIGHT"
	ThemeDark  PersistedTheme = "DARK"
)

// UnmarshalYAML validates theme on unmarshal
func (t *PersistedTheme) UnmarshalYAML(data []byte) error {
	var s string
	if err := yaml.Unmarshal(data, &s); err != nil {
		return err
	}
	s = strings.ToUpper(strings.TrimSpace(s))
	if s != string(ThemeLight) && s != string(ThemeDark) {
		return fmt.Errorf("invalid theme %q: must be LIGHT or DARK", s)
	}
	*t = PersistedTheme(s)
	return nil
}

// String returns the theme as a string
func (t PersistedTheme) String() string {
	return string(t)
}

// CSSColor represents a validated CSS color value
type CSSColor string

// UnmarshalYAML validates CSS color on unmarshal
func (c *CSSColor) UnmarshalYAML(data []byte) error {
	var s string
	if err := yaml.Unmarshal(data, &s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		*c = ""
		return nil
	}
	if !IsValidCSSColor(s) {
		return fmt.Errorf("invalid CSS color %q", s)
	}
	*c = CSSColor(s)
	return nil
}

// String returns the color as a string
func (c CSSColor) String() string {
	return string(c)
}

// AbsolutePath represents a validated absolute filesystem path
type AbsolutePath string

// UnmarshalYAML validates path is absolute on unmarshal
func (p *AbsolutePath) UnmarshalYAML(data []byte) error {
	var s string
	if err := yaml.Unmarshal(data, &s); err != nil {
		return err
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("path cannot be empty")
	}
	if !filepath.IsAbs(s) {
		return fmt.Errorf("path %q must be absolute", s)
	}
	*p = AbsolutePath(filepath.Clean(s))
	return nil
}

// String returns the path as a string
func (p AbsolutePath) String() string {
	return string(p)
}
