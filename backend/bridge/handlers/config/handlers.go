package config

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// RegisterHandlers registers config handlers with the new handler system
func RegisterHandlers(sess *session.Session) {
	username := sess.User.Username

	// Unified config endpoints
	ipc.RegisterFunc("config", "get", func(ctx context.Context, args []string, emit ipc.Events) error {
		cfg, cfgPath, err := Load(username)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		logger.Debugf("[config.get] user=%q path=%s", username, cfgPath)
		return emit.Result(cfg)
	})

	ipc.RegisterFunc("config", "set", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}

		// Payload with optional nested fields
		var payload struct {
			AppSettings *struct {
				Theme        *string `json:"theme"`
				PrimaryColor *string `json:"primaryColor"`
				ThemeColors  *struct {
					BackgroundDefault *string `json:"backgroundDefault"`
					BackgroundPaper   *string `json:"backgroundPaper"`
					HeaderBackground  *string `json:"headerBackground"`
					FooterBackground  *string `json:"footerBackground"`
					SidebarBackground *string `json:"sidebarBackground"`
					CardBackground    *string `json:"cardBackground"`
				} `json:"themeColors"`
				SidebarCollapsed        *bool                    `json:"sidebarCollapsed"`
				ShowHiddenFiles         *bool                    `json:"showHiddenFiles"`
				DashboardOrder          []string                 `json:"dashboardOrder"`
				HiddenCards             []string                 `json:"hiddenCards"`
				ContainerOrder          []string                 `json:"containerOrder"`
				DockerDashboardSections *DockerDashboardSections `json:"dockerDashboardSections"`
				ViewModes               map[string]string        `json:"viewModes"`
				ChunkSizeMB             *int                     `json:"chunkSizeMB"`
			} `json:"appSettings"`
			Docker *struct {
				Folder           *string  `json:"folder"`
				AutoUpdateStacks []string `json:"autoUpdateStacks"`
			} `json:"docker"`
		}

		if err := json.Unmarshal([]byte(args[0]), &payload); err != nil {
			return ipc.ErrInvalidArgs
		}

		cfg, _, err := Load(username)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		// Update AppSettings if provided
		if payload.AppSettings != nil {
			if payload.AppSettings.Theme != nil {
				t := strings.ToUpper(strings.TrimSpace(*payload.AppSettings.Theme))
				if t != string(ThemeLight) && t != string(ThemeDark) {
					return fmt.Errorf("invalid theme value (LIGHT|DARK)")
				}
				cfg.AppSettings.Theme = Theme(t)
			}
			if payload.AppSettings.PrimaryColor != nil {
				if !IsValidCSSColor(*payload.AppSettings.PrimaryColor) {
					return fmt.Errorf("invalid primaryColor")
				}
				cfg.AppSettings.PrimaryColor = CSSColor(*payload.AppSettings.PrimaryColor)
			}
			if tc := payload.AppSettings.ThemeColors; tc != nil {
				colors := &ThemeColors{}
				fields := []struct {
					src *string
					dst **CSSColor
					key string
				}{
					{tc.BackgroundDefault, &colors.BackgroundDefault, "backgroundDefault"},
					{tc.BackgroundPaper, &colors.BackgroundPaper, "backgroundPaper"},
					{tc.HeaderBackground, &colors.HeaderBackground, "headerBackground"},
					{tc.FooterBackground, &colors.FooterBackground, "footerBackground"},
					{tc.SidebarBackground, &colors.SidebarBackground, "sidebarBackground"},
					{tc.CardBackground, &colors.CardBackground, "cardBackground"},
				}
				for _, f := range fields {
					if f.src == nil {
						continue
					}
					if !IsValidCSSColor(*f.src) {
						return fmt.Errorf("invalid themeColors.%s", f.key)
					}
					v := CSSColor(*f.src)
					*f.dst = &v
				}
				cfg.AppSettings.ThemeColors = colors
			}
			if payload.AppSettings.SidebarCollapsed != nil {
				cfg.AppSettings.SidebarCollapsed = *payload.AppSettings.SidebarCollapsed
			}
			if payload.AppSettings.ShowHiddenFiles != nil {
				cfg.AppSettings.ShowHiddenFiles = *payload.AppSettings.ShowHiddenFiles
			}
			if payload.AppSettings.DashboardOrder != nil {
				cfg.AppSettings.DashboardOrder = payload.AppSettings.DashboardOrder
			}
			if payload.AppSettings.HiddenCards != nil {
				cfg.AppSettings.HiddenCards = payload.AppSettings.HiddenCards
			}
			if payload.AppSettings.ContainerOrder != nil {
				cfg.AppSettings.ContainerOrder = payload.AppSettings.ContainerOrder
			}
			if payload.AppSettings.DockerDashboardSections != nil {
				cfg.AppSettings.DockerDashboardSections = payload.AppSettings.DockerDashboardSections
			}
			if payload.AppSettings.ViewModes != nil {
				normalized := make(map[string]string, len(payload.AppSettings.ViewModes))
				for key, mode := range payload.AppSettings.ViewModes {
					k := strings.TrimSpace(key)
					m := strings.ToLower(strings.TrimSpace(mode))
					if k == "" {
						continue
					}
					if m != "card" && m != "table" {
						continue
					}
					normalized[k] = m
				}
				cfg.AppSettings.ViewModes = normalized
			}
			if payload.AppSettings.ChunkSizeMB != nil {
				v := *payload.AppSettings.ChunkSizeMB
				if v != 0 && (v < 1 || v > 32) {
					return fmt.Errorf("chunkSizeMB must be 0 (default) or between 1 and 32")
				}
				cfg.AppSettings.ChunkSizeMB = v
			}
		}

		// Update Docker settings if provided
		if payload.Docker != nil {
			if payload.Docker.Folder != nil {
				folderInput := strings.TrimSpace(*payload.Docker.Folder)
				if folderInput == "" {
					return fmt.Errorf("docker folder cannot be empty")
				}
				folder := filepath.Clean(folderInput)
				if !filepath.IsAbs(folder) {
					return fmt.Errorf("docker folder must be an absolute path")
				}
				if folder == string(filepath.Separator) {
					return fmt.Errorf("docker folder cannot be root")
				}
				cfg.Docker.Folder = AbsolutePath(folder)
			}
			if payload.Docker.AutoUpdateStacks != nil {
				cfg.Docker.AutoUpdateStacks = payload.Docker.AutoUpdateStacks
			}
		}

		cfgPath, err := Save(username, cfg)
		if err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		logger.Debugf("[config.set] user=%q updated config: path=%s", username, cfgPath)

		return emit.Result(map[string]any{
			"message": "config updated",
			"path":    cfgPath,
		})
	})

}
