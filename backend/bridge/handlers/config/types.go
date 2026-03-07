package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// Settings holds the persisted configuration.
type Settings struct {
	AppSettings AppSettings `json:"appSettings" yaml:"appSettings"`
	Docker      Docker      `json:"docker" yaml:"docker"`
}

// DockerDashboardSections holds the collapsed state of each dashboard section
type DockerDashboardSections struct {
	Overview  bool `json:"overview" yaml:"overview"`
	Daemon    bool `json:"daemon" yaml:"daemon"`
	Resources bool `json:"resources" yaml:"resources"`
}

// ThemeColors holds optional per-field color overrides for the UI theme
type ThemeColors struct {
	BackgroundDefault *CSSColor `json:"backgroundDefault,omitempty" yaml:"backgroundDefault,omitempty"`
	BackgroundPaper   *CSSColor `json:"backgroundPaper,omitempty" yaml:"backgroundPaper,omitempty"`
	HeaderBackground  *CSSColor `json:"headerBackground,omitempty" yaml:"headerBackground,omitempty"`
	FooterBackground  *CSSColor `json:"footerBackground,omitempty" yaml:"footerBackground,omitempty"`
	SidebarBackground *CSSColor `json:"sidebarBackground,omitempty" yaml:"sidebarBackground,omitempty"`
	CardBackground    *CSSColor `json:"cardBackground,omitempty" yaml:"cardBackground,omitempty"`
}

// AppSettings holds UI-related settings
type AppSettings struct {
	Theme                   Theme                    `json:"theme" yaml:"theme"`
	PrimaryColor            CSSColor                 `json:"primaryColor" yaml:"primaryColor"`
	ThemeColors             *ThemeColors             `json:"themeColors,omitempty" yaml:"themeColors,omitempty"`
	SidebarCollapsed        bool                     `json:"sidebarCollapsed" yaml:"sidebarCollapsed"`
	ShowHiddenFiles         bool                     `json:"showHiddenFiles" yaml:"showHiddenFiles"`
	DashboardOrder          []string                 `json:"dashboardOrder,omitempty" yaml:"dashboardOrder,omitempty"`
	HiddenCards             []string                 `json:"hiddenCards,omitempty" yaml:"hiddenCards,omitempty"`
	ContainerOrder          []string                 `json:"containerOrder,omitempty" yaml:"containerOrder,omitempty"`
	DockerDashboardSections *DockerDashboardSections `json:"dockerDashboardSections,omitempty" yaml:"dockerDashboardSections,omitempty"`
	ViewModes               map[string]string        `json:"viewModes,omitempty" yaml:"viewModes,omitempty"`
	// ChunkSizeMB is the file-transfer chunk size in MiB (1–32). 0 = use default (1 MiB).
	ChunkSizeMB int `json:"chunkSizeMB,omitempty" yaml:"chunkSizeMB,omitempty"`
}

// DockerProxy holds Caddy reverse proxy configuration
type DockerProxy struct {
	CaddyEnabled bool   `json:"caddyEnabled" yaml:"caddyEnabled"`
	BaseDomain   string `json:"baseDomain,omitempty" yaml:"baseDomain,omitempty"` // empty = use .localhost
	TLSEmail     string `json:"tlsEmail,omitempty" yaml:"tlsEmail,omitempty"`
}

// Docker holds Docker-related settings
type Docker struct {
	Folder           AbsolutePath `json:"folder" yaml:"folder"`
	AutoUpdateStacks []string     `json:"autoUpdateStacks,omitempty" yaml:"autoUpdateStacks,omitempty"`
	Proxy            DockerProxy  `json:"proxy" yaml:"proxy,omitempty"`
}

// Theme represents a validated theme value (LIGHT or DARK)
type Theme string

const (
	ThemeLight Theme = "LIGHT"
	ThemeDark  Theme = "DARK"
)

// UnmarshalYAML validates theme on unmarshal
func (t *Theme) UnmarshalYAML(data []byte) error {
	var s string
	if err := yaml.Unmarshal(data, &s); err != nil {
		return err
	}
	s = strings.ToUpper(strings.TrimSpace(s))
	if s != string(ThemeLight) && s != string(ThemeDark) {
		return fmt.Errorf("invalid theme %q: must be LIGHT or DARK", s)
	}
	*t = Theme(s)
	return nil
}

// String returns the theme as a string
func (t Theme) String() string {
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
