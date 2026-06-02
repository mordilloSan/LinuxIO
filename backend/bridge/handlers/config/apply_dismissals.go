package config

import (
	"strings"

	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func applyDismissalsUpdate(dismissals **bridgeconfig.PersistedDismissals, payload *configDismissalsPayload) {
	if *dismissals == nil {
		*dismissals = &bridgeconfig.PersistedDismissals{}
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
