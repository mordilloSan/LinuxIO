package config

import (
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/settings"
)

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
