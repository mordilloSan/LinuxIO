package logs

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"slices"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

const streamTypeGeneralLogs = "logs.general.follow"

// maxBacklogLines is the server-side cap on the backlog phase. "All in
// window" on a busy system can match hundreds of thousands of entries; the
// frontend's initial ring starts at this size, so journalctl is killed once
// the cap is reached and older entries are fetched by cursor on demand.
const maxBacklogLines = 5000

// maxBacklogBytes bounds the temporary in-memory backlog before it is written
// to the Channel.
const maxBacklogBytes = 16 * 1024 * 1024

// streamGeneralLogsChannel streams general journal logs through a direct channel
// lifecycle in two phases, mirroring Cockpit's journal viewer: a bounded
// one-shot backlog query (newest-first, killed at maxBacklogLines) followed by
// a live tail anchored at the newest cursor. The split gives a deterministic
// "backlog_complete" signal even when zero entries match, and keeps "All in
// window" from streaming an unbounded journal.
func streamGeneralLogsChannel(parent context.Context, stream net.Conn, _ runtime.Runtime, request apischema.GeneralLogsFollowRequest) error {
	ctx, cleanup := bridgeipc.ReceiveOnlyChannelContext(parent, stream)
	defer cleanup()
	req := parseGeneralLogsRequest(request)
	if req.scopeError != nil {
		return writeLogError(stream, req.scopeError)
	}
	slog.Debug("starting general log channel",
		"component", "logs",
		"route", streamTypeGeneralLogs,
		"lines", req.lines,
		"time_period", req.timePeriod,
		"priority", req.priority,
		"identifier", req.identifier,
		"field_filters", strings.Join(req.fieldFilters, " "),
		"follow", req.follow)

	if req.afterCursor != "" && !isValidJournalCursor(req.afterCursor) {
		return writeLogError(stream, errors.New("invalid journal cursor"))
	}

	var newestCursor string
	var count int
	var truncated bool
	if req.afterCursor != "" {
		newestCursor = req.afterCursor
	} else {
		var err error
		newestCursor, count, truncated, err = streamGeneralLogsBacklog(ctx, stream, req)
		if err != nil {
			// A canceled context is the routine end of a client-closed stream
			// (filter change, live toggle, navigating away) landing mid-backlog.
			// Any other failure is returned and logged once by the router.
			if errors.Is(err, context.Canceled) {
				slog.Debug("general log backlog canceled",
					"component", "logs",
					"route", streamTypeGeneralLogs,
					"error", err)
			}
			return writeLogErrorUnlessCanceled(ctx, stream, err)
		}
	}

	if err := relay.WriteProgress(stream, 0, map[string]any{
		"type":      "backlog_complete",
		"count":     count,
		"truncated": truncated,
		"resumed":   req.afterCursor != "",
	}); err != nil {
		return err
	}

	if !req.follow {
		return relay.WriteResultOKAndClose(stream, 0, map[string]any{"status": "completed", "count": count, "truncated": truncated})
	}

	if err := streamGeneralLogsFollow(ctx, stream, req, newestCursor); err != nil {
		return writeLogErrorUnlessCanceled(ctx, stream, err)
	}
	return relay.WriteResultOKAndClose(stream, 0, map[string]any{"status": "stopped"})
}

type generalLogsBacklog struct {
	lines        []string
	newestCursor string
	bytes        int
	truncated    bool
}

func newGeneralLogsBacklog() *generalLogsBacklog {
	return &generalLogsBacklog{lines: make([]string, 0, 256)}
}

func (backlog *generalLogsBacklog) consume(line string) bool {
	entry, cursor := trimJournalLine(line)
	entryBytes := len(entry) + 1
	if backlog.hasNoRoom(entryBytes) {
		backlog.truncated = true
		return false
	}

	if backlog.newestCursor == "" {
		backlog.newestCursor = cursor
	}
	backlog.lines = append(backlog.lines, entry)
	backlog.bytes += entryBytes
	if backlog.reachedLimit() {
		backlog.truncated = true
		return false
	}
	return true
}

func (backlog *generalLogsBacklog) hasNoRoom(entryBytes int) bool {
	return len(backlog.lines) > 0 && backlog.bytes+entryBytes > maxBacklogBytes
}

func (backlog *generalLogsBacklog) reachedLimit() bool {
	return len(backlog.lines) >= maxBacklogLines || backlog.bytes >= maxBacklogBytes
}

func emitGeneralLogsBacklog(stream net.Conn, lines []string) error {
	batch := journalDataBatch{stream: stream}
	for _, line := range slices.Backward(lines) {
		if err := batch.append(line); err != nil {
			return err
		}
	}
	return batch.flush()
}

// streamGeneralLogsBacklog runs the one-shot newest-first query, emits the
// matched entries in chronological order as batched frames, and returns the
// newest entry's cursor for the follow phase to anchor on.
func streamGeneralLogsBacklog(ctx context.Context, stream net.Conn, req generalLogsRequest) (newestCursor string, count int, truncated bool, err error) {
	command, err := startJournalCommand(ctx, backlogArgs(req))
	if err != nil {
		return "", 0, false, err
	}

	backlog := newGeneralLogsBacklog()
	stopped, readErr := readJournalOutput(ctx, command, backlog.consume)
	if err := command.finish(ctx, readErr, stopped); err != nil {
		return "", 0, false, err
	}

	// journalctl --reverse produced newest-first; the frontend consumes
	// chronological batches.
	if err := emitGeneralLogsBacklog(stream, backlog.lines); err != nil {
		return "", 0, false, err
	}
	return backlog.newestCursor, len(backlog.lines), backlog.truncated, nil
}
