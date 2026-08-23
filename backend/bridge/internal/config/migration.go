package config

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
)

// legacySettings is the pre-split on-disk shape. It is intentionally kept
// private and temporary: it exists only to recognize one upgrade boundary and
// must not become another runtime configuration model.
type legacySettings struct {
	AppSettings legacyAppSettings    `yaml:"appSettings"`
	Docker      *Docker              `yaml:"docker"`
	Jobs        PersistedJobSettings `yaml:"jobs"`
	Dismissals  *PersistedDismissals `yaml:"dismissals,omitempty"`
}

type legacyAppSettings struct {
	Theme                   PersistedTheme           `yaml:"theme"`
	PrimaryColor            CSSColor                 `yaml:"primaryColor"`
	ThemeColors             *ThemeColorsByMode       `yaml:"themeColors,omitempty"`
	SidebarCollapsed        bool                     `yaml:"sidebarCollapsed"`
	NavigationMode          string                   `yaml:"navigationMode,omitempty"`
	DockTileColors          string                   `yaml:"dockTileColors,omitempty"`
	DockAccentGradient      DockAccentGradient       `yaml:"dockAccentGradient"`
	ShowHiddenFiles         bool                     `yaml:"showHiddenFiles"`
	HiddenCards             []string                 `yaml:"hiddenCards,omitempty"`
	DockerDashboardSections *DockerDashboardSections `yaml:"dockerDashboardSections,omitempty"`
	HardwareSections        *HardwareSections        `yaml:"hardwareSections,omitempty"`
	ViewModes               map[string]string        `yaml:"viewModes,omitempty"`
	LayoutOrders            map[string][]string      `yaml:"layoutOrders,omitempty"`
	DashboardOrder          []string                 `yaml:"dashboardOrder,omitempty"`
	ContainerOrder          []string                 `yaml:"containerOrder,omitempty"`
	ChunkSizeMB             int                      `yaml:"chunkSizeMB,omitempty"`
	TerminalFontSize        int                      `yaml:"terminalFontSize,omitempty"`
}

// migrateLegacyConfigLocked recognizes only a valid old combined document.
// It never repairs invalid core data. A valid existing UI file is preserved so
// an interrupted conversion can finish without rolling back newer UI state;
// an invalid UI file follows the normal cheap reset policy.
func migrateLegacyConfigLocked(cfgPath, uiPath, base string, owner fileOwnership, uiExists bool) (bool, error) {
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		return false, err
	}
	legacyCore, migratedUI, err := parseLegacySettings(raw, base)
	if err != nil {
		// A failed legacy parse is intentionally indistinguishable from an
		// invalid current document to the caller: neither shape is rewritten.
		return false, nil
	}
	if uiExists {
		uiRaw, err := os.ReadFile(uiPath)
		if err != nil {
			return false, fmt.Errorf("read existing UI config during legacy conversion: %w", err)
		}
		if _, err := parseUIConfig(uiRaw, uiPath); err != nil {
			if err := writeEmptyUIConfigOwned(uiPath, owner); err != nil {
				return false, fmt.Errorf("reset invalid UI config during legacy conversion: %w", err)
			}
		}
	} else {
		if err := writeUIConfigOwned(uiPath, *migratedUI, owner); err != nil {
			return false, fmt.Errorf("write migrated UI config: %w", err)
		}
	}
	if err := writeCoreConfigOwned(cfgPath, *legacyCore, owner); err != nil {
		return false, fmt.Errorf("write migrated core config: %w", err)
	}
	return true, nil
}

