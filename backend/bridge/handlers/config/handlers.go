package config

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type themePayload struct {
	Theme               *string `json:"theme"`
	PrimaryColor        *string `json:"primaryColor"`
	SidebarCollapsed    *bool   `json:"sidebarCollapsed"`
	SidebarCollapsedAlt *bool   `json:"SidebarCollapsed"`
	ShowHiddenFiles     *bool   `json:"showHiddenFiles"`
}

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
				Theme            *string `json:"theme"`
				PrimaryColor     *string `json:"primaryColor"`
				SidebarCollapsed *bool   `json:"sidebarCollapsed"`
				ShowHiddenFiles  *bool   `json:"showHiddenFiles"`
			} `json:"appSettings"`
			Docker *struct {
				Folder *string `json:"folder"`
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
			if payload.AppSettings.SidebarCollapsed != nil {
				cfg.AppSettings.SidebarCollapsed = *payload.AppSettings.SidebarCollapsed
			}
			if payload.AppSettings.ShowHiddenFiles != nil {
				cfg.AppSettings.ShowHiddenFiles = *payload.AppSettings.ShowHiddenFiles
			}
		}

		// Update Docker settings if provided
		if payload.Docker != nil {
			if payload.Docker.Folder != nil {
				folder := strings.TrimSpace(*payload.Docker.Folder)
				if folder == "" {
					return fmt.Errorf("docker folder cannot be empty")
				}
				cfg.Docker.Folder = AbsolutePath(folder)
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

	// Legacy endpoints (kept for backward compatibility)
	ipc.RegisterFunc("config", "theme_get", func(ctx context.Context, args []string, emit ipc.Events) error {
		cfg, cfgPath, err := Load(username)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		logger.Debugf("[theme.get] user=%q path=%s theme=%s primary=%s collapsed=%v showHidden=%v",
			username, cfgPath, cfg.AppSettings.Theme, cfg.AppSettings.PrimaryColor, cfg.AppSettings.SidebarCollapsed, cfg.AppSettings.ShowHiddenFiles)
		return emit.Result(cfg.AppSettings)
	})

	ipc.RegisterFunc("config", "theme_set", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		var payload themePayload
		if err := json.Unmarshal([]byte(args[0]), &payload); err != nil {
			return ipc.ErrInvalidArgs
		}

		cfg, _, err := Load(username)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		prev := cfg.AppSettings
		next := prev

		if payload.Theme != nil {
			t := strings.ToUpper(strings.TrimSpace(*payload.Theme))
			if t != string(ThemeLight) && t != string(ThemeDark) {
				return fmt.Errorf("invalid theme value (LIGHT|DARK)")
			}
			next.Theme = Theme(t)
		}
		if payload.PrimaryColor != nil {
			if !IsValidCSSColor(*payload.PrimaryColor) {
				return fmt.Errorf("invalid primaryColor")
			}
			next.PrimaryColor = CSSColor(*payload.PrimaryColor)
		}
		if payload.SidebarCollapsed != nil {
			next.SidebarCollapsed = *payload.SidebarCollapsed
		} else if payload.SidebarCollapsedAlt != nil {
			next.SidebarCollapsed = *payload.SidebarCollapsedAlt
		}
		if payload.ShowHiddenFiles != nil {
			next.ShowHiddenFiles = *payload.ShowHiddenFiles
		}

		cfg.AppSettings = next
		cfgPath, err := Save(username, cfg)
		if err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		logger.Debugf("[theme.set] user=%q updated theme: theme=%s primary=%s collapsed=%v showHidden=%v path=%s",
			username, next.Theme, next.PrimaryColor, next.SidebarCollapsed, next.ShowHiddenFiles, cfgPath)

		return emit.Result(map[string]any{
			"message":          "theme updated",
			"path":             cfgPath,
			"appliedTheme":     string(next.Theme),
			"appliedPrimary":   string(next.PrimaryColor),
			"sidebarCollapsed": next.SidebarCollapsed,
			"showHiddenFiles":  next.ShowHiddenFiles,
		})
	})

	ipc.RegisterFunc("config", "docker_config_get", func(ctx context.Context, args []string, emit ipc.Events) error {
		cfg, cfgPath, err := Load(username)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}
		logger.Debugf("[docker_config.get] user=%q path=%s folder=%s",
			username, cfgPath, cfg.Docker.Folder)
		return emit.Result(cfg.Docker)
	})

	ipc.RegisterFunc("config", "docker_config_set", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		var payload struct {
			Folder *string `json:"folder"`
		}
		if err := json.Unmarshal([]byte(args[0]), &payload); err != nil {
			return ipc.ErrInvalidArgs
		}

		cfg, _, err := Load(username)
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		if payload.Folder != nil {
			folder := strings.TrimSpace(*payload.Folder)
			if folder == "" {
				return fmt.Errorf("folder cannot be empty")
			}
			cfg.Docker.Folder = AbsolutePath(folder)
		}

		cfgPath, err := Save(username, cfg)
		if err != nil {
			return fmt.Errorf("save config: %w", err)
		}

		logger.Debugf("[docker_config.set] user=%q updated docker config: folder=%s path=%s",
			username, cfg.Docker.Folder, cfgPath)

		return emit.Result(map[string]any{
			"message":       "docker config updated",
			"path":          cfgPath,
			"appliedFolder": string(cfg.Docker.Folder),
		})
	})
}
