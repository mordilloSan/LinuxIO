package settings

import (
	"github.com/mordilloSan/filebrowser/backend/auth/users"
)

type Settings struct {
	Server       Server       `json:"server"`
	Auth         Auth         `json:"auth"`
	Frontend     Frontend     `json:"frontend"`
	UserDefaults UserDefaults `json:"userDefaults"`
}

type Server struct {
	TLSKey           string      `json:"tlsKey"`               // path to TLS key
	TLSCert          string      `json:"tlsCert"`              // path to TLS cert
	DisablePreviews  bool        `json:"disablePreviews"`      // disable all previews thumbnails, simple icons will be used
	DisableResize    bool        `json:"disablePreviewResize"` // disable resizing of previews for faster loading over slow connections
	Port             int         `json:"port"`                 // port to listen on
	Logging          []LogConfig `json:"logging" yaml:"logging"`
	CacheDir         string      `json:"cacheDir"`       // path to the cache directory, used for thumbnails and other cached files
	MaxArchiveSizeGB int64       `json:"maxArchiveSize"` // max pre-archive combined size of files/folder that are allowed to be archived (in GB)
	Filesystem       Filesystem  `json:"filesystem"`     // filesystem settings
	MuPdfAvailable   bool        `json:"-"`              // used internally if compiled with mupdf support
	EmbeddedFs       bool        `json:"-"`              // used internally if compiled with embedded fs support
}

type Filesystem struct {
	CreateFilePermission      string `json:"createFilePermission" validate:"required,file_permission"`      // Unix permissions like 644, 755, 2755 (default: 644)
	CreateDirectoryPermission string `json:"createDirectoryPermission" validate:"required,file_permission"` // Unix permissions like 755, 2755, 1777 (default: 755)
}

type LogConfig struct {
	Levels    string `json:"levels" yaml:"levels"`       // separated list of log levels to enable. (eg. "info|warning|error|debug")
	ApiLevels string `json:"apiLevels" yaml:"apiLevels"` // separated list of log levels to enable for the API. (eg. "info|warning|error")
	Output    string `json:"output" yaml:"output"`       // output location. (eg. "stdout" or "path/to/file.log")
	NoColors  bool   `json:"noColors" yaml:"noColors"`   // disable colors in the output
	Json      bool   `json:"json" yaml:"json"`           // output in json format
	Utc       bool   `json:"utc" yaml:"utc"`             // use UTC time in the output instead of local time
}

const RootPath = "/"

type Frontend struct {
	Name        string        `json:"name"` // display name
	Styling     StylingConfig `json:"styling"`
	Description string        `json:"description"` // description that shows up in html head meta description
}

type StylingConfig struct {
	CustomCSS       string `json:"customCSS"`       // if a valid path to a css file is provided, it will be applied for all users
	LightBackground string `json:"lightBackground"` // specify a valid CSS color property value to use as the background color in light mode
	DarkBackground  string `json:"darkBackground"`  // Specify a valid CSS color property value to use as the background color in dark mode
}

// UserDefaults is a type that holds the default values
type UserDefaults struct {
	DarkMode    bool              `json:"darkMode"`    // should dark mode be enabled
	Locale      string            `json:"locale"`      // language to use: eg. de, en, or fr
	ViewMode    string            `json:"viewMode"`    // view mode to use: eg. normal, list, grid, or compact
	ShowHidden  bool              `json:"showHidden"`  // show hidden files in the UI.
	GallerySize int               `json:"gallerySize"` // 0-9 - the size of the gallery thumbnails
	ThemeColor  string            `json:"themeColor"`  // theme color to use: eg. #ff0000, or var(--red), var(--purple), etc
	Preview     users.Preview     `json:"preview"`
	FileLoading users.FileLoading `json:"fileLoading"` // upload and download settings
}
