package config

import "github.com/mordilloSan/LinuxIO/backend/bridge/settings"

func applyJobSettingsUpdate(jobs *settings.JobSettings, payload *configJobSettingsPayload) error {
	if err := applyOptionalNonNegativeInt(&jobs.ProgressMinIntervalMs, payload.ProgressMinIntervalMs, "jobs.progressMinIntervalMs"); err != nil {
		return err
	}
	if err := applyOptionalNonNegativeInt(&jobs.NotificationMinIntervalMs, payload.NotificationMinIntervalMs, "jobs.notificationMinIntervalMs"); err != nil {
		return err
	}
	if err := applyOptionalNonNegativeInt(&jobs.ProgressMinBytesMB, payload.ProgressMinBytesMB, "jobs.progressMinBytesMB"); err != nil {
		return err
	}
	if err := applyOptionalPositiveInt(&jobs.HeavyArchiveConcurrency, payload.HeavyArchiveConcurrency, "jobs.heavyArchiveConcurrency"); err != nil {
		return err
	}
	if err := applyOptionalNonNegativeInt(&jobs.ArchiveCompressionWorkers, payload.ArchiveCompressionWorkers, "jobs.archiveCompressionWorkers"); err != nil {
		return err
	}
	return applyOptionalNonNegativeInt(&jobs.ArchiveExtractWorkers, payload.ArchiveExtractWorkers, "jobs.archiveExtractWorkers")
}
