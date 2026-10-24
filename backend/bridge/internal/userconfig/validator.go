package userconfig

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/goccy/go-yaml"
)

// parseCoreConfig decodes one complete core document and validates the result.
// Defaults only supply omitted core fields. A failed decode is returned to the
// caller unchanged; parseCoreConfig never rewrites. Only the startup read
// (loadCoreOrQuarantineOwned) quarantines and replaces an invalid document;
// core mutations leave it untouched.
func parseCoreConfig(raw []byte, base string) (*Settings, error) {
	if err := validateSingleYAMLDocument(raw); err != nil {
		return nil, err
	}

	cfg := defaultSettings(base)
	if err := yaml.UnmarshalWithOptions(raw, cfg, yaml.Strict()); err != nil {
		return nil, err
	}
	if errs := validateSettings(cfg); len(errs) > 0 {
		err := errors.New(strings.Join(errs, "; "))
		return nil, err
	}
	return cfg, nil
}

// parseUIConfig decodes one UI document over the backend defaults. This keeps
// old sparse UI documents readable while making the runtime value complete.
func parseUIConfig(raw []byte) (*UIPreferences, error) {
	if err := validateSingleYAMLDocument(raw); err != nil {
		return nil, err
	}

	ui := DefaultUIPreferences()
	if err := yaml.UnmarshalWithOptions(raw, &ui, yaml.Strict()); err != nil {
		return nil, err
	}
	ui.ViewModes = normalizeViewModesForDefault(ui.ViewModes)
	if errs := validateUIPreferences(&ui); len(errs) > 0 {
		err := errors.New(strings.Join(errs, "; "))
		return nil, err
	}
	return &ui, nil
}

// normalizeViewModesForDefault keeps only explicit deviations from the
// backend policy. This lets a future backend default change affect surfaces
// that were previously left at the policy value, including snapshots written
// by the first split-config release before inheritance was restored.
func normalizeViewModesForDefault(viewModes map[string]string) map[string]string {
	if viewModes == nil {
		return nil
	}
	result := make(map[string]string, len(viewModes))
	for key, mode := range viewModes {
		if mode == DefaultViewMode {
			continue
		}
		result[key] = mode
	}
	return result
}
func validateSingleYAMLDocument(raw []byte) error {
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	var document any
	if err := decoder.Decode(&document); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("YAML document is empty")
		}
		return err
	}
	if document == nil {
		return errors.New("YAML document is empty")
	}

	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err != nil {
			return err
		}
		return errors.New("multiple YAML documents are not supported")
	}
	return nil
}

// validateUIPreferences validates effective UI preferences accepted from disk
// and by UI replacements.
func validateUIPreferences(cfg *UIPreferences) []string {
	if cfg == nil {
		return []string{"UI preferences are nil"}
	}
	var errs []string
	if cfg.Theme != ThemeLight && cfg.Theme != ThemeDark {
		errs = append(errs, "theme must be LIGHT or DARK")
	}
	if !isValidCSSColor(string(cfg.PrimaryColor)) {
		errs = append(errs, "primaryColor must be a valid CSS color")
	}
	if cfg.NavigationMode != NavigationModeSidebar && cfg.NavigationMode != NavigationModeDock {
		errs = append(errs, "navigationMode must be sidebar or dock")
	}
	if cfg.DockTileColors == "" || !isValidDockTileColors(cfg.DockTileColors) {
		errs = append(errs, "dockTileColors must be accent, mono, neutral or vibrant")
	}
	if cfg.DockAccentGradient == nil {
		errs = append(errs, "dockAccentGradient is required")
	} else {
		errs = append(errs, ValidateDockAccentGradient(*cfg.DockAccentGradient)...)
	}
	if cfg.DockerDashboardSections == nil {
		errs = append(errs, "dockerDashboardSections is required")
	}
	if cfg.HardwareSections == nil {
		errs = append(errs, "hardwareSections is required")
	}
	if cfg.HiddenCards == nil {
		errs = append(errs, "hiddenCards is required")
	}
	if cfg.ViewModes == nil {
		errs = append(errs, "viewModes is required")
	}
	if cfg.LayoutOrders == nil {
		errs = append(errs, "layoutOrders is required")
	}
	if !ValidTerminalFontSize(cfg.TerminalFontSize) {
		errs = append(errs, "terminalFontSize must be between 10 and 28")
	}
	errs = append(errs, validateThemeColors(cfg)...)
	errs = append(errs, validateViewModes(cfg.ViewModes)...)
	return errs
}

func validateThemeColors(cfg *UIPreferences) []string {
	var errs []string
	for modeName, colors := range map[string]*ThemeColors{
		"light": cfg.themeColorsValue(ThemeLight),
		"dark":  cfg.themeColorsValue(ThemeDark),
	} {
		if colors == nil {
			continue
		}
		for _, slot := range themeColorSlots(colors) {
			if color := *slot.Value; color != nil && *color != "" && !isValidCSSColor(string(*color)) {
				errs = append(errs, fmt.Sprintf("themeColors.%s.%s must be a valid CSS color", modeName, slot.Key))
			}
		}
	}
	return errs
}

func validateViewModes(viewModes map[string]string) []string {
	var errs []string
	for key, mode := range viewModes {
		if strings.TrimSpace(key) == "" {
			errs = append(errs, "viewModes keys cannot be empty")
		}
		if mode != ViewModeCard && mode != ViewModeTable {
			errs = append(errs, fmt.Sprintf("viewModes.%s must be card or table", key))
		}
	}
	return errs
}

// validateSettings validates the functional settings accepted by disk loads and
// core updates. Docker folders are configuration values, not filesystem
// observations: they must be structurally valid but need not exist yet.
func validateSettings(cfg *Settings) []string {
	if cfg == nil {
		return []string{"config is nil"}
	}
	var errs []string
	if _, err := ParseDockerFolders(cfg.Docker.Folders); err != nil {
		errs = append(errs, err.Error())
	}
	if !ValidChunkSizeMB(cfg.AppSettings.ChunkSizeMB) {
		errs = append(errs, "appSettings.chunkSizeMB must be 0 (default) or between 1 and 32")
	}
	errs = append(errs, ValidateJobSettings(cfg.Jobs)...)
	return errs
}

func (p *UIPreferences) themeColorsValue(mode PersistedTheme) *ThemeColors {
	if p == nil || p.ThemeColors == nil {
		return nil
	}
	if mode == ThemeLight {
		return p.ThemeColors.Light
	}
	return p.ThemeColors.Dark
}
