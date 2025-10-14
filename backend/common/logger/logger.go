package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/coreos/go-systemd/v22/journal"
)

var (
	Debug   = log.New(io.Discard, "", 0)
	Info    = log.New(io.Discard, "", 0)
	Warning = log.New(io.Discard, "", 0)
	Error   = log.New(io.Discard, "", 0)

	programName string
)

// journalWriter writes to systemd journal with the program name as identifier.
type journalWriter struct {
	priority journal.Priority
}

func (j journalWriter) Write(p []byte) (int, error) {
	msg := strings.TrimSuffix(string(p), "\n")
	err := journal.Send(msg, j.priority, map[string]string{
		"SYSLOG_IDENTIFIER": programName,
	})
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// Init sets up the logger for the environment.
func Init(mode string, verbose bool) {
	programName = filepath.Base(os.Args[0])

	if mode == "production" {
		if journal.Enabled() {
			// In prod with journald, always log everything (even Debug) to journald, never to stdout/stderr.
			Debug = log.New(journalWriter{journal.PriDebug}, "", 0)
			Info = log.New(journalWriter{journal.PriInfo}, "", 0)
			Warning = log.New(journalWriter{journal.PriWarning}, "", 0)
			Error = log.New(journalWriter{journal.PriErr}, "", 0)
			return
		} else {
			// In prod but journald is not enabled (very rare): discard all logs to avoid leaking to stdout/stderr.
			Debug = log.New(io.Discard, "", 0)
			Info = log.New(io.Discard, "", 0)
			Warning = log.New(io.Discard, "", 0)
			Error = log.New(io.Discard, "", 0)
			return
		}
	}
	// Development: Only log Debug if verbose=true, others always to stdout with color
	Debug = newDevLogger(os.Stdout, "DEBUG", verbose)
	Info = newDevLogger(os.Stdout, "INFO", true)
	Warning = newDevLogger(os.Stdout, "WARN", true)
	Error = newDevLogger(os.Stdout, "ERROR", true)
}

// newDevLogger returns a colored logger for the level, or discards if disabled.
func newDevLogger(out io.Writer, level string, enabled bool) *log.Logger {
	if !enabled {
		return log.New(io.Discard, "", 0)
	}
	colors := map[string]string{
		"DEBUG": "\033[36m", // Cyan
		"INFO":  "\033[32m", // Green
		"WARN":  "\033[33m", // Yellow
		"ERROR": "\033[31m", // Red
	}
	reset := "\033[0m"
	levelLabel := fmt.Sprintf("%s[%s]%s", colors[level], level, reset)
	prefix := fmt.Sprintf("%s [%s] ", levelLabel, programName)
	return log.New(out, prefix, log.LstdFlags)
}

// getCallerFuncName returns a "package.function" string for the caller at stack depth.
func getCallerFuncName(depth int) string {
	pc, _, _, ok := runtime.Caller(depth)
	if !ok {
		return "unknown"
	}
	fn := runtime.FuncForPC(pc)
	if fn == nil {
		return "unknown"
	}
	full := fn.Name()
	lastSlash := strings.LastIndex(full, "/")
	if lastSlash >= 0 && lastSlash+1 < len(full) {
		full = full[lastSlash+1:]
	}
	return full
}

// --- Wrapper logging functions, always prints function name automatically --- //

func Debugf(format string, v ...interface{}) {
	msg := fmt.Sprintf("[%s] %s", getCallerFuncName(2), fmt.Sprintf(format, v...))
	Debug.Println(msg)
}

func Infof(format string, v ...interface{}) {
	msg := fmt.Sprintf("[%s] %s", getCallerFuncName(2), fmt.Sprintf(format, v...))
	Info.Println(msg)
}

func Warnf(format string, v ...interface{}) {
	msg := fmt.Sprintf("[%s] %s", getCallerFuncName(2), fmt.Sprintf(format, v...))
	Warning.Println(msg)
}

func Errorf(format string, v ...interface{}) {
	msg := fmt.Sprintf("[%s] %s", getCallerFuncName(2), fmt.Sprintf(format, v...))
	Error.Println(msg)
}

// --- Plain "Println" helpers for literal messages (rare) --- //

func Debugln(v ...interface{}) {
	msg := fmt.Sprintf("[%s] %s", getCallerFuncName(2), fmt.Sprint(v...))
	Debug.Println(msg)
}

func Infoln(v ...interface{}) {
	msg := fmt.Sprintf("[%s] %s", getCallerFuncName(2), fmt.Sprint(v...))
	Info.Println(msg)
}

func Warnln(v ...interface{}) {
	msg := fmt.Sprintf("[%s] %s", getCallerFuncName(2), fmt.Sprint(v...))
	Warning.Println(msg)
}

func Errorln(v ...interface{}) {
	msg := fmt.Sprintf("[%s] %s", getCallerFuncName(2), fmt.Sprint(v...))
	Error.Println(msg)
}
