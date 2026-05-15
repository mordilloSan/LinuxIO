package config

import (
	"fmt"

	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func applyConfigPayload(cfg *bridgeconfig.Settings, payload *configSetPayload) error {
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
