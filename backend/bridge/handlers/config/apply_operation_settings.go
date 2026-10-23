package config

import (
	"errors"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
)

func applyJobSettingsUpdate(jobs *userconfig.PersistedJobSettings, payload *apischema.ConfigJobSettingsPayload) error {
	if payload.HeavyArchiveConcurrency != nil && *payload.HeavyArchiveConcurrency <= 0 {
		return errors.New("jobs.heavyArchiveConcurrency must be > 0")
	}
	next := *jobs
	applyOptionalInt(&next.ProgressMinIntervalMs, payload.ProgressMinIntervalMs)
	applyOptionalInt(&next.NotificationMinIntervalMs, payload.NotificationMinIntervalMs)
	applyOptionalInt(&next.ProgressMinBytesMB, payload.ProgressMinBytesMB)
	applyOptionalInt(&next.HeavyArchiveConcurrency, payload.HeavyArchiveConcurrency)
	applyOptionalInt(&next.ArchiveCompressionWorkers, payload.ArchiveCompressionWorkers)
	applyOptionalInt(&next.ArchiveExtractWorkers, payload.ArchiveExtractWorkers)
	if errs := userconfig.ValidateJobSettings(next); len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	*jobs = next
	return nil
}
