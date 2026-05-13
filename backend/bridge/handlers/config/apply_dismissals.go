package config

import (
	"strings"

	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func applyDismissalsUpdate(dismissals **bridgeconfig.Dismissals, payload *configDismissalsPayload) {
	if *dismissals == nil {
		*dismissals = &bridgeconfig.Dismissals{}
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
