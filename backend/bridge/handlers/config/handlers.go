package config

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/settings"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type configHandlers struct {
	username string
	store    *settings.UserStore
}

type configSetPayload struct {
	AppSettings *configAppSettingsPayload `json:"appSettings"`
	Docker      *configDockerPayload      `json:"docker"`
	Jobs        *configJobSettingsPayload `json:"jobs"`
	Dismissals  *configDismissalsPayload  `json:"dismissals"`
}

type configAppSettingsPayload struct {
	Theme                   *string                           `json:"theme"`
	PrimaryColor            *string                           `json:"primaryColor"`
	ThemeColors             *configThemeColorsByModePayload   `json:"themeColors"`
	SidebarCollapsed        *bool                             `json:"sidebarCollapsed"`
	ShowHiddenFiles         *bool                             `json:"showHiddenFiles"`
	DashboardOrder          []string                          `json:"dashboardOrder"`
	HiddenCards             []string                          `json:"hiddenCards"`
	ContainerOrder          []string                          `json:"containerOrder"`
	DockerDashboardSections *settings.DockerDashboardSections `json:"dockerDashboardSections"`
	HardwareSections        *settings.HardwareSections        `json:"hardwareSections"`
	ViewModes               map[string]string                 `json:"viewModes"`
	ChunkSizeMB             *int                              `json:"chunkSizeMB"`
}

type configThemeColorsByModePayload struct {
	Light *configThemeColorsPayload `json:"light"`
	Dark  *configThemeColorsPayload `json:"dark"`
}

type configThemeColorsPayload struct {
	BackgroundDefault               *string `json:"backgroundDefault"`
	BackgroundPaper                 *string `json:"backgroundPaper"`
	HeaderBackground                *string `json:"headerBackground"`
	FooterBackground                *string `json:"footerBackground"`
	SidebarBackground               *string `json:"sidebarBackground"`
	CardBackground                  *string `json:"cardBackground"`
	DialogBorder                    *string `json:"dialogBorder"`
	DialogGlow                      *string `json:"dialogGlow"`
	DialogBackdrop                  *string `json:"dialogBackdrop"`
	CodeBackground                  *string `json:"codeBackground"`
	CodeText                        *string `json:"codeText"`
	ChartRx                         *string `json:"chartRx"`
	ChartTx                         *string `json:"chartTx"`
	ChartNeutral                    *string `json:"chartNeutral"`
	FileBrowserSurface              *string `json:"fileBrowserSurface"`
	FileBrowserChrome               *string `json:"fileBrowserChrome"`
	FileBrowserBreadcrumbBackground *string `json:"fileBrowserBreadcrumbBackground"`
	FileBrowserBreadcrumbText       *string `json:"fileBrowserBreadcrumbText"`
}

type configDockerPayload struct {
	Folders          []string                  `json:"folders"`
	AutoUpdateStacks []string                  `json:"autoUpdateStacks"`
	Proxy            *configDockerProxyPayload `json:"proxy"`
}

type configDockerProxyPayload struct {
	CaddyEnabled *bool   `json:"caddyEnabled"`
	BaseDomain   *string `json:"baseDomain"`
	TLSEmail     *string `json:"tlsEmail"`
}

type configJobSettingsPayload struct {
	ProgressMinIntervalMs     *int `json:"progressMinIntervalMs"`
	NotificationMinIntervalMs *int `json:"notificationMinIntervalMs"`
	ProgressMinBytesMB        *int `json:"progressMinBytesMB"`
	HeavyArchiveConcurrency   *int `json:"heavyArchiveConcurrency"`
	ArchiveCompressionWorkers *int `json:"archiveCompressionWorkers"`
	ArchiveExtractWorkers     *int `json:"archiveExtractWorkers"`
}

type configDismissalsPayload struct {
	UncleanShutdownBootID *string `json:"uncleanShutdownBootId"`
	FailedLoginAlertID    *string `json:"failedLoginAlertId"`
}

// RegisterHandlers registers config handlers with the new handler system
func RegisterHandlers(rt runtime.Runtime) {
	handlers := configHandlers{
		username: rt.Username(),
		store:    rt.Store,
	}
	rpc.Register("config", rt, []rpc.Command{
		{Name: "get", Handler: handlers.handleGetConfig},
		{Name: "set", Handler: handlers.handleSetConfig},
	})
}

