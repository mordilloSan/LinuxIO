package config

import (
	"errors"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func applyJobSettingsUpdate(jobs *userconfig.PersistedJobSettings, payload *apischema.ConfigJobSettingsPayload) error {
	next := *jobs
	applyOptionalInt(&next.ProgressMinIntervalMs, payload.ProgressMinIntervalMs)
	applyOptionalInt(&next.NotificationMinIntervalMs, payload.NotificationMinIntervalMs)
	applyOptionalInt(&next.ProgressMinBytesMB, payload.ProgressMinBytesMB)
	applyOptionalInt(&next.ArchiveCompressionWorkers, payload.ArchiveCompressionWorkers)
	applyOptionalInt(&next.ArchiveExtractWorkers, payload.ArchiveExtractWorkers)
	if errs := userconfig.ValidateJobSettings(next); len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	*jobs = next
	return nil
}
