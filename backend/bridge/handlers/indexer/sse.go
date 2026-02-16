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
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := scanner.Text()

			// SSE comment
			if strings.HasPrefix(line, ":") {
				continue
			}

			// Empty line dispatches the accumulated event
			if line == "" {
				if len(dataParts) > 0 || currentType != "" {
					evt := SSEEvent{
						Type: currentType,
						Data: strings.Join(dataParts, "\n"),
					}
					select {
					case events <- evt:
					case <-ctx.Done():
						return
					}
					currentType = ""
					dataParts = dataParts[:0]
				}
				continue
			}

			if after, ok := strings.CutPrefix(line, "event:"); ok {
				currentType = strings.TrimSpace(after)
				continue
			}
			if after, ok := strings.CutPrefix(line, "data:"); ok {
				dataParts = append(dataParts, strings.TrimSpace(after))
				continue
			}
			// Other fields (id:, retry:) are ignored.
		}

		if err := scanner.Err(); err != nil {
			if ctx.Err() != nil {
				return
			}
			errCh <- err
			return
		}

		// EOF with un-dispatched data — flush it.
		if len(dataParts) > 0 || currentType != "" {
			evt := SSEEvent{
				Type: currentType,
				Data: strings.Join(dataParts, "\n"),
			}
			select {
			case events <- evt:
			case <-ctx.Done():
			}
		}
	}()

	return events, errCh
}
