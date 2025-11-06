package settings

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/go-playground/validator/v10"
	"github.com/goccy/go-yaml"
	"github.com/gtsteffaniak/go-logger/logger"

	"github.com/mordilloSan/filebrowser/backend/adapters/fs/fileutils"
	"github.com/mordilloSan/filebrowser/backend/auth/users"
)

var Config Settings

func Initialize(configFile string) {
	err := loadConfigWithDefaults(configFile, false)
	if err != nil {
		logger.Errorf("unable to load config, exiting...")
		logger.Fatal(err.Error())
	}
	err = ValidateConfig(Config)
	if err != nil {
		logger.Errorf("The provided config file failed validation. ")
		logger.Fatal(err.Error())
	}
	setupFs()
	setupLogging()
	setupFrontend(false)
}

func setupFs() {
	// Convert permission values (like 644, 755) to octal interpretation
	filePermOctal, err := strconv.ParseUint(Config.Server.Filesystem.CreateFilePermission, 8, 32)
	if err != nil {
		Config.Server.Filesystem.CreateFilePermission = "644"
		filePermOctal, _ = strconv.ParseUint("644", 8, 32)
	}
	dirPermOctal, err := strconv.ParseUint(Config.Server.Filesystem.CreateDirectoryPermission, 8, 32)
	if err != nil {
		Config.Server.Filesystem.CreateDirectoryPermission = "755"
		dirPermOctal, _ = strconv.ParseUint("755", 8, 32)
	}
	fileutils.SetFsPermissions(os.FileMode(filePermOctal), os.FileMode(dirPermOctal))
}

func setupFrontend(generate bool) {
	if Config.Frontend.Description == "" {
		Config.Frontend.Description = "FileBrowser Quantum is a file manager for the web which can be used to manage files on your server"
	}
	Config.Frontend.Styling.LightBackground = FallbackColor(Config.Frontend.Styling.LightBackground, "#f5f5f5")
	Config.Frontend.Styling.DarkBackground = FallbackColor(Config.Frontend.Styling.DarkBackground, "#141D24")
	setCustomCSSContent(readCustomCSS(Config.Frontend.Styling.CustomCSS))
}

func setupLogging() {
	if len(Config.Server.Logging) == 0 {
		Config.Server.Logging = []LogConfig{
			{
				Output: "stdout",
			},
		}
	}
	for _, logConfig := range Config.Server.Logging {
		// Enable debug logging automatically in dev mode
		levels := logConfig.Levels
		if os.Getenv("FILEBROWSER_DEVMODE") == "true" {
			levels = "info|warning|error|debug"
		}

		logConfig := logger.JsonConfig{
			Levels:     levels,
			ApiLevels:  logConfig.ApiLevels,
			Output:     logConfig.Output,
			Utc:        logConfig.Utc,
			NoColors:   logConfig.NoColors,
			Json:       logConfig.Json,
			Structured: false,
		}
		err := logger.EnableCompatibilityMode(logConfig)
		if err != nil {
			log.Println("[ERROR] Failed to set up logger:", err)
		}
	}
}

func loadConfigWithDefaults(configFile string, generate bool) error {
	Config = setDefaults(generate)

	// Check if config file exists
	if _, err := os.Stat(configFile); err != nil {
		if configFile != "" {
			logger.Errorf("could not open config file '%v', using default settings.", configFile)
		}
		loadEnvConfig()
		return nil
	}

	// Try multi-file config first (combine all YAML files in the directory)
	combinedYAML, err := combineYAMLFiles(configFile)
	if err != nil {
		return fmt.Errorf("failed to combine YAML files: %v", err)
	}

	// First pass: Unmarshal into a generic map to resolve all anchors and aliases
	// This allows YAML anchors defined in auxiliary files to be properly merged
	var rawConfig map[string]interface{}
	err = yaml.Unmarshal(combinedYAML, &rawConfig)
	if err != nil {
		return fmt.Errorf("error parsing YAML data: %v", err)
	}

	// Filter to only keep valid top-level Settings struct fields
	// This removes anchor definitions that are just templates (e.g., "test_server: &test_server")
	validFields := map[string]bool{
		"server":       true,
		"auth":         true,
		"frontend":     true,
		"userDefaults": true,
	}

	filteredConfig := make(map[string]interface{})
	for key, value := range rawConfig {
		if validFields[key] {
			filteredConfig[key] = value
		}
	}

	// Marshal the filtered config back to YAML
	filteredYAML, err := yaml.Marshal(filteredConfig)
	if err != nil {
		return fmt.Errorf("error re-marshaling filtered YAML: %v", err)
	}

	// Second pass: Decode with strict validation (disallow unknown fields within valid sections)
	decoder := yaml.NewDecoder(bytes.NewReader(filteredYAML), yaml.DisallowUnknownField())
	err = decoder.Decode(&Config)
	if err != nil {
		return fmt.Errorf("error unmarshaling YAML data: %v", err)
	}

	loadEnvConfig()
	return nil
}

func ValidateConfig(config Settings) error {
	validate := validator.New()

	// Register custom validator for file permissions
	err := validate.RegisterValidation("file_permission", validateFilePermission)
	if err != nil {
		return fmt.Errorf("could not register file_permission validator: %v", err)
	}

	err = validate.Struct(Config)
	if err != nil {
		return fmt.Errorf("could not validate config: %v", err)
	}
	return nil
}

// validateFilePermission validates that a string is a valid Unix octal file permission (3-4 digits, 0-7)
func validateFilePermission(fl validator.FieldLevel) bool {
	value := fl.Field().String()

	// Must be 3 or 4 characters long
	if len(value) < 3 || len(value) > 4 {
		return false
	}

	// All characters must be octal digits (0-7)
	for _, char := range value {
		if char < '0' || char > '7' {
			return false
		}
	}

	return true
}

func loadEnvConfig() {
	jwtTokenSecret := os.Getenv("FILEBROWSER_JWT_TOKEN_SECRET")
	if jwtTokenSecret != "" {
		Config.Auth.Key = jwtTokenSecret
		logger.Infof("Using JWT Token Secret from FILEBROWSER_JWT_TOKEN_SECRET environment variable")
	}

}

func setDefaults(generate bool) Settings {

	s := Settings{
		Server: Server{
			Port:             80,
			MaxArchiveSizeGB: 50,
			CacheDir:         "tmp",
			Filesystem: Filesystem{
				CreateFilePermission:      "644",
				CreateDirectoryPermission: "755",
			},
		},
		Auth: Auth{
			TokenExpirationHours: 2,
		},
		Frontend: Frontend{
			Name: "FileBrowser Quantum",
		},

		UserDefaults: UserDefaults{
			ShowHidden:  true,
			DarkMode:    true,
			ViewMode:    "normal",
			Locale:      "en",
			GallerySize: 3,
			ThemeColor:  "var(--blue)",
			FileLoading: users.FileLoading{
				MaxConcurrent: 10,
				ChunkSize:     10, // 10MB
			},
		},
	}
	return s
}
