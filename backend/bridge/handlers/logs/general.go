package logs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

// journaldFieldMatch matches journald-style KEY=VALUE operands. The key must
// start with an uppercase letter or underscore and contain only uppercase
// letters, digits, and underscores. Anything else is rejected to keep
// untrusted UI input from being passed straight to journalctl.
var journaldFieldMatch = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*=.*$`)

// isValidJournalCursor bounds cursor lookups: journal cursors are printable
// ASCII key=value pairs joined by semicolons. The value is passed as a single
// execve argument (never through a shell), so printable + bounded is enough.
func isValidJournalCursor(cursor string) bool {
	if cursor == "" || len(cursor) > 1024 {
		return false
	}
	for i := 0; i < len(cursor); i++ {
		if cursor[i] < 0x20 || cursor[i] > 0x7e {
			return false
		}
	}
	return true
}

const streamTypeGeneralLogs = "logs.general.follow"

// maxBacklogLines is the server-side cap on the backlog phase. "All in
// window" on a busy system can match hundreds of thousands of entries; the
// frontend's initial ring starts at this size, so journalctl is killed once
// the cap is reached and older entries are fetched by cursor on demand.
const maxBacklogLines = 5000

const (
	// followFlushInterval coalesces follow-mode lines into batched frames so a
	// chatty journal produces a few frames per second, not one per line.
	followFlushInterval = 150 * time.Millisecond
	// flushChunkBytes flushes a frame early once it grows past this size.
	flushChunkBytes = 64 * 1024
	// maxBacklogBytes bounds the temporary in-memory backlog before it is written
	// to the Channel.
	maxBacklogBytes = 16 * 1024 * 1024
	// entryLookupTimeout bounds the one-shot cursor lookup for logs.general_entry.
	entryLookupTimeout = 10 * time.Second
	// defaultPageLines / maxPageLines size the "load earlier" pagination pages;
	// pageLookupTimeout bounds each page query.
	defaultPageLines  = 500
	maxPageLines      = 1000
	pageLookupTimeout = 30 * time.Second
)

// keepJournalFields is the allowlist of journald fields forwarded to the UI
// list; LINUXIO_* user fields are always kept. Everything else is stripped
// server-side — the full entry is available on demand via logs.general_entry.
var keepJournalFields = map[string]struct{}{
	"__CURSOR":             {},
	"__REALTIME_TIMESTAMP": {},
	"_BOOT_ID":             {},
	"MESSAGE":              {},
	"PRIORITY":             {},
	"SYSLOG_IDENTIFIER":    {},
	"_COMM":                {},
	"_SYSTEMD_UNIT":        {},
	"UNIT":                 {},
	"CODE_FUNC":            {},
}

type generalLogsRequest struct {
	lines        string
	timePeriod   string
	priority     string
	identifier   string
	fieldFilters []string
	follow       bool
	afterCursor  string
}

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
			// (filter change, live toggle, navigating away) landing mid-backlog,
			// not a failure.
			level, message := slog.LevelError, "general log backlog failed"
			if errors.Is(err, context.Canceled) {
				level, message = slog.LevelDebug, "general log backlog canceled"
			}
			slog.Log(ctx, level, message,
				"component", "logs",
				"route", streamTypeGeneralLogs,
				"error", err)
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

func writeLogError(stream net.Conn, err error) error {
	code := 500
	var bridgeErr *bridgeipc.Error
	if errors.As(err, &bridgeErr) && bridgeErr.Code != 0 {
		code = bridgeErr.Code
	}
	return relay.WriteResultErrorAndClose(stream, 0, err.Error(), code)
}

func writeLogErrorUnlessCanceled(ctx context.Context, stream net.Conn, err error) error {
	if errors.Is(err, context.Canceled) || ctx.Err() != nil {
		return relay.WriteStreamClose(stream, 0)
	}
	return writeLogError(stream, err)
}

func parseGeneralLogsRequest(request apischema.GeneralLogsFollowRequest) generalLogsRequest {
	req := generalLogsRequest{lines: "100", follow: true}
	if request.Lines != nil && strings.TrimSpace(*request.Lines) != "" {
		req.lines = strings.TrimSpace(*request.Lines)
	}
	if request.TimePeriod != nil && strings.TrimSpace(*request.TimePeriod) != "" {
		req.timePeriod = strings.TrimSpace(*request.TimePeriod)
	}
	if request.Priority != nil && strings.TrimSpace(*request.Priority) != "" {
		req.priority = strings.TrimSpace(*request.Priority)
	}
	if request.Identifier != nil && strings.TrimSpace(*request.Identifier) != "" {
		req.identifier = strings.TrimSpace(*request.Identifier)
	}
	if request.Follow != nil {
		req.follow = *request.Follow
	}
	if request.AfterCursor != nil {
		req.afterCursor = strings.TrimSpace(*request.AfterCursor)
	}
	for _, raw := range request.FieldFilters {
		f := strings.TrimSpace(raw)
		if f == "" || !journaldFieldMatch.MatchString(f) {
			continue
		}
		req.fieldFilters = append(req.fieldFilters, f)
	}
	return req
}

func appendCommonFilters(args []string, req generalLogsRequest) []string {
	if req.priority != "" {
		args = append(args, "-p", req.priority)
	}
	if req.identifier != "" {
		args = append(args, "-t", req.identifier)
	}
	return append(args, req.fieldFilters...)
}

func backlogArgs(req generalLogsRequest) []string {
	args := []string{"-q", "--no-pager", "-o", "json", "--reverse"}
	if req.lines != "" && req.lines != "all" {
		args = append(args, "-n", req.lines)
	}
	if req.timePeriod != "" {
		args = append(args, "--since", req.timePeriod+" ago")
	}
	return appendCommonFilters(args, req)
}

func followArgs(req generalLogsRequest, afterCursor string) []string {
	args := []string{"-q", "--no-pager", "-o", "json", "--follow"}
	switch {
	case afterCursor != "":
		args = append(args, "--after-cursor", afterCursor)
	case req.timePeriod != "":
		// Empty backlog: re-cover the (proven empty) window so entries logged
		// between the two journalctl processes are not lost.
		args = append(args, "--no-tail", "--since", req.timePeriod+" ago")
	default:
		args = append(args, "-n", "0")
	}
	return appendCommonFilters(args, req)
}

func generalPageArgs(req generalLogsRequest, cursor string) []string {
	// journalctl treats --since, --cursor, --cursor-file and --after-cursor as
	// mutually exclusive seek positions. The selected time window is enforced
	// while reading entries below instead of being passed as --since.
	args := []string{
		"-q", "--no-pager", "-o", "json", "--reverse", "--cursor", cursor,
	}
	return appendCommonFilters(args, req)
}

func relativeJournalPeriod(value string) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}

	if before, ok := strings.CutSuffix(value, "d"); ok {
		days, err := strconv.ParseUint(before, 10, 16)
		if err != nil || days == 0 {
			return 0, false
		}
		return time.Duration(days) * 24 * time.Hour, true
	}

	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, false
	}
	return duration, true
}

func journalPeriodCutoff(value string, now time.Time) (int64, bool) {
	duration, ok := relativeJournalPeriod(value)
	if !ok {
		return 0, false
	}
	return now.Add(-duration).UnixMicro(), true
}

func journalLineOlderThan(line string, cutoffMicros int64) bool {
	var envelope struct {
		RealtimeTimestamp json.RawMessage `json:"__REALTIME_TIMESTAMP"`
	}
	if err := json.Unmarshal([]byte(line), &envelope); err != nil ||
		len(envelope.RealtimeTimestamp) == 0 {
		return false
	}

	rawTimestamp := strings.Trim(string(envelope.RealtimeTimestamp), `"`)
	timestamp, err := strconv.ParseInt(rawTimestamp, 10, 64)
	return err == nil && timestamp < cutoffMicros
}

