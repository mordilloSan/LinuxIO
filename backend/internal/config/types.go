package config

// Settings holds the persisted configuration.
type Settings struct {
	ThemeSettings ThemeSettings `json:"themeSettings" yaml:"themeSettings"`
	Docker        Docker        `json:"docker" yaml:"docker"`
}

type ThemeSettings struct {
	Theme            string `json:"theme" yaml:"theme"`               // "LIGHT" | "DARK"
	PrimaryColor     string `json:"primaryColor" yaml:"primaryColor"` // "#RRGGBB" or valid CSS color
	SidebarCollapsed bool   `json:"sidebarCollapsed" yaml:"sidebarCollapsed"`
}

type Docker struct {
	Folder string `json:"folder" yaml:"folder"` // absolute path
}
