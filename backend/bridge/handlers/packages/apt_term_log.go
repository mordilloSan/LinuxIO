package packages

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// aptTermLogPath is apt's Dir::Log::Terminal. PackageKit's apt backend tees
// dpkg's terminal output there, which is the only place that output exists.
var aptTermLogPath = "/var/log/apt/term.log"

const aptTermLogPollInterval = 250 * time.Millisecond

// aptTermLog follows term.log from the offset it was opened at.
type aptTermLog struct {
	file    *os.File
	partial []byte
}

// openAptTermLog returns nil when the log cannot be read (non-apt hosts,
// sessions without adm access); raw output is optional.
func openAptTermLog() *aptTermLog {
	file, err := os.Open(aptTermLogPath)
	if err != nil {
		slog.Debug("apt terminal log unavailable", "path", aptTermLogPath, "error", err)
		return nil
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		slog.Debug("apt terminal log unavailable", "path", aptTermLogPath, "error", err)
		_ = file.Close()
		return nil
	}
	return &aptTermLog{file: file}
}

// lines returns the complete lines appended since the last call, as a
// terminal would show them.
// shortcut: a logrotate of term.log during a transaction ends the output; reopen on truncation if that shows up.
func (l *aptTermLog) lines() []string {
	if l == nil {
		return nil
	}
	chunk, err := io.ReadAll(l.file)
	if err != nil {
		slog.Debug("failed to read apt terminal log", "path", aptTermLogPath, "error", err)
	}
	l.partial = append(l.partial, chunk...)
	before, after, ok := bytes.CutLast(l.partial, []byte{'\n'})
	if !ok {
		return nil
	}
	complete := string(before)
	l.partial = append([]byte(nil), after...)

	var lines []string
	for line := range strings.SplitSeq(complete, "\n") {
		// The pty ends lines in \r\n; dpkg redraws progress with carriage returns, keep the final redraw.
		line = strings.TrimSuffix(line, "\r")
		line = line[strings.LastIndexByte(line, '\r')+1:]
		if line == "" || strings.HasPrefix(line, "Log started: ") || strings.HasPrefix(line, "Log ended: ") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func (l *aptTermLog) Close() {
	if l != nil {
		_ = l.file.Close()
	}
}

func reportAptTermLog(report pkgUpdateReporter, termLog *aptTermLog) {
	for _, line := range termLog.lines() {
		reportPkgUpdateProgress(report, &PkgUpdateProgress{Type: "output", Message: line})
	}
}