type runningJournalCommand struct {
	cmd    *exec.Cmd
	stderr bytes.Buffer
	stdout io.ReadCloser
}

var journalCommandContext = exec.CommandContext

func startJournalCommand(ctx context.Context, args []string) (*runningJournalCommand, error) {
	command := &runningJournalCommand{}
	command.cmd = journalCommandContext(ctx, "journalctl", args...)
	command.cmd.Stderr = &command.stderr

	stdout, err := command.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	command.stdout = stdout
	if err := command.cmd.Start(); err != nil {
		return nil, err
	}
	return command, nil
}

// finish waits for journalctl and distinguishes an intentional early stop
// (our line/byte/page limit) from a genuine command failure.
func (command *runningJournalCommand) finish(ctx context.Context, readErr error, stopped bool) error {
	waitErr := command.cmd.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	if readErr != nil {
		return readErr
	}
	if waitErr == nil || stopped || isJournalctlNoEntries(waitErr, command.stderr.String()) {
		return nil
	}
	return journalctlError(waitErr, command.stderr.String())
}

func journalOutputLine(raw string) (string, bool) {
	line := strings.TrimSpace(raw)
	return line, line != "" && !strings.HasPrefix(line, "-- ")
}

func journalReadError(err error) error {
	if err == io.EOF || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// readJournalOutput reads complete journal entries until EOF or until consume
// returns false. stopped reports that the consumer deliberately ended the
// command, allowing its kill-induced exit status to be ignored.
func readJournalOutput(
	ctx context.Context,
	command *runningJournalCommand,
	consume func(string) bool,
) (stopped bool, err error) {
	reader := bufio.NewReaderSize(command.stdout, 256*1024)
	for {
		if ctx.Err() != nil {
			killLogsProcess(command.cmd)
			return false, nil
		}

		raw, readErr := reader.ReadString('\n')
		if line, ok := journalOutputLine(raw); ok && !consume(line) {
			killLogsProcess(command.cmd)
			return true, nil
		}
		if readErr != nil {
			return false, journalReadError(readErr)
		}
	}
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

type journalDataBatch struct {
	stream net.Conn
	buf    strings.Builder
}

func (batch *journalDataBatch) append(line string) error {
	lineBytes := len(line) + 1
	if batch.buf.Len() > 0 && batch.buf.Len()+lineBytes > flushChunkBytes {
		if err := batch.flush(); err != nil {
			return err
		}
	}
	batch.buf.WriteString(line)
	batch.buf.WriteByte('\n')
	if batch.buf.Len() >= flushChunkBytes {
		return batch.flush()
	}
	return nil
}

func (batch *journalDataBatch) flush() error {
	if batch.buf.Len() == 0 {
		return nil
	}
	err := relay.WriteRelayFrame(batch.stream, &relay.StreamFrame{Opcode: relay.OpStreamData, StreamID: 0, Payload: []byte(batch.buf.String())})
	batch.buf.Reset()
	return err
}

func (batch *journalDataBatch) drain(lines <-chan string) error {
	for {
		select {
		case line := <-lines:
			if err := batch.append(line); err != nil {
				return err
			}
		default:
			return nil
		}
	}
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

func startJournalLineReader(
	ctx context.Context,
	stdout io.Reader,
) (<-chan string, <-chan error) {
	lines := make(chan string, 256)
	done := make(chan error, 1)
	go func() {
		done <- readJournalLines(ctx, stdout, lines)
	}()
	return lines, done
}

func readJournalLines(ctx context.Context, stdout io.Reader, lines chan<- string) error {
	reader := bufio.NewReaderSize(stdout, 256*1024)
	for {
		raw, readErr := reader.ReadString('\n')
		if line, ok := journalOutputLine(raw); ok {
			entry, _ := trimJournalLine(line)
			select {
			case lines <- entry:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if readErr != nil {
			return journalReadError(readErr)
		}
	}
}

func cancelGeneralLogsFollow(
	ctx context.Context,
	command *runningJournalCommand,
	readDone <-chan error,
	batch *journalDataBatch,
	lines <-chan string,
) error {
	killLogsProcess(command.cmd)
	<-readDone
	_ = command.cmd.Wait()
	if err := batch.drain(lines); err != nil {
		return err
	}
	if err := batch.flush(); err != nil {
		return err
	}
	return ctx.Err()
}

func finishGeneralLogsFollow(
	ctx context.Context,
	command *runningJournalCommand,
	readErr error,
	batch *journalDataBatch,
	lines <-chan string,
) error {
	finishErr := command.finish(ctx, readErr, false)
	if err := batch.drain(lines); err != nil {
		return err
	}
	if err := batch.flush(); err != nil {
		return err
	}
	if finishErr != nil {
		return finishErr
	}
	// A follow process exiting on its own is unexpected; surface it so the
	// frontend can reconnect instead of showing a silent dead tail.
	return errors.New("journalctl follow stream ended unexpectedly")
}

// streamGeneralLogsFollow tails the journal from just after newestCursor,
// coalescing lines into batched frames on a size/interval policy.
func streamGeneralLogsFollow(ctx context.Context, stream net.Conn, req generalLogsRequest, afterCursor string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	command, err := startJournalCommand(ctx, followArgs(req, afterCursor))
	if err != nil {
		return err
	}

	lines, readDone := startJournalLineReader(ctx, command.stdout)
	abort := func() {
		cancel()
		killLogsProcess(command.cmd)
		<-readDone
		_ = command.cmd.Wait()
	}
	ticker := time.NewTicker(followFlushInterval)
	defer ticker.Stop()
	batch := journalDataBatch{stream: stream}

	for {
		select {
		case <-ctx.Done():
			return cancelGeneralLogsFollow(ctx, command, readDone, &batch, lines)
		case line := <-lines:
			if err := batch.append(line); err != nil {
				abort()
				return err
			}
		case <-ticker.C:
			if err := batch.flush(); err != nil {
				abort()
				return err
			}
		case readErr := <-readDone:
			return finishGeneralLogsFollow(ctx, command, readErr, &batch, lines)
		}
	}
}

// trimJournalLine strips a journalctl -o json line down to the fields the UI
// consumes (keepJournalFields + LINUXIO_*), returning the reduced JSON and the
// entry's cursor. Lines that fail to parse pass through unchanged.
func trimJournalLine(line string) (trimmed string, cursor string) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &fields); err != nil {
		return line, ""
	}
	if raw, ok := fields["__CURSOR"]; ok {
		_ = json.Unmarshal(raw, &cursor)
	}
	kept := make(map[string]json.RawMessage, len(keepJournalFields)+4)
	for key, value := range fields {
		if _, ok := keepJournalFields[key]; ok || strings.HasPrefix(key, "LINUXIO_") {
			kept[key] = value
		}
	}
	out, err := json.Marshal(kept)
	if err != nil {
		return line, cursor
	}
	return string(out), cursor
}

func generalLogsPageSize(lines *string) int {
	if lines == nil {
		return defaultPageLines
	}
	size, err := strconv.Atoi(strings.TrimSpace(*lines))
	if err != nil || size <= 0 {
		return defaultPageLines
	}
	return min(size, maxPageLines)
}

type generalLogsPageCollector struct {
	cursor       string
	pageSize     int
	cutoffMicros int64
	hasCutoff    bool
	entries      []string
	truncated    bool
}

func newGeneralLogsPageCollector(
	cursor string,
	pageSize int,
	timePeriod string,
) *generalLogsPageCollector {
	cutoffMicros, hasCutoff := journalPeriodCutoff(timePeriod, time.Now())
	return &generalLogsPageCollector{
		cursor:       cursor,
		pageSize:     pageSize,
		cutoffMicros: cutoffMicros,
		hasCutoff:    hasCutoff,
		entries:      make([]string, 0, pageSize+1),
	}
}

func (page *generalLogsPageCollector) consume(line string) bool {
	if page.hasCutoff && journalLineOlderThan(line, page.cutoffMicros) {
		return false
	}

	entry, entryCursor := trimJournalLine(line)
	if entryCursor == page.cursor {
		return true
	}
	page.entries = append(page.entries, entry)
	if len(page.entries) <= page.pageSize {
		return true
	}
	page.truncated = true
	return false
}

func (page *generalLogsPageCollector) response() apischema.GeneralLogsPageResponse {
	if page.truncated {
		page.entries = page.entries[:page.pageSize]
	}
	return apischema.GeneralLogsPageResponse{
		Entries: page.entries,
		HasMore: page.truncated,
	}
}

// GetGeneralLogsPage returns up to the requested number of trimmed entries
// strictly OLDER than the given cursor (newest-first), under the same filters
// and time window as the live view — the backend half of "load earlier
// entries" pagination. HasMore reports whether older matches remain.
func GetGeneralLogsPage(ctx context.Context, request apischema.GeneralLogsPageRequest) (apischema.GeneralLogsPageResponse, error) {
	resp := apischema.GeneralLogsPageResponse{Entries: []string{}}
	cursor := strings.TrimSpace(request.Cursor)
	if !isValidJournalCursor(cursor) {
		return resp, errors.New("invalid journal cursor")
	}
	req := parseGeneralLogsRequest(apischema.GeneralLogsFollowRequest{
		TimePeriod:   request.TimePeriod,
		Priority:     request.Priority,
		Identifier:   request.Identifier,
		FieldFilters: request.FieldFilters,
	})

	ctx, cancel := context.WithTimeout(ctx, pageLookupTimeout)
	defer cancel()

	// --reverse --cursor starts AT the boundary entry and walks backwards;
	// the boundary itself is dropped below by cursor equality. --since cannot
	// be combined with --cursor, so stop at the equivalent timestamp cutoff.
	page := newGeneralLogsPageCollector(cursor, generalLogsPageSize(request.Lines), req.timePeriod)
	command, err := startJournalCommand(ctx, generalPageArgs(req, cursor))
	if err != nil {
		return resp, err
	}

	stopped, readErr := readJournalOutput(ctx, command, page.consume)
	if err := command.finish(ctx, readErr, stopped); err != nil {
		return apischema.GeneralLogsPageResponse{}, err
	}
	return page.response(), nil
}

// GetGeneralLogEntry fetches a single full journal entry by cursor — used by
// the UI's expanded-row raw view so list entries only carry trimmed fields.
func GetGeneralLogEntry(ctx context.Context, cursor string) (map[string]any, error) {
	cursor = strings.TrimSpace(cursor)
	if !isValidJournalCursor(cursor) {
		return nil, errors.New("invalid journal cursor")
	}
	ctx, cancel := context.WithTimeout(ctx, entryLookupTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, "journalctl",
		"-q", "--no-pager", "-o", "json", "-n", "1", "--cursor", cursor).Output()
	if err != nil {
		var stderr string
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			stderr = string(exitErr.Stderr)
		}
		if isJournalctlNoEntries(err, stderr) {
			return nil, errors.New("journal entry not found")
		}
		return nil, journalctlError(err, stderr)
	}
	line := strings.TrimSpace(string(out))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if line == "" {
		return nil, errors.New("journal entry not found")
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// isJournalctlNoEntries reports whether err is legacy journalctl's benign
// "no entries matched" exit (status 1 with a silent stderr). Modern systemd
// exits 0 on zero matches and reserves 1 for genuine failures — those write
// to stderr and must not be swallowed.
func isJournalctlNoEntries(err error, stderr string) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == 1 &&
		strings.TrimSpace(stderr) == ""
}

// journalctlError prefers journalctl's own stderr message over Go's generic
// "exit status N" so the failure reason reaches the UI error banner.
func journalctlError(err error, stderr string) error {
	if message := strings.TrimSpace(stderr); message != "" {
		if i := strings.IndexByte(message, '\n'); i >= 0 {
			message = message[:i]
		}
		return errors.New(message)
	}
	return err
}

func killLogsProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	if killErr := cmd.Process.Kill(); killErr != nil {
		slog.Debug("failed to kill journalctl process",
			"component", "logs",
			"error", killErr)
	}
}
