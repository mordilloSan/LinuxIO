package logging

import (
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/common/logging/journald"
)

func Configure(identifier string, verbose bool) error {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}

	handler, err := journald.NewHandler(journald.Options{
		Identifier:     identifier,
		Level:          level,
		AddSource:      true,
		FieldPrefix:    "LINUXIO",
		SuppressFields: []string{"SESSION_ID"},
	})
	if err != nil {
		return err
	}

	slog.SetDefault(slog.New(handler))
	return nil
}
