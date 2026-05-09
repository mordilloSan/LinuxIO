package logging

import (
	"log"
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

	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Route standard-library log output through the default slog handler too.
	log.SetFlags(0)
	log.SetOutput(slog.NewLogLogger(logger.Handler(), slog.LevelInfo).Writer())
	return nil
}
