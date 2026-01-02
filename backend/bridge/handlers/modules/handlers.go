package modules

import "fmt"

// ModuleHandlers returns the handler map for module-related API calls
func ModuleHandlers() map[string]func([]string) (any, error) {
	return map[string]func([]string) (any, error){
		"GetModules": func([]string) (any, error) {
			return GetLoadedModulesForFrontend()
		},
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
			ComponentURL: fmt.Sprintf("/modules/%s/component.js", module.Manifest.Name),
		})
	}

	return result, nil
}