func (h configHandlers) handleGetConfig(ctx context.Context, args []string, emit ipc.Events) error {
	cfg, cfgPath, err := settings.SnapshotForUser(h.username, h.store)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.Jobs = settings.EffectiveJobSettings(cfg.Jobs)
	slog.Debug("loaded user config", "component", "config", "user", h.username, "path", cfgPath)
	return rpc.EmitResult(emit, cfg, nil)
}

func (h configHandlers) handleSetConfig(ctx context.Context, args []string, emit ipc.Events) error {
	payload, err := rpc.DecodeJSONArg[configSetPayload](args, 0)
	if err != nil {
		return err
	}
	slog.Info("config update requested", "component", "config", "user", h.username)

	_, cfgPath, err := settings.UpdateForUser(h.username, h.store, func(cfg *settings.Settings) error {
		return applyConfigPayload(cfg, &payload)
	})
	if err != nil {
		return fmt.Errorf("update config: %w", err)
	}
	slog.Info("user config updated", "component", "config", "user", h.username, "path", cfgPath)
	return rpc.EmitResult(emit, map[string]any{
		"message": "config updated",
		"path":    cfgPath,
	}, nil)
}

func applyConfigPayload(cfg *settings.Settings, payload *configSetPayload) error {
	if payload.AppSettings != nil {
		if err := applyAppSettingsUpdate(&cfg.AppSettings, payload.AppSettings); err != nil {
			return err
		}
	}
	if payload.Docker != nil {
		if err := applyDockerSettingsUpdate(&cfg.Docker, payload.Docker); err != nil {
			return err
		}
	}
	if payload.Jobs != nil {
		if err := applyJobSettingsUpdate(&cfg.Jobs, payload.Jobs); err != nil {
			return err
		}
	}
	if payload.Dismissals != nil {
		applyDismissalsUpdate(&cfg.Dismissals, payload.Dismissals)
	}
	return nil
}

func applyAppSettingsUpdate(app *settings.AppSettings, payload *configAppSettingsPayload) error {
	if err := applyThemeSetting(app, payload.Theme); err != nil {
		return err
	}
	if err := applyPrimaryColorSetting(app, payload.PrimaryColor); err != nil {
		return err
	}
	if err := applyThemeColorOverrides(app, payload.ThemeColors); err != nil {
		return err
	}
	applyOptionalBool(&app.SidebarCollapsed, payload.SidebarCollapsed)
	applyOptionalBool(&app.ShowHiddenFiles, payload.ShowHiddenFiles)
	applyOptionalStringSlice(&app.DashboardOrder, payload.DashboardOrder)
	applyOptionalStringSlice(&app.HiddenCards, payload.HiddenCards)
	applyOptionalStringSlice(&app.ContainerOrder, payload.ContainerOrder)
	applyOptionalDockerDashboardSections(app, payload.DockerDashboardSections)
	applyOptionalHardwareSections(app, payload.HardwareSections)
	applyViewModes(app, payload.ViewModes)
	return applyChunkSizeSetting(app, payload.ChunkSizeMB)
}

func applyThemeSetting(app *settings.AppSettings, theme *string) error {
	if theme == nil {
		return nil
	}
	normalized := strings.ToUpper(strings.TrimSpace(*theme))
	if normalized != string(settings.ThemeLight) && normalized != string(settings.ThemeDark) {
		return fmt.Errorf("invalid theme value (LIGHT|DARK)")
	}
	app.Theme = settings.Theme(normalized)
	return nil
}

func applyPrimaryColorSetting(app *settings.AppSettings, primaryColor *string) error {
	if primaryColor == nil {
		return nil
	}
	if !settings.IsValidCSSColor(*primaryColor) {
		return fmt.Errorf("invalid primaryColor")
	}
	app.PrimaryColor = settings.CSSColor(*primaryColor)
	return nil
}

func applyThemeColorOverrides(app *settings.AppSettings, payload *configThemeColorsByModePayload) error {
	if payload == nil {
		return nil
	}
	light, err := buildThemeColors(payload.Light, "light")
	if err != nil {
		return err
	}
	dark, err := buildThemeColors(payload.Dark, "dark")
	if err != nil {
		return err
	}
	if light == nil && dark == nil {
		app.ThemeColors = nil
	} else {
		app.ThemeColors = &settings.ThemeColorsByMode{Light: light, Dark: dark}
	}
	return nil
}

