package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

// parseCoreConfig decodes one complete core document and validates the result.
// Defaults only supply omitted core fields. A failed decode is returned to the
// caller unchanged so a malformed or unknown-field document cannot be erased
// by a read or an unrelated mutation.
func parseCoreConfig(raw []byte, path, base string) (*Settings, error) {
	cfg, err := decodeCoreConfig(raw, base)
	if err != nil {
		logYAMLError(err, path)
		return nil, err
	}
	return cfg, nil
}

func decodeCoreConfig(raw []byte, base string) (*Settings, error) {
	if err := validateSingleYAMLDocument(raw); err != nil {
		return nil, err
	}

	cfg := DefaultSettings(base)
	if err := yaml.UnmarshalWithOptions(raw, cfg, yaml.Strict()); err != nil {
		return nil, err
	}
	if errs := ValidateConfig(cfg); len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, "; "))
	}
	return cfg, nil
}

// parseUIConfig decodes one UI document over the backend defaults. This keeps
// old sparse UI documents readable while making the runtime value complete.
func parseUIConfig(raw []byte, path string) (*UIPreferences, error) {
	if err := validateSingleYAMLDocument(raw); err != nil {
		logYAMLError(err, path)
		return nil, err
	}

	ui := DefaultUIPreferences()
	if err := yaml.UnmarshalWithOptions(raw, &ui, yaml.Strict()); err != nil {
		logYAMLError(err, path)
		return nil, err
	}
	ui.ViewModes = normalizeViewModesForDefault(ui.ViewModes)
	if errs := ValidateUIPreferences(&ui); len(errs) > 0 {
		err := errors.New(strings.Join(errs, "; "))
		slog.Error("UI config validation failed", "component", "config", "path", path, "error", err)
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

// ValidateUIPreferences validates effective UI preferences accepted from disk
// and by UI replacements.
func ValidateUIPreferences(cfg *UIPreferences) []string {
	if cfg == nil {
		return []string{"UI preferences are nil"}
	}
	var errs []string
	if cfg.Theme != ThemeLight && cfg.Theme != ThemeDark {
		errs = append(errs, "theme must be LIGHT or DARK")
	}
	if !IsValidCSSColor(string(cfg.PrimaryColor)) {
		errs = append(errs, "primaryColor must be a valid CSS color")
	}
	if cfg.NavigationMode != NavigationModeSidebar && cfg.NavigationMode != NavigationModeDock {
		errs = append(errs, "navigationMode must be sidebar or dock")
	}
	if cfg.DockTileColors == "" || !IsValidDockTileColors(cfg.DockTileColors) {
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
	if cfg.TerminalFontSize < 10 || cfg.TerminalFontSize > 28 {
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
		for key, color := range themeColorFields(colors) {
			if color != nil && *color != "" && !IsValidCSSColor(string(*color)) {
				errs = append(errs, fmt.Sprintf("themeColors.%s.%s must be a valid CSS color", modeName, key))
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
		if mode != "card" && mode != "table" {
			errs = append(errs, fmt.Sprintf("viewModes.%s must be card or table", key))
		}
	}
	return errs
}

// ValidateConfig validates the functional settings accepted by disk loads and
// core updates. Docker folders are configuration values, not filesystem
// observations: they must be structurally valid but need not exist yet.
func ValidateConfig(cfg *Settings) []string {
	if cfg == nil {
		return []string{"config is nil"}
	}
	var errs []string
	if len(cfg.Docker.Folders) == 0 {
		errs = append(errs, "docker.folders cannot be empty")
	}
	seenFolders := make(map[string]struct{}, len(cfg.Docker.Folders))
	for _, folderValue := range cfg.Docker.Folders {
		folder := strings.TrimSpace(string(folderValue))
		if folder == "" || !filepath.IsAbs(filepath.Clean(folder)) {
			errs = append(errs, "docker.folders must contain absolute paths")
			continue
		}
		folder = filepath.Clean(folder)
		if folder == string(os.PathSeparator) {
			errs = append(errs, "docker.folders cannot include root")
		}
		if _, exists := seenFolders[folder]; exists {
			errs = append(errs, "docker.folders cannot include duplicates")
			continue
		}
		seenFolders[folder] = struct{}{}
	}
	if cfg.AppSettings.ChunkSizeMB < 0 || cfg.AppSettings.ChunkSizeMB > 32 {
		errs = append(errs, "appSettings.chunkSizeMB must be 0 (default) or between 1 and 32")
	}
	if cfg.Jobs.ProgressMinIntervalMs < 0 {
		errs = append(errs, "jobs.progressMinIntervalMs must be >= 0")
	}
	if cfg.Jobs.NotificationMinIntervalMs < 0 {
		errs = append(errs, "jobs.notificationMinIntervalMs must be >= 0")
	}
	if cfg.Jobs.ProgressMinBytesMB < 0 {
		errs = append(errs, "jobs.progressMinBytesMB must be >= 0")
	}
	if cfg.Jobs.HeavyArchiveConcurrency < 0 {
		errs = append(errs, "jobs.heavyArchiveConcurrency must be >= 0")
	}
	if cfg.Jobs.ArchiveCompressionWorkers < 0 {
		errs = append(errs, "jobs.archiveCompressionWorkers must be >= 0")
	}
	if cfg.Jobs.ArchiveExtractWorkers < 0 {
		errs = append(errs, "jobs.archiveExtractWorkers must be >= 0")
	}
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

func themeColorFields(colors *ThemeColors) map[string]*CSSColor {
	return map[string]*CSSColor{
		"backgroundDefault": colors.BackgroundDefault, "backgroundPaper": colors.BackgroundPaper,
		"headerBackground": colors.HeaderBackground, "footerBackground": colors.FooterBackground,
		"sidebarBackground": colors.SidebarBackground, "cardBackground": colors.CardBackground,
		"dialogBorder": colors.DialogBorder, "dialogGlow": colors.DialogGlow,
		"dialogBackdrop": colors.DialogBackdrop, "codeBackground": colors.CodeBackground,
		"codeText": colors.CodeText, "chartRx": colors.ChartRx, "chartTx": colors.ChartTx,
		"chartNeutral": colors.ChartNeutral, "fileBrowserSurface": colors.FileBrowserSurface,
		"fileBrowserChrome":               colors.FileBrowserChrome,
		"fileBrowserBreadcrumbBackground": colors.FileBrowserBreadcrumbBackground,
		"fileBrowserBreadcrumbText":       colors.FileBrowserBreadcrumbText,
	}
}

func logYAMLError(err error, path string) {
	if syntaxErr, ok := errors.AsType[*yaml.SyntaxError](err); ok {
		if tok := syntaxErr.GetToken(); tok != nil {
			slog.Error("config syntax error", "component", "config", "path", path, "line", tok.Position.Line, "column", tok.Position.Column, "detail", syntaxErr.GetMessage())
			return
		}
		slog.Error("config syntax error", "component", "config", "path", path, "detail", syntaxErr.GetMessage())
		return
	}
	slog.Error("config parse error", "component", "config", "path", path, "error", err)
}
