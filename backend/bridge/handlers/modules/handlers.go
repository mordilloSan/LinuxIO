package modules

import (
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/common/middleware"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// ModuleHandlers returns the handler map for module-related API calls
func ModuleHandlers(sess *session.Session, handlerRegistry map[string]map[string]func([]string) (any, error)) map[string]func([]string) (any, error) {
	// Create closures for handlers that need the handlerRegistry
	uninstallModuleHandler := func(args []string) (any, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("module name required")
		}
		return UninstallModuleOperation(args[0], handlerRegistry)
	}

	installModuleHandler := func(args []string) (any, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("source path required")
		}
		targetName := ""
		if len(args) > 1 {
			targetName = args[1]
		}
		createSymlink := len(args) > 2 && args[2] == "true"
		return InstallModuleOperation(args[0], targetName, createSymlink, handlerRegistry)
	}

	getModuleDetailsHandler := func(args []string) (any, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("module name required")
		}
		return GetModuleDetailsInfo(args[0])
	}

	validateModuleHandler := func(args []string) (any, error) {
		if len(args) < 1 {
			return nil, fmt.Errorf("path required")
		}
		return ValidateModuleAtPath(args[0])
	}

	return map[string]func([]string) (any, error){
		// Public handler - no privilege required
		"GetModules": func([]string) (any, error) {
			return GetLoadedModulesForFrontend()
		},

		// Privileged handlers for module management
		"GetModuleDetails": middleware.RequirePrivileged(sess, getModuleDetailsHandler),
		"UninstallModule":  middleware.RequirePrivileged(sess, uninstallModuleHandler),
		"InstallModule":    middleware.RequirePrivileged(sess, installModuleHandler),
		"ValidateModule":   middleware.RequirePrivileged(sess, validateModuleHandler),
	}
}

// GetLoadedModulesForFrontend returns module info formatted for frontend consumption
func GetLoadedModulesForFrontend() ([]ModuleFrontendInfo, error) {
	modules := GetLoadedModules()
	result := make([]ModuleFrontendInfo, 0)

	for _, module := range modules {
		// Only include enabled modules with sidebar enabled
		if !module.Enabled || !module.Manifest.UI.Sidebar.Enabled {
			continue
		}

		result = append(result, ModuleFrontendInfo{
			Name:         module.Manifest.Name,
			Title:        module.Manifest.Title,
			Description:  module.Manifest.Description,
			Version:      module.Manifest.Version,
			Route:        module.Manifest.UI.Route,
			Icon:         module.Manifest.UI.Icon,
			Position:     module.Manifest.UI.Sidebar.Position,
			ComponentURL: fmt.Sprintf("/modules/%s/ui/component.js", module.Manifest.Name),
		})
	}

	return result, nil
}

// GetModuleDetailsInfo returns detailed module information including management metadata
func GetModuleDetailsInfo(moduleName string) (*ModuleDetailsInfo, error) {
	module, exists := GetModule(moduleName)
	if !exists {
		return nil, fmt.Errorf("module '%s' not found", moduleName)
	}

	// Check if module is system module
	isSystem := IsSystemModule(module.Path)

	// Check if module path is a symlink
	isSymlink, err := IsSymlinkModule(module.Path)
	if err != nil {
		isSymlink = false // Default to false if check fails
	}

	// Get list of registered handlers
	registry := GetRegistry()
	var handlers []string

	// Get command handlers
	for _, cmd := range registry.ListCommands(moduleName) {
		handlers = append(handlers, cmd.CommandName)
	}

	// Get DBus handlers
	for _, dbus := range registry.ListDbus(moduleName) {
		handlers = append(handlers, dbus.CommandName)
	}

	return &ModuleDetailsInfo{
		ModuleFrontendInfo: ModuleFrontendInfo{
			Name:         module.Manifest.Name,
			Title:        module.Manifest.Title,
			Description:  module.Manifest.Description,
			Version:      module.Manifest.Version,
			Route:        module.Manifest.UI.Route,
			Icon:         module.Manifest.UI.Icon,
			Position:     module.Manifest.UI.Sidebar.Position,
			ComponentURL: fmt.Sprintf("/modules/%s/ui/component.js", module.Manifest.Name),
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
	}, nil
}