func parseLegacySettings(raw []byte, base string) (*Settings, *UIPreferences, error) {
	if err := validateSingleYAMLDocument(raw); err != nil {
		return nil, nil, err
	}
	defaults := DefaultSettings(base)
	uiDefaults := DefaultUIPreferences()
	legacy := legacySettings{
		AppSettings: legacyAppSettings{
			Theme:                   uiDefaults.Theme,
			PrimaryColor:            uiDefaults.PrimaryColor,
			ThemeColors:             uiDefaults.ThemeColors,
			NavigationMode:          uiDefaults.NavigationMode,
			DockTileColors:          uiDefaults.DockTileColors,
			DockAccentGradient:      *uiDefaults.DockAccentGradient,
			HiddenCards:             uiDefaults.HiddenCards,
			DockerDashboardSections: uiDefaults.DockerDashboardSections,
			HardwareSections:        uiDefaults.HardwareSections,
			ViewModes:               uiDefaults.ViewModes,
			LayoutOrders:            uiDefaults.LayoutOrders,
			TerminalFontSize:        uiDefaults.TerminalFontSize,
			ShowHiddenFiles:         defaults.AppSettings.ShowHiddenFiles,
			ChunkSizeMB:             defaults.AppSettings.ChunkSizeMB,
		},
		Jobs: defaults.Jobs,
	}
	if err := yaml.UnmarshalWithOptions(raw, &legacy, yaml.Strict()); err != nil {
		return nil, nil, err
	}
	if legacy.Docker == nil {
		return nil, nil, errors.New("legacy docker settings are required")
	}

	core := &Settings{
		AppSettings: PersistedAppSettings{
			ShowHiddenFiles: legacy.AppSettings.ShowHiddenFiles,
			ChunkSizeMB:     legacy.AppSettings.ChunkSizeMB,
		},
		Docker:     *legacy.Docker,
		Jobs:       legacy.Jobs,
		Dismissals: legacy.Dismissals,
	}
	ui := &UIPreferences{
		Theme:                   legacy.AppSettings.Theme,
		PrimaryColor:            legacy.AppSettings.PrimaryColor,
		ThemeColors:             legacy.AppSettings.ThemeColors,
		SidebarCollapsed:        legacy.AppSettings.SidebarCollapsed,
		NavigationMode:          legacy.AppSettings.NavigationMode,
		DockTileColors:          legacy.AppSettings.DockTileColors,
		DockAccentGradient:      &legacy.AppSettings.DockAccentGradient,
		HiddenCards:             legacy.AppSettings.HiddenCards,
		DockerDashboardSections: legacy.AppSettings.DockerDashboardSections,
		HardwareSections:        legacy.AppSettings.HardwareSections,
		ViewModes:               normalizeViewModesForDefault(legacy.AppSettings.ViewModes),
		LayoutOrders:            cloneLegacyLayoutOrders(legacy.AppSettings.LayoutOrders),
		TerminalFontSize:        legacy.AppSettings.TerminalFontSize,
	}
	if ui.NavigationMode == "" {
		ui.NavigationMode = uiDefaults.NavigationMode
	}
	if ui.DockTileColors == "" {
		ui.DockTileColors = uiDefaults.DockTileColors
	}
	if ui.TerminalFontSize == 0 {
		ui.TerminalFontSize = uiDefaults.TerminalFontSize
	}
	if ui.DockAccentGradient == nil {
		ui.DockAccentGradient = uiDefaults.DockAccentGradient
	}
	if ui.HiddenCards == nil {
		ui.HiddenCards = []string{}
	}
	if ui.DockerDashboardSections == nil {
		ui.DockerDashboardSections = cloneDockerDashboardSections(uiDefaults.DockerDashboardSections)
	}
	if ui.HardwareSections == nil {
		ui.HardwareSections = cloneHardwareSections(uiDefaults.HardwareSections)
	}
	if ui.ViewModes == nil {
		ui.ViewModes = map[string]string{}
	}
	if ui.LayoutOrders == nil {
		ui.LayoutOrders = map[string][]string{}
	}
	if len(legacy.AppSettings.DashboardOrder) > 0 {
		if _, exists := ui.LayoutOrders["dashboard"]; !exists {
			ui.LayoutOrders["dashboard"] = append([]string(nil), legacy.AppSettings.DashboardOrder...)
		}
	}
	if len(legacy.AppSettings.ContainerOrder) > 0 {
		if _, exists := ui.LayoutOrders["docker.containers"]; !exists {
			ui.LayoutOrders["docker.containers"] = append([]string(nil), legacy.AppSettings.ContainerOrder...)
		}
	}

	if errs := ValidateConfig(core); len(errs) > 0 {
		return nil, nil, errors.New("legacy core validation failed: " + joinValidationErrors(errs))
	}
	if errs := ValidateUIPreferences(ui); len(errs) > 0 {
		return nil, nil, errors.New("legacy UI validation failed: " + joinValidationErrors(errs))
	}
	return core, ui, nil
}

func cloneLegacyLayoutOrders(orders map[string][]string) map[string][]string {
	result := make(map[string][]string, len(orders))
	for surface, order := range orders {
		result[surface] = append([]string(nil), order...)
	}
	return result
}

func cloneDockerDashboardSections(value *DockerDashboardSections) *DockerDashboardSections {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func cloneHardwareSections(value *HardwareSections) *HardwareSections {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func joinValidationErrors(values []string) string {
	return strings.Join(values, "; ")
}
