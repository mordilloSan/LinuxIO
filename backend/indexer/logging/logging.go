package logging

import (
	"log"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/indexer/logging/journald"
)

// Configure installs the default slog logger. It tries to use the native
// journald handler first; if the journal socket is unavailable (e.g. running
// outside systemd), it falls back to a stderr text handler.
func Configure(identifier string, verbose bool) {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}

	handler := buildHandler(identifier, level)

	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Route standard-library log output through the default slog handler too.
	log.SetFlags(0)
	log.SetOutput(slog.NewLogLogger(handler, slog.LevelInfo).Writer())
}

func buildHandler(identifier string, level slog.Level) slog.Handler {
	jh, err := journald.NewHandler(journald.Options{
		Identifier:  identifier,
		Level:       level,
		AddSource:   true,
		FieldPrefix: "INDEXER",
	})
	if err == nil {
		return jh
	}

	return slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     level,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) != 0 {
				return a
			}
			switch a.Key {
			case slog.TimeKey, slog.LevelKey:
				return slog.Attr{}
			case slog.SourceKey:
				if src, ok := a.Value.Any().(*slog.Source); ok && src != nil {
					return slog.String(slog.SourceKey, shortSource(src.File, src.Line))
				}
			}
			return a
		},
	})
}

// shortSource keeps the last two path components of file (e.g. "cmd/daemon.go:462").
func shortSource(file string, line int) string {
	if i := strings.LastIndexByte(file, '/'); i >= 0 {
		if j := strings.LastIndexByte(file[:i], '/'); j >= 0 {
			file = file[j+1:]
		} else if i == 0 {
			file = file[i+1:]
		}
	}
	return file + ":" + strconv.Itoa(line)
}
