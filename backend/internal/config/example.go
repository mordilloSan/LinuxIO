package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

// GenerateExampleYAML returns a YAML document with defaults for the given base.
func GenerateExampleYAML(base string) ([]byte, error) {
	cfg := DefaultSettings(base)
	return yaml.Marshal(cfg)
}

// WriteExampleYAML writes a default YAML to path (0644).
func WriteExampleYAML(path, base string) error {
	b, err := GenerateExampleYAML(base)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// ExampleDefaults returns a stable, user-agnostic config for docs/examples.
// Keep this in sync with your real defaults (used at runtime).
func ExampleDefaults() Settings {
	return Settings{
		ThemeSettings: ThemeSettings{
			Theme:            "DARK",
			PrimaryColor:     "#2196f3",
			SidebarCollapsed: false,
		},
		Docker: Docker{
			// Use a neutral path that makes sense in docs. Avoid per-user paths.
			Folder: "/var/lib/linuxio/docker",
		},
	}
}
