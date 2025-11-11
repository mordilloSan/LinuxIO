package userconfig

// Settings holds the persisted configuration.
type Settings struct {
	AppSettings AppSettings `json:"appSettings" yaml:"appSettings"`
	Docker      Docker      `json:"docker" yaml:"docker"`
}

type AppSettings struct {
	Theme            string `json:"theme" yaml:"theme"`
	PrimaryColor     string `json:"primaryColor" yaml:"primaryColor"`
	SidebarCollapsed bool   `json:"sidebarCollapsed" yaml:"sidebarCollapsed"`
	ShowHiddenFiles  bool   `json:"showHiddenFiles" yaml:"showHiddenFiles"`
}

type Docker struct {
	Folder string `json:"folder" yaml:"folder"` // absolute path
}
