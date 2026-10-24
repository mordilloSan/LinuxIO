package config

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func applyConfigPayload(cfg *userconfig.Settings, payload *apischema.ConfigSetPayload) error {
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
	if payload.Assistant != nil {
		if err := applyAssistantSettingsUpdate(&cfg.Assistant, payload.Assistant); err != nil {
			return err
		}
	}
	if payload.Dismissals != nil {
		applyDismissalsUpdate(&cfg.Dismissals, payload.Dismissals)
	}
	return nil
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

func applyOptionalInt(dst *int, value *int) {
	if value != nil {
		*dst = *value
	}
}
