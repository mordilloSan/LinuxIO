package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/generic"
	"gopkg.in/yaml.v3"
)

var loadedModules = make(map[string]*ModuleInfo)

// LoadModules discovers and loads all modules from system and user directories
func LoadModules(handlerRegistry map[string]map[string]func([]string) (any, error)) error {
	registry := GetRegistry()

	// Load from system directory
	systemDir := "/etc/linuxio/modules"
	systemModules, err := loadModulesFromDir(systemDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to load system modules: %w", err)
	}

	// Load from user directory
	userHome := os.Getenv("HOME")
	if userHome == "" {
		userHome = "/root"
	}
	userDir := filepath.Join(userHome, ".config/linuxio/modules")
	userModules, err := loadModulesFromDir(userDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to load user modules: %w", err)
	}

	// Merge modules (user overrides system)
	allModules := make(map[string]*ModuleInfo)
	for name, mod := range systemModules {
		allModules[name] = mod
	}
	for name, mod := range userModules {
		allModules[name] = mod // User modules override system modules
	}

	// Register each module's handlers
	for name, module := range allModules {
		if err := registerModule(module, handlerRegistry, registry); err != nil {
			fmt.Printf("Warning: failed to register module %s: %v\n", name, err)
			continue
		}
		loadedModules[name] = module
		fmt.Printf("Loaded module: %s v%s\n", module.Manifest.Title, module.Manifest.Version)
	}

	return nil
}

// loadModulesFromDir reads all modules from a directory
func loadModulesFromDir(dir string) (map[string]*ModuleInfo, error) {
	modules := make(map[string]*ModuleInfo)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		modulePath := filepath.Join(dir, entry.Name())
		manifestPath := filepath.Join(modulePath, "module.yaml")

		// Check if module.yaml exists
		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			continue
		}

		// Parse manifest
		manifest, err := parseManifest(manifestPath)
		if err != nil {
			fmt.Printf("Warning: failed to parse %s: %v\n", manifestPath, err)
			continue
		}

		modules[manifest.Name] = &ModuleInfo{
			Manifest: *manifest,
			Path:     modulePath,
			Enabled:  true,
		}
	}

	return modules, nil
}

// parseManifest reads and parses a module.yaml file
func parseManifest(path string) (*ModuleManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	var manifest ModuleManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	// Validate required fields
	if manifest.Name == "" {
		return nil, fmt.Errorf("module name is required")
	}
	if manifest.Version == "" {
		return nil, fmt.Errorf("module version is required")
	}
	if manifest.Title == "" {
		return nil, fmt.Errorf("module title is required")
	}

	return &manifest, nil
}

// registerModule registers all handlers from a module
func registerModule(
	module *ModuleInfo,
	handlerRegistry map[string]map[string]func([]string) (any, error),
	cmdRegistry *CommandRegistry,
) error {
	namespace := "module." + module.Manifest.Name
	handlerRegistry[namespace] = make(map[string]func([]string) (any, error))

	// Register shell command handlers
	for name, cmd := range module.Manifest.Handlers.Commands {
		// Add to whitelist
		timeout := cmd.Timeout
		if timeout == 0 {
			timeout = 10 // Default timeout
		}
		cmdRegistry.RegisterCommand(module.Manifest.Name, name, cmd.Command, timeout, cmd.Args)

		// Create handler function
		handlerRegistry[namespace][name] = createCommandHandler(module.Manifest.Name, name, cmdRegistry)
	}

	// Register DBus handlers
	for name, dbus := range module.Manifest.Handlers.Dbus {
		// Add to whitelist
		cmdRegistry.RegisterDbus(module.Manifest.Name, name, dbus)

		// Create handler function
		handlerRegistry[namespace][name] = createDbusHandler(module.Manifest.Name, name, cmdRegistry)
	}

	return nil
}

// createCommandHandler creates a handler function for a shell command
// This handler will check the registry before executing
func createCommandHandler(moduleName, commandName string, registry *CommandRegistry) func([]string) (any, error) {
	return func(args []string) (any, error) {
		// Check if command is whitelisted
		cmd := registry.GetCommand(moduleName, commandName)
		if cmd == nil {
			return nil, fmt.Errorf("command %s:%s not found in registry", moduleName, commandName)
		}

		// Template substitution
		command := cmd.Template

		// Support both numeric {{.arg0}} and named {{.argName}} placeholders
		for i, argValue := range args {
			// Numeric placeholders: {{.arg0}}, {{.arg1}}, etc.
			placeholderN := fmt.Sprintf("{{.arg%d}}", i)
			command = strings.ReplaceAll(command, placeholderN, argValue)

			// Named placeholders: {{.path}}, {{.host}}, etc.
			if i < len(cmd.Args) {
				argName := cmd.Args[i].Name
				placeholderNamed := fmt.Sprintf("{{.%s}}", argName)
				command = strings.ReplaceAll(command, placeholderNamed, argValue)
			}
		}

		// Execute using generic command handler
		timeout := fmt.Sprintf("%d", cmd.Timeout)
		return generic.ExecCommandDirect(command, timeout)
	}
}

// createDbusHandler creates a handler function for a DBus call
// This handler will check the registry before executing
func createDbusHandler(moduleName, commandName string, registry *CommandRegistry) func([]string) (any, error) {
	return func(args []string) (any, error) {
		// Check if DBus call is whitelisted
		dbus := registry.GetDbus(moduleName, commandName)
		if dbus == nil {
			return nil, fmt.Errorf("dbus call %s:%s not found in registry", moduleName, commandName)
		}

		// Build DBus call arguments
		dbusArgs := []string{
			dbus.Bus,
			dbus.Destination,
			dbus.Path,
			dbus.Interface,
			dbus.Method,
		}

		// Add predefined args from manifest
		dbusArgs = append(dbusArgs, dbus.Args...)

		// Add runtime args
		dbusArgs = append(dbusArgs, args...)

		// Execute using generic DBus handler
		return generic.CallDbusMethodDirect(dbusArgs)
	}
}

// GetLoadedModules returns all loaded modules
func GetLoadedModules() map[string]*ModuleInfo {
	return loadedModules
}

// GetModule returns a specific loaded module
func GetModule(name string) (*ModuleInfo, bool) {
	module, ok := loadedModules[name]
	return module, ok
}
