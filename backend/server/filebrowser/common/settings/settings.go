//go:generate go run ./tools/yaml.go -input=common/settings/settings.go -output=config.generated.yaml
package settings

import (
	"crypto/rand"

	"github.com/mordilloSan/filebrowser/backend/auth/users"
)

const DefaultUsersHomeBasePath = "/users"

// AuthMethod describes an authentication method.
type AuthMethod string

// GenerateKey generates a key of 512 bits.
func GenerateKey() ([]byte, error) {
	b := make([]byte, 64)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return nil, err
	}

	return b, nil
}

func GetSettingsConfig(nameType string, Value string) string {
	return nameType + Value
}

// Apply applies the default options to a user.
func ApplyUserDefaults(u *users.User) {
	u.DarkMode = Config.UserDefaults.DarkMode
	u.Locale = Config.UserDefaults.Locale
	u.ViewMode = Config.UserDefaults.ViewMode
	u.Preview = Config.UserDefaults.Preview
	u.ShowHidden = Config.UserDefaults.ShowHidden
	u.ThemeColor = Config.UserDefaults.ThemeColor
	u.GallerySize = Config.UserDefaults.GallerySize
	u.FileLoading = Config.UserDefaults.FileLoading
	u.Version = users.CurrentUserConfigVersion
}
