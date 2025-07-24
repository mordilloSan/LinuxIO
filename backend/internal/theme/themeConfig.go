package theme

import (
	"backend/internal/auth"
	"backend/internal/logger"
	"errors"
	"net/http"
	"os"
	"os/user"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

type ThemeSettings struct {
	Theme           string `json:"theme"`
	PrimaryColor    string `json:"primaryColor"`
	SidebarColapsed bool   `json:"sidebarColapsed"`
}

func InitTheme() error {
	path, err := getThemeFilePath()
	if err != nil {
		logger.Errorf("Failed to determine theme config path: %v", err)
		return err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		logger.Infof("No theme file found, creating from embedded default...")
		// Optional: if you want to embed a YAML default, otherwise create minimal struct
		defaultTheme := ThemeSettings{
			Theme:           "LIGHT",
			PrimaryColor:    "#2196f3",
			SidebarColapsed: false,
		}
		return SaveThemeToFile(defaultTheme)
	}
	return nil
}

func LoadTheme() (ThemeSettings, error) {
	var settings ThemeSettings
	path, err := getThemeFilePath()
	if err != nil {
		logger.Errorf("Failed to determine theme config path: %v", err)
		return settings, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		logger.Errorf("Failed to read theme file: %v", err)
		return settings, err
	}
	if err := yaml.Unmarshal(data, &settings); err != nil {
		logger.Errorf("Failed to parse theme YAML: %v", err)
		return settings, err
	}
	return settings, nil
}

func SaveThemeToFile(settings ThemeSettings) error {
	path, err := getThemeFilePath()
	if err != nil {
		logger.Errorf("Failed to determine theme config path: %v", err)
		return err
	}
	data, err := yaml.Marshal(&settings)
	if err != nil {
		logger.Errorf("Failed to encode theme YAML: %v", err)
		return err
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		logger.Errorf("Failed to write theme YAML: %v", err)
		return err
	}
	logger.Infof("Theme settings saved to %s", path)
	return nil
}

// --- Gin Routes ---

func RegisterThemeRoutes(router *gin.Engine) {

	theme := router.Group("/theme", auth.AuthMiddleware())
	theme.GET("/theme/get", func(c *gin.Context) {
		settings, err := LoadTheme()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				if initErr := InitTheme(); initErr != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize theme"})
					return
				}
				settings, err = LoadTheme()
				if err != nil {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load initialized theme"})
					return
				}
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to load theme"})
				return
			}
		}
		c.JSON(http.StatusOK, settings)
	})
	theme.POST("/set", func(c *gin.Context) {
		var body ThemeSettings

		if err := c.BindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		if body.Theme != "LIGHT" && body.Theme != "DARK" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid theme value"})
			return
		}

		if err := SaveThemeToFile(body); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save theme settings"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Theme settings saved"})
	})
}

func getThemeFilePath() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", err
	}
	return filepath.Join(u.HomeDir, ".linuxio-theme.yaml"), nil
}