func buildThemeColors(payload *configThemeColorsPayload, modePrefix string) (*settings.ThemeColors, error) {
	if payload == nil {
		return nil, nil
	}
	colors := &settings.ThemeColors{}
	hasAny := false
	fields := []struct {
		src *string
		dst **settings.CSSColor
		key string
	}{
		{src: payload.BackgroundDefault, dst: &colors.BackgroundDefault, key: "backgroundDefault"},
		{src: payload.BackgroundPaper, dst: &colors.BackgroundPaper, key: "backgroundPaper"},
		{src: payload.HeaderBackground, dst: &colors.HeaderBackground, key: "headerBackground"},
		{src: payload.FooterBackground, dst: &colors.FooterBackground, key: "footerBackground"},
		{src: payload.SidebarBackground, dst: &colors.SidebarBackground, key: "sidebarBackground"},
		{src: payload.CardBackground, dst: &colors.CardBackground, key: "cardBackground"},
		{src: payload.DialogBorder, dst: &colors.DialogBorder, key: "dialogBorder"},
		{src: payload.DialogGlow, dst: &colors.DialogGlow, key: "dialogGlow"},
		{src: payload.DialogBackdrop, dst: &colors.DialogBackdrop, key: "dialogBackdrop"},
		{src: payload.CodeBackground, dst: &colors.CodeBackground, key: "codeBackground"},
		{src: payload.CodeText, dst: &colors.CodeText, key: "codeText"},
		{src: payload.ChartRx, dst: &colors.ChartRx, key: "chartRx"},
		{src: payload.ChartTx, dst: &colors.ChartTx, key: "chartTx"},
		{src: payload.ChartNeutral, dst: &colors.ChartNeutral, key: "chartNeutral"},
		{src: payload.FileBrowserSurface, dst: &colors.FileBrowserSurface, key: "fileBrowserSurface"},
		{src: payload.FileBrowserChrome, dst: &colors.FileBrowserChrome, key: "fileBrowserChrome"},
		{src: payload.FileBrowserBreadcrumbBackground, dst: &colors.FileBrowserBreadcrumbBackground, key: "fileBrowserBreadcrumbBackground"},
		{src: payload.FileBrowserBreadcrumbText, dst: &colors.FileBrowserBreadcrumbText, key: "fileBrowserBreadcrumbText"},
	}
	for _, field := range fields {
		if field.src == nil {
			continue
		}
		if !settings.IsValidCSSColor(*field.src) {
			return nil, fmt.Errorf("invalid themeColors.%s.%s", modePrefix, field.key)
		}
		value := settings.CSSColor(*field.src)
		*field.dst = &value
		hasAny = true
	}
	if !hasAny {
		return nil, nil
	}
	return colors, nil
}

func applyOptionalBool(dst *bool, value *bool) {
	if value != nil {
		*dst = *value
	}
}

func applyOptionalStringSlice(dst *[]string, value []string) {
	if value != nil {
		*dst = value
	}
}

func applyOptionalDockerDashboardSections(app *settings.AppSettings, sections *settings.DockerDashboardSections) {
	if sections != nil {
		app.DockerDashboardSections = sections
	}
}

func applyOptionalHardwareSections(app *settings.AppSettings, sections *settings.HardwareSections) {
	if sections != nil {
		app.HardwareSections = sections
	}
}

func applyViewModes(app *settings.AppSettings, viewModes map[string]string) {
	if viewModes == nil {
		return
	}
	normalized := make(map[string]string, len(viewModes))
	for key, mode := range viewModes {
		normalizedKey := strings.TrimSpace(key)
		normalizedMode := strings.ToLower(strings.TrimSpace(mode))
		if normalizedKey == "" {
			continue
		}
		if normalizedMode != "card" && normalizedMode != "table" {
			continue
		}
		normalized[normalizedKey] = normalizedMode
	}
	app.ViewModes = normalized
}

func applyChunkSizeSetting(app *settings.AppSettings, chunkSize *int) error {
	if chunkSize == nil {
		return nil
	}
	value := *chunkSize
	if value != 0 && (value < 1 || value > 32) {
		return fmt.Errorf("chunkSizeMB must be 0 (default) or between 1 and 32")
	}
	app.ChunkSizeMB = value
	return nil
}

