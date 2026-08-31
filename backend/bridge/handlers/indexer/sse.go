package indexer

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

const (
	maxSSELineBytes      = 256 * 1024
	maxSSEEventDataBytes = 1 << 20
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
		scanner.Buffer(make([]byte, 64*1024), maxSSELineBytes)
		var currentType string
		var dataParts []string
		var dataBytes int

		for scanner.Scan() {
			if ctx.Err() != nil {
				return
			}
			var err error
			currentType, dataParts, dataBytes, err = processSSELine(scanner.Text(), currentType, dataParts, dataBytes, events, ctx)
			if err != nil {
				errCh <- err
				return
			}
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
	dataBytes int,
	events chan<- SSEEvent,
	ctx context.Context,
) (string, []string, int, error) {
	if strings.HasPrefix(line, ":") {
		return currentType, dataParts, dataBytes, nil
	}
	if line == "" {
		flushSSEEvent(currentType, dataParts, events, ctx)
		return "", dataParts[:0], 0, nil
	}
	if after, ok := strings.CutPrefix(line, "event:"); ok {
		return strings.TrimSpace(after), dataParts, dataBytes, nil
	}
	if after, ok := strings.CutPrefix(line, "data:"); ok {
		part := strings.TrimSpace(after)
		nextBytes := dataBytes + len(part)
		if len(dataParts) > 0 {
			nextBytes++
		}
		if nextBytes > maxSSEEventDataBytes {
			return currentType, dataParts, dataBytes, fmt.Errorf("SSE event data exceeds %d bytes", maxSSEEventDataBytes)
		}
		return currentType, append(dataParts, part), nextBytes, nil
	}
	return currentType, dataParts, dataBytes, nil
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
