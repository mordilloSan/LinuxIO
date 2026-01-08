package modules

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/generic"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/go_logger/logger"
	"gopkg.in/yaml.v3"
)

var loadedModules = make(map[string]*ModuleInfo)

// LoadModules discovers and loads all modules from system and user directories
func LoadModules(
	jsonHandlers map[string]map[string]func([]string) (any, error),
	streamHandlers map[string]func(*session.Session, net.Conn, []string) error,
) error {
	// Load from system directory
	systemDir := "/etc/linuxio/modules"
	logger.Debugf("Loading from system directory: %s", systemDir)
	systemModules, err := loadModulesFromDir(systemDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to load system modules: %w", err)
	}
	logger.Infof("Found %d modules in system directory", len(systemModules))
	for name := range systemModules {
		logger.Debugf("  - %s (system)", name)
	}

	// Load from user directory
	userHome := os.Getenv("HOME")
	if userHome == "" {
		userHome = "/root"
	}
	userDir := filepath.Join(userHome, ".config/linuxio/modules")
	logger.Debugf("Loading from user directory: %s", userDir)
	userModules, err := loadModulesFromDir(userDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to load user modules: %w", err)
	}
	logger.Infof("Found %d modules in user directory", len(userModules))
	for name := range userModules {
		logger.Debugf("  - %s (user)", name)
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
		if err := registerModule(module, jsonHandlers, streamHandlers); err != nil {
			logger.Warnf("Failed to register module %s: %v", name, err)
			continue
		}
		loadedModules[name] = module
		logger.Infof("Loaded module: %s v%s", module.Manifest.Title, module.Manifest.Version)
	}

	return nil
}

// loadModulesFromDir reads all modules from a directory
func loadModulesFromDir(dir string) (map[string]*ModuleInfo, error) {
	modules := make(map[string]*ModuleInfo)

	entries, err := os.ReadDir(dir)
	if err != nil {
		logger.Debugf("Error reading directory %s: %v", dir, err)
		return nil, err
	}

	logger.Debugf("Reading directory %s, found %d entries", dir, len(entries))

	for _, entry := range entries {
		modulePath := filepath.Join(dir, entry.Name())

		// Use os.Stat to follow symlinks (entry.IsDir() returns false for symlinks)
		info, err := os.Stat(modulePath)
		if err != nil {
			logger.Debugf("  Skipping %s: stat failed: %v", entry.Name(), err)
			continue
		}

		isSymlink := entry.Type()&os.ModeSymlink != 0
		logger.Debugf("  Entry: %s (isDir=%v, isSymlink=%v)", entry.Name(), info.IsDir(), isSymlink)
		if !info.IsDir() {
			continue
		}

		manifestPath := filepath.Join(modulePath, "module.yaml")

		// Check if module.yaml exists
		manifestInfo, statErr := os.Stat(manifestPath)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				logger.Debugf("  Skipping %s: module.yaml not found", entry.Name())
				continue
			}
			logger.Debugf("  Skipping %s: module.yaml stat failed: %v", entry.Name(), statErr)
			continue
		}
		if manifestInfo.IsDir() {
			logger.Debugf("  Skipping %s: module.yaml is a directory", entry.Name())
			continue
		}

		// Parse manifest
		manifest, err := parseManifest(manifestPath)
		if err != nil {
			logger.Warnf("Failed to parse %s: %v", manifestPath, err)
			continue
		}

		logger.Debugf("  ✓ Parsed module: %s (name=%s)", entry.Name(), manifest.Name)
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
	jsonHandlers map[string]map[string]func([]string) (any, error),
	streamHandlers map[string]func(*session.Session, net.Conn, []string) error,
) error {
	namespace := "module." + module.Manifest.Name
	jsonHandlers[namespace] = make(map[string]func([]string) (any, error))

	// Register shell command handlers
	for name, cmd := range module.Manifest.Handlers.Commands {
		// Create handler function
		jsonHandlers[namespace][name] = createCommandHandler(module.Manifest.Name, name, cmd)
	}

	// Register DBus handlers (immediate response -> JsonHandlers)
	for name, dbus := range module.Manifest.Handlers.Dbus {
		// Create handler function
		jsonHandlers[namespace][name] = createDbusHandler(module.Manifest.Name, name, dbus)
	}

	// Register DBus stream handlers (signal streaming -> StreamHandlers)
	for name, dbusStream := range module.Manifest.Handlers.DbusStreams {
		// Create stream handler function
		// Stream type: "module.{moduleName}.{handlerName}"
		streamType := namespace + "." + name
		streamHandlers[streamType] = createDbusStreamHandler(module.Manifest.Name, name, dbusStream)

		logger.Debugf("Registered D-Bus stream handler: %s", streamType)
	}

	return nil
}

