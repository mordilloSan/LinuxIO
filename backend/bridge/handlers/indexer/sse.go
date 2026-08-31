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

// SSEDecoder reads one event at a time. It is used by request handlers so the
// request context owns the only reader and no channel-producing goroutine can
// outlive a callback that stops consuming events.
type SSEDecoder struct {
	ctx         context.Context
	scanner     *bufio.Scanner
	currentType string
	dataParts   []string
	dataBytes   int
}

func NewSSEDecoder(ctx context.Context, r io.Reader) *SSEDecoder {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), maxSSELineBytes)
	return &SSEDecoder{ctx: ctx, scanner: scanner}
}

func (d *SSEDecoder) flushEvent() (SSEEvent, bool) {
	if len(d.dataParts) == 0 && d.currentType == "" {
		return SSEEvent{}, false
	}
	event := SSEEvent{Type: d.currentType, Data: strings.Join(d.dataParts, "\n")}
	d.currentType = ""
	d.dataParts = d.dataParts[:0]
	d.dataBytes = 0
	return event, true
}

func (d *SSEDecoder) appendDataLine(line string) error {
	part := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	nextBytes := d.dataBytes + len(part)
	if len(d.dataParts) > 0 {
		nextBytes++
	}
	if nextBytes > maxSSEEventDataBytes {
		return fmt.Errorf("SSE event data exceeds %d bytes", maxSSEEventDataBytes)
	}
	d.dataParts = append(d.dataParts, part)
	d.dataBytes = nextBytes
	return nil
}

func (d *SSEDecoder) consumeLine(line string) (SSEEvent, bool, error) {
	if strings.HasPrefix(line, ":") {
		return SSEEvent{}, false, nil
	}
	if line == "" {
		event, ok := d.flushEvent()
		return event, ok, nil
	}
	if after, ok := strings.CutPrefix(line, "event:"); ok {
		d.currentType = strings.TrimSpace(after)
		return SSEEvent{}, false, nil
	}
	if strings.HasPrefix(line, "data:") {
		return SSEEvent{}, false, d.appendDataLine(line)
	}
	return SSEEvent{}, false, nil
}

// Next returns the next complete event, io.EOF after the final partial event,
// or the underlying parse/read error.
func (d *SSEDecoder) Next() (SSEEvent, error) {
	for d.scanner.Scan() {
		if err := d.ctx.Err(); err != nil {
			return SSEEvent{}, err
		}
		event, complete, err := d.consumeLine(d.scanner.Text())
		if err != nil {
			return SSEEvent{}, err
		}
		if complete {
			return event, nil
		}
	}
	if err := d.ctx.Err(); err != nil {
		return SSEEvent{}, err
	}
	if err := d.scanner.Err(); err != nil {
		return SSEEvent{}, err
	}
	if event, ok := d.flushEvent(); ok {
		return event, nil
	}
	return SSEEvent{}, io.EOF
}
