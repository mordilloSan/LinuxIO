package logs

import (
	"context"
	"encoding/json/v2"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

const (
	// entryLookupTimeout bounds the one-shot cursor lookup for logs.general_entry.
	entryLookupTimeout = 10 * time.Second
	// defaultPageLines / maxPageLines size the "load earlier" pagination pages;
	// pageLookupTimeout bounds each page query.
	defaultPageLines  = 500
	maxPageLines      = 1000
	pageLookupTimeout = 30 * time.Second
)

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
		Unit:         request.Unit,
		InvocationID: request.InvocationID,
		FieldFilters: request.FieldFilters,
	})
	if req.scopeError != nil {
		return resp, req.scopeError
	}

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
	if err := json.Unmarshal([]byte(line), &entry, json.MatchCaseInsensitiveNames(true)); err != nil {
		return nil, err
	}
	return entry, nil
}
