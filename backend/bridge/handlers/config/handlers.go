package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mordilloSan/go_logger/logger"

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

func ThemeHandlers(sess *session.Session) map[string]ipc.HandlerFunc {
	username := sess.User.Username

	return map[string]ipc.HandlerFunc{
		"theme_get": func(args []string) (any, error) {
			cfg, cfgPath, err := Load(username)
			if err != nil {
				return nil, fmt.Errorf("load config: %w", err)
			}
			logger.Debugf("[theme.get] user=%q path=%s theme=%s primary=%s collapsed=%v showHidden=%v",
				username, cfgPath, cfg.AppSettings.Theme, cfg.AppSettings.PrimaryColor, cfg.AppSettings.SidebarCollapsed, cfg.AppSettings.ShowHiddenFiles)
			return cfg.AppSettings, nil
		},
		"theme_set": func(args []string) (any, error) {
			if len(args) < 1 {
				return nil, fmt.Errorf("bad_request:missing payload")
			}
			var payload themePayload
			if err := json.Unmarshal([]byte(args[0]), &payload); err != nil {
				return nil, fmt.Errorf("bad_request:invalid request body")
			}

			cfg, _, err := Load(username)
			if err != nil {
				return nil, fmt.Errorf("load config: %w", err)
			}
			prev := cfg.AppSettings
			next := prev

			if payload.Theme != nil {
				t := strings.ToUpper(strings.TrimSpace(*payload.Theme))
				if t != "LIGHT" && t != "DARK" {
					return nil, fmt.Errorf("bad_request:invalid theme value (LIGHT|DARK)")
				}
				next.Theme = t
			}
			if payload.PrimaryColor != nil {
				if !IsValidCSSColor(*payload.PrimaryColor) {
					return nil, fmt.Errorf("bad_request:invalid primaryColor")
				}
				next.PrimaryColor = *payload.PrimaryColor
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
				return nil, fmt.Errorf("save config: %w", err)
			}

			logger.Debugf("[theme.set] user=%q updated theme: theme=%s primary=%s collapsed=%v showHidden=%v path=%s",
				username, next.Theme, next.PrimaryColor, next.SidebarCollapsed, next.ShowHiddenFiles, cfgPath)

			return map[string]any{
				"message":          "theme updated",
				"path":             cfgPath,
				"appliedTheme":     next.Theme,
				"appliedPrimary":   next.PrimaryColor,
				"sidebarCollapsed": next.SidebarCollapsed,
				"showHiddenFiles":  next.ShowHiddenFiles,
			}, nil
		},
	}
}
