package indexer

import (
	"bufio"
	"context"
	"io"
	"strings"
)

// SSEEvent represents a single Server-Sent Event.
type SSEEvent struct {
	Type string // From the "event:" field; empty if not set.
	Data string // From "data:" field(s), joined by "\n" for multiline data.
}

// ReadSSE reads Server-Sent Events from r and delivers them on the returned channel.
//
// Events are dispatched when an empty line is encountered (per the SSE spec).
// Consecutive "data:" lines are joined with newlines.
// Comment lines (starting with ":") are silently skipped.
//
// Error contract:
//   - EOF is a clean close: the events channel is closed, nothing is sent on errCh.
//   - Any non-EOF read error is sent as a single value on errCh (buffered, size 1).
//   - If ctx is cancelled the goroutine exits without reporting an error.
//   - Both channels are always closed when the goroutine exits.
func ReadSSE(ctx context.Context, r io.Reader) (<-chan SSEEvent, <-chan error) {
	events := make(chan SSEEvent, 4)
	errCh := make(chan error, 1)

	go func() {
		defer close(events)
		defer close(errCh)

		scanner := bufio.NewScanner(r)
		// Increase token limit above bufio.Scanner's 64 KiB default for larger JSON lines.
		scanner.Buffer(make([]byte, 64*1024), 256*1024)
		var currentType string
		var dataParts []string

		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			currentType, dataParts = processSSELine(scanner.Text(), currentType, dataParts, events, ctx)
			if ctx.Err() != nil {
				return
			}
		}

		if err := scanner.Err(); err != nil {
			if ctx.Err() != nil {
				return
			}
			errCh <- err
			return
		}

		flushSSEEvent(currentType, dataParts, events, ctx)
	}()

	return events, errCh
}

func processSSELine(
	line, currentType string,
	dataParts []string,
	events chan<- SSEEvent,
	ctx context.Context,
) (string, []string) {
	if strings.HasPrefix(line, ":") {
		return currentType, dataParts
	}
	if line == "" {
		flushSSEEvent(currentType, dataParts, events, ctx)
		return "", dataParts[:0]
	}
	if after, ok := strings.CutPrefix(line, "event:"); ok {
		return strings.TrimSpace(after), dataParts
	}
	if after, ok := strings.CutPrefix(line, "data:"); ok {
		return currentType, append(dataParts, strings.TrimSpace(after))
	}
	return currentType, dataParts
}

func flushSSEEvent(currentType string, dataParts []string, events chan<- SSEEvent, ctx context.Context) {
	if len(dataParts) == 0 && currentType == "" {
		return
	}
	evt := SSEEvent{
		Type: currentType,
		Data: strings.Join(dataParts, "\n"),
	}
	select {
	case events <- evt:
	case <-ctx.Done():
	}
}
