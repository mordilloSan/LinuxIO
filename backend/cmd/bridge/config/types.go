package config

// Settings holds the persisted configuration.
type Settings struct {
	ThemeSettings ThemeSettings `yaml:"themeSettings"`
	Docker        Docker        `yaml:"docker"`
}

type ThemeSettings struct {
	Theme            string `yaml:"theme"`        // "LIGHT" | "DARK"
	PrimaryColor     string `yaml:"primaryColor"` // "#RRGGBB" or valid CSS color
	SidebarCollapsed bool   `yaml:"sidebarCollapsed"`
}

type Docker struct {
	Folder string `yaml:"folder"` // absolute path
}
