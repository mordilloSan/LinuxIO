package config

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

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

// Settings holds the persisted configuration.
type Settings struct {
	AppSettings AppSettings `json:"appSettings" yaml:"appSettings"`
	Docker      Docker      `json:"docker" yaml:"docker"`
}

// AppSettings holds UI-related settings
type AppSettings struct {
	Theme            Theme    `json:"theme" yaml:"theme"`
	PrimaryColor     CSSColor `json:"primaryColor" yaml:"primaryColor"`
	SidebarCollapsed bool     `json:"sidebarCollapsed" yaml:"sidebarCollapsed"`
	ShowHiddenFiles  bool     `json:"showHiddenFiles" yaml:"showHiddenFiles"`
}

// Docker holds Docker-related settings
type Docker struct {
	Folder AbsolutePath `json:"folder" yaml:"folder"`
}