// createCommandHandler creates a handler function for a shell command
func createCommandHandler(moduleName, commandName string, cmdDef CommandHandler) func([]string) (any, error) {
	return func(args []string) (any, error) {
		// Template substitution
		command := cmdDef.Command

		// Support both numeric {{.arg0}} and named {{.argName}} placeholders
		for i, argValue := range args {
			// Numeric placeholders: {{.arg0}}, {{.arg1}}, etc.
			placeholderN := fmt.Sprintf("{{.arg%d}}", i)
			command = strings.ReplaceAll(command, placeholderN, argValue)

			// Named placeholders: {{.path}}, {{.host}}, etc.
			if i < len(cmdDef.Args) {
				argName := cmdDef.Args[i].Name
				placeholderNamed := fmt.Sprintf("{{.%s}}", argName)
				command = strings.ReplaceAll(command, placeholderNamed, argValue)
			}
		}

		// Execute using generic command handler
		timeout := fmt.Sprintf("%d", cmdDef.Timeout)
		if cmdDef.Timeout == 0 {
			timeout = "10" // Default timeout
		}
		return generic.ExecCommandDirect(command, timeout)
	}
}

// createDbusHandler creates a handler function for a DBus call
func createDbusHandler(moduleName, commandName string, dbusDef DbusHandler) func([]string) (any, error) {
	return func(args []string) (any, error) {
		// Build DBus call arguments
		dbusArgs := []string{
			dbusDef.Bus,
			dbusDef.Destination,
			dbusDef.Path,
			dbusDef.Interface,
			dbusDef.Method,
		}

		// Add predefined args from manifest
		dbusArgs = append(dbusArgs, dbusDef.Args...)

		// Add runtime args
		dbusArgs = append(dbusArgs, args...)

		// Execute using generic DBus handler
		return generic.CallDbusMethodDirect(dbusArgs)
	}
}

// createDbusStreamHandler creates a stream handler for a DBus operation with signals
func createDbusStreamHandler(moduleName, commandName string, dbusStreamDef DbusStreamHandler) func(*session.Session, net.Conn, []string) error {
	return func(sess *session.Session, conn net.Conn, args []string) error {
		// Build DBus stream arguments
		// Format: [bus, destination, path, interface, method, signal1, signal2, ..., "--", methodArg1, methodArg2, ...]
		streamArgs := []string{
			dbusStreamDef.Bus,
			dbusStreamDef.Destination,
			dbusStreamDef.Path,
			dbusStreamDef.Interface,
			dbusStreamDef.Method,
		}

		// Add signal names
		streamArgs = append(streamArgs, dbusStreamDef.Signals...)

		// Add separator
		streamArgs = append(streamArgs, "--")

		// Add predefined args from manifest
		streamArgs = append(streamArgs, dbusStreamDef.Args...)

		// Add runtime args
		streamArgs = append(streamArgs, args...)

		// Execute using generic DBus stream handler
		return generic.HandleDbusStream(conn, streamArgs)
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

// GetLoadedModulesForFrontend returns all loaded modules in frontend-friendly format
func GetLoadedModulesForFrontend() ([]ModuleFrontendInfo, error) {
	var modules []ModuleFrontendInfo
	for _, module := range loadedModules {
		info := ModuleFrontendInfo{
			Name:         module.Manifest.Name,
			Title:        module.Manifest.Title,
			Description:  module.Manifest.Description,
			Version:      module.Manifest.Version,
			Route:        module.Manifest.UI.Route,
			Icon:         module.Manifest.UI.Icon,
			Position:     module.Manifest.UI.Sidebar.Position,
			ComponentURL: fmt.Sprintf("/modules/%s/index.js", module.Manifest.Name),
		}
		modules = append(modules, info)
	}
	return modules, nil
}

// GetModuleDetailsInfo returns detailed info for a specific module
func GetModuleDetailsInfo(name string) (*ModuleDetailsInfo, error) {
	module, exists := GetModule(name)
	if !exists {
		return nil, fmt.Errorf("module '%s' not found", name)
	}

	// Collect handler names
	var handlers []string
	for handlerName := range module.Manifest.Handlers.Commands {
		handlers = append(handlers, handlerName)
	}
	for handlerName := range module.Manifest.Handlers.Dbus {
		handlers = append(handlers, handlerName)
	}
	for handlerName := range module.Manifest.Handlers.DbusStreams {
		handlers = append(handlers, handlerName)
	}

	// Check if system module
	isSystem := IsSystemModule(module.Path)

	// Check if symlink
	isSymlink, _ := IsSymlinkModule(module.Path)

	details := &ModuleDetailsInfo{
		ModuleFrontendInfo: ModuleFrontendInfo{
			Name:         module.Manifest.Name,
			Title:        module.Manifest.Title,
			Description:  module.Manifest.Description,
			Version:      module.Manifest.Version,
			Route:        module.Manifest.UI.Route,
			Icon:         module.Manifest.UI.Icon,
			Position:     module.Manifest.UI.Sidebar.Position,
			ComponentURL: fmt.Sprintf("/modules/%s/index.js", module.Manifest.Name),
		},
		Author:      module.Manifest.Author,
		Homepage:    module.Manifest.Homepage,
		License:     module.Manifest.License,
		Path:        module.Path,
		IsSystem:    isSystem,
		IsSymlink:   isSymlink,
		Handlers:    handlers,
		Permissions: module.Manifest.Permissions,
		Settings:    module.Manifest.Settings,
	}

	return details, nil
}
