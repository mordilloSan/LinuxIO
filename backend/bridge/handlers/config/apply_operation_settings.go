package config

import (
	"fmt"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
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
		return fmt.Errorf("%w: %s", bridgeipc.ErrInvalidArgs, strings.Join(errs, "; "))
	}
	*jobs = next
	return nil
}
