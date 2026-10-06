package docker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"testing"

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func dockerMuxFrame(streamType byte, payload string) []byte {
	frame := make([]byte, 8+len(payload))
	frame[0] = streamType
	binary.BigEndian.PutUint32(frame[4:8], uint32(len(payload)))
	copy(frame[8:], payload)
	return frame
}

func readDockerLogRecords(t *testing.T, out *bytes.Buffer) []dockerLogRecord {
	t.Helper()
	var records []dockerLogRecord
	reader := bytes.NewReader(out.Bytes())
	for reader.Len() > 0 {
		frame, err := relay.ReadRelayFrame(reader)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		if frame.Opcode != relay.OpStreamData {
			t.Fatalf("opcode = %d, want data", frame.Opcode)
		}
		for line := range bytes.SplitSeq(bytes.TrimRight(frame.Payload, "\n"), []byte("\n")) {
			var rec dockerLogRecord
			if err := json.Unmarshal(line, &rec); err != nil {
				t.Fatalf("decode %q: %v", line, err)
			}
			records = append(records, rec)
		}
	}
	return records
}

func assertDockerLogRecords(t *testing.T, got, want []dockerLogRecord) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("records = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("record %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestCopyContainerLogsDemuxesAndJoinsPartialLines(t *testing.T) {
	var out bytes.Buffer
	stdout := &dockerLogLineWriter{stream: &out, id: "abc123def456", name: "web"}
	stderr := &dockerLogLineWriter{stream: &out, id: "abc123def456", name: "web", stderr: true}
	var src bytes.Buffer
	src.Write(dockerMuxFrame(1, "2026-10-05T10:00:00.000000001Z hello \x1b[32mworld\x1b[0m\n2026-10-05T10:00:00.000000002Z par"))
	src.Write(dockerMuxFrame(1, "tial line\n"))
	src.Write(dockerMuxFrame(2, "2026-10-05T10:00:00.000000003Z oops\n"))
	src.Write(dockerMuxFrame(1, "2026-10-05T10:00:00.000000004Z no newline at end"))

	if err := copyContainerLogs(&src, false, stdout, stderr); err != nil {
		t.Fatalf("copy: %v", err)
	}

	assertDockerLogRecords(t, readDockerLogRecords(t, &out), []dockerLogRecord{
		{ID: "abc123def456", Name: "web", TS: "2026-10-05T10:00:00.000000001Z", Line: "hello world"},
		{ID: "abc123def456", Name: "web", TS: "2026-10-05T10:00:00.000000002Z", Line: "partial line"},
		{ID: "abc123def456", Name: "web", TS: "2026-10-05T10:00:00.000000003Z", Line: "oops", Stderr: true},
		{ID: "abc123def456", Name: "web", TS: "2026-10-05T10:00:00.000000004Z", Line: "no newline at end"},
	})
}

func TestCopyContainerLogsRawTTY(t *testing.T) {
	var out bytes.Buffer
	stdout := &dockerLogLineWriter{stream: &out, id: "abc123def456", name: "tty"}
	src := bytes.NewBufferString("2026-10-05T10:00:00Z \x1b[1mstarted\x1b[0m\r\nno timestamp here\r\n")

	if err := copyContainerLogs(src, true, stdout, nil); err != nil {
		t.Fatalf("copy: %v", err)
	}

	assertDockerLogRecords(t, readDockerLogRecords(t, &out), []dockerLogRecord{
		{ID: "abc123def456", Name: "tty", TS: "2026-10-05T10:00:00Z", Line: "started"},
		{ID: "abc123def456", Name: "tty", TS: "", Line: "no timestamp here"},
	})
}

// chunkedWriter splits every Write into small pieces and yields between them,
// so unsynchronised writers interleave the way a windowed yamux stream can.
type chunkedWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *chunkedWriter) Write(p []byte) (int, error) {
	for start := 0; start < len(p); start += 7 {
		end := min(start+7, len(p))
		w.mu.Lock()
		w.buf.Write(p[start:end])
		w.mu.Unlock()
		runtime.Gosched()
	}
	return len(p), nil
}

func TestDockerLogStreamSerialisesConcurrentWriters(t *testing.T) {
	sink := &chunkedWriter{}
	stream := &dockerLogStream{w: sink}
	var writers sync.WaitGroup
	for i, name := range []string{"web", "db", "cache"} {
		writer := &dockerLogLineWriter{stream: stream, id: fmt.Sprintf("%012d", i), name: name}
		writers.Go(func() {
			for n := range 200 {
				fmt.Fprintf(writer, "2026-10-05T10:00:00.%09dZ line %d\n", n, n)
			}
		})
	}
	writers.Wait()

	records := readDockerLogRecords(t, &sink.buf)
	if len(records) != 600 {
		t.Fatalf("records = %d, want 600", len(records))
	}
}

func TestAttachStartedContainersSkipsListedStartsAndStopsOnEOF(t *testing.T) {
	messages := make(chan events.Message)
	errs := make(chan error, 1)
	var followed []string
	follow := func(id, name string, sinceStart bool) {
		followed = append(followed, fmt.Sprintf("%s:%s:%t", id, name, sinceStart))
	}
	listed := map[string]struct{}{"listed": {}}
	done := make(chan error, 1)
	go func() {
		done <- attachStartedContainers(context.Background(), client.EventsResult{Messages: messages, Err: errs}, listed, 1_000, follow)
	}()

	web := map[string]string{"name": "web"}
	messages <- events.Message{Actor: events.Actor{ID: "listed", Attributes: web}, TimeNano: 900}
	messages <- events.Message{Actor: events.Actor{ID: "listed", Attributes: web}, TimeNano: 2_000}
	messages <- events.Message{Actor: events.Actor{ID: "fresh", Attributes: map[string]string{"name": "db"}}, TimeNano: 500}
	errs <- io.EOF

	if err := <-done; err != nil {
		t.Fatalf("attach: %v", err)
	}
	want := []string{"listed:web:true", "fresh:db:true"}
	if fmt.Sprint(followed) != fmt.Sprint(want) {
		t.Fatalf("followed = %v, want %v", followed, want)
	}
}

func TestAttachStartedContainersIgnoresErrorAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	errs := make(chan error, 1)
	errs <- ctx.Err()
	err := attachStartedContainers(ctx, client.EventsResult{Messages: make(chan events.Message), Err: errs}, nil, 0, func(string, string, bool) {})
	if err != nil {
		t.Fatalf("err = %v, want nil after cancel", err)
	}
}

func TestAttachStartedContainersReportsEventStreamFailure(t *testing.T) {
	errs := make(chan error, 1)
	errs <- errors.New("boom")
	err := attachStartedContainers(context.Background(), client.EventsResult{Messages: make(chan events.Message), Err: errs}, nil, 0, func(string, string, bool) {})
	if err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v, want boom", err)
	}
}
