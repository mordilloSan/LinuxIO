package logging

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/mordilloSan/LinuxIO/backend/common/logging/journald"
)

// configured reports whether Configure installed the journald handler.
var configured bool

// level is the live threshold shared with the journald handler, so SetDebug
// takes effect without rebuilding the logger.
var level slog.LevelVar

// SetDebug switches the process between Debug and Info logging.
func SetDebug(enabled bool) {
	if enabled {
		level.Set(slog.LevelDebug)
		return
	}
	level.Set(slog.LevelInfo)
}

// DebugEnabled reports whether Debug records are currently admitted.
func DebugEnabled() bool {
	return level.Level() <= slog.LevelDebug
}

func Configure(identifier string, verbose bool) error {
	SetDebug(verbose)

	handler, err := journald.NewHandler(journald.Options{
		Identifier:     identifier,
		Level:          &level,
		AddSource:      true,
		FieldPrefix:    "LINUXIO",
		SuppressFields: []string{"SESSION_ID"},
	})
	if err != nil {
		return err
	}

	slog.SetDefault(slog.New(handler))
	configured = true
	return nil
}

// ReportFatal records a daemon's fatal exit error at Error priority; systemd
// records anything the process prints to stderr at info priority. Interactive
// runs also get the error on stderr, unless the logger never came up (slog
// already writes there) or stderr is the journal. It does not exit.
func ReportFatal(err error) {
	slog.Error("daemon exited", "error", err)
	if configured && os.Getenv("JOURNAL_STREAM") == "" {
		fmt.Fprintln(os.Stderr, err)
	}
}