func applyDockerSettingsUpdate(docker *settings.Docker, payload *configDockerPayload) error {
	if err := applyDockerFoldersSetting(docker, payload.Folders); err != nil {
		return err
	}
	if payload.AutoUpdateStacks != nil {
		docker.AutoUpdateStacks = payload.AutoUpdateStacks
	}
	if payload.Proxy != nil {
		applyDockerProxyUpdate(&docker.Proxy, payload.Proxy)
	}
	return nil
}

func applyDockerProxyUpdate(proxy *settings.DockerProxy, payload *configDockerProxyPayload) {
	if payload.CaddyEnabled != nil {
		proxy.CaddyEnabled = *payload.CaddyEnabled
	}
	if payload.BaseDomain != nil {
		proxy.BaseDomain = strings.TrimSpace(*payload.BaseDomain)
	}
	if payload.TLSEmail != nil {
		proxy.TLSEmail = strings.TrimSpace(*payload.TLSEmail)
	}
}

func applyDockerFoldersSetting(docker *settings.Docker, folderValues []string) error {
	if folderValues == nil {
		return nil
	}
	if len(folderValues) == 0 {
		return fmt.Errorf("docker folders cannot be empty")
	}

	folders := make([]settings.AbsolutePath, 0, len(folderValues))
	seen := make(map[string]struct{}, len(folderValues))
	for _, folderValue := range folderValues {
		folderInput := strings.TrimSpace(folderValue)
		if folderInput == "" {
			return fmt.Errorf("docker folders cannot include an empty path")
		}
		folder := filepath.Clean(folderInput)
		if !filepath.IsAbs(folder) {
			return fmt.Errorf("docker folder must be an absolute path")
		}
		if folder == string(filepath.Separator) {
			return fmt.Errorf("docker folder cannot be root")
		}
		if _, exists := seen[folder]; exists {
			return fmt.Errorf("docker folders cannot include duplicates")
		}
		seen[folder] = struct{}{}
		folders = append(folders, settings.AbsolutePath(folder))
	}

	docker.Folders = folders
	return nil
}

func applyJobSettingsUpdate(jobs *settings.JobSettings, payload *configJobSettingsPayload) error {
	if err := applyOptionalNonNegativeInt(&jobs.ProgressMinIntervalMs, payload.ProgressMinIntervalMs, "jobs.progressMinIntervalMs"); err != nil {
		return err
	}
	if err := applyOptionalNonNegativeInt(&jobs.NotificationMinIntervalMs, payload.NotificationMinIntervalMs, "jobs.notificationMinIntervalMs"); err != nil {
		return err
	}
	if err := applyOptionalNonNegativeInt(&jobs.ProgressMinBytesMB, payload.ProgressMinBytesMB, "jobs.progressMinBytesMB"); err != nil {
		return err
	}
	if err := applyOptionalPositiveInt(&jobs.HeavyArchiveConcurrency, payload.HeavyArchiveConcurrency, "jobs.heavyArchiveConcurrency"); err != nil {
		return err
	}
	if err := applyOptionalNonNegativeInt(&jobs.ArchiveCompressionWorkers, payload.ArchiveCompressionWorkers, "jobs.archiveCompressionWorkers"); err != nil {
		return err
	}
	return applyOptionalNonNegativeInt(&jobs.ArchiveExtractWorkers, payload.ArchiveExtractWorkers, "jobs.archiveExtractWorkers")
}

func applyDismissalsUpdate(dismissals **settings.Dismissals, payload *configDismissalsPayload) {
	if *dismissals == nil {
		*dismissals = &settings.Dismissals{}
	}
	if payload.UncleanShutdownBootID != nil {
		(*dismissals).UncleanShutdownBootID = strings.TrimSpace(*payload.UncleanShutdownBootID)
	}
	if payload.FailedLoginAlertID != nil {
		(*dismissals).FailedLoginAlertID = strings.TrimSpace(*payload.FailedLoginAlertID)
	}
	if (*dismissals).UncleanShutdownBootID == "" && (*dismissals).FailedLoginAlertID == "" {
		*dismissals = nil
	}
}

func applyOptionalNonNegativeInt(dst *int, value *int, name string) error {
	if value == nil {
		return nil
	}
	if *value < 0 {
		return fmt.Errorf("%s must be >= 0", name)
	}
	*dst = *value
	return nil
}

func applyOptionalPositiveInt(dst *int, value *int, name string) error {
	if value == nil {
		return nil
	}
	if *value <= 0 {
		return fmt.Errorf("%s must be > 0", name)
	}
	*dst = *value
	return nil
}
