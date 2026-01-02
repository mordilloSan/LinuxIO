package modules

// ModuleManifest represents a module's YAML configuration
type ModuleManifest struct {
	Name        string          `yaml:"name"`
	Version     string          `yaml:"version"`
	Title       string          `yaml:"title"`
	Description string          `yaml:"description"`
	Author      string          `yaml:"author"`
	Homepage    string          `yaml:"homepage"`
	License     string          `yaml:"license"`
	UI          UIConfig        `yaml:"ui"`
	Handlers    HandlerConfig   `yaml:"handlers"`
	Permissions []string        `yaml:"permissions"`
	Settings    []SettingConfig `yaml:"settings"`
}

// UIConfig defines UI integration for the module
type UIConfig struct {
	Route   string        `yaml:"route"`
	Icon    string        `yaml:"icon"`
	Sidebar SidebarConfig `yaml:"sidebar"`
}

// SidebarConfig defines sidebar appearance
type SidebarConfig struct {
	Enabled  bool   `yaml:"enabled"`
	Position int    `yaml:"position"`
	Section  string `yaml:"section"`
}

// HandlerConfig defines all handlers for the module
type HandlerConfig struct {
	Commands map[string]CommandHandler `yaml:"commands"`
	Dbus     map[string]DbusHandler    `yaml:"dbus"`
}

// CommandHandler defines a shell command handler
type CommandHandler struct {
	Description string         `yaml:"description"`
	Command     string         `yaml:"command"`
	Timeout     int            `yaml:"timeout"`
	Args        []HandlerArg   `yaml:"args"`
	Returns     HandlerReturns `yaml:"returns"`
}

// DbusHandler defines a DBus method call handler
type DbusHandler struct {
	Description string         `yaml:"description"`
	Bus         string         `yaml:"bus"`         // "system" or "session"
	Destination string         `yaml:"destination"` // e.g. org.freedesktop.login1
	Path        string         `yaml:"path"`        // e.g. /org/freedesktop/login1
	Interface   string         `yaml:"interface"`   // e.g. org.freedesktop.login1.Manager
	Method      string         `yaml:"method"`      // e.g. Reboot
	Args        []string       `yaml:"args"`        // Method arguments
	Returns     HandlerReturns `yaml:"returns"`
}

// HandlerArg defines an argument for a handler
type HandlerArg struct {
	Name        string      `yaml:"name"`
	Type        string      `yaml:"type"`
	Required    bool        `yaml:"required"`
	Default     interface{} `yaml:"default"`
	Description string      `yaml:"description"`
}

// HandlerReturns defines the return type of a handler
type HandlerReturns struct {
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
}

// SettingConfig defines a configurable setting
type SettingConfig struct {
	Name        string      `yaml:"name"`
	Type        string      `yaml:"type"`
	Default     interface{} `yaml:"default"`
	Description string      `yaml:"description"`
	Min         interface{} `yaml:"min"`
	Max         interface{} `yaml:"max"`
}

// ModuleInfo is the runtime representation of a loaded module
type ModuleInfo struct {
	Manifest ModuleManifest
	Path     string // Path to module directory
	Enabled  bool
}

// ModuleFrontendInfo is the subset of module data sent to frontend
type ModuleFrontendInfo struct {
	Name         string `json:"name"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Version      string `json:"version"`
	Route        string `json:"route"`
	Icon         string `json:"icon"`
	Position     int    `json:"position"`
	ComponentURL string `json:"componentUrl"`
}
