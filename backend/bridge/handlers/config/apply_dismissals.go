package config

import (
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeconfig "github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
)

func applyDismissalsUpdate(dismissals **bridgeconfig.PersistedDismissals, payload *apischema.ConfigDismissalsPayload) {
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
