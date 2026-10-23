package streamrelay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

func frames(t *testing.T, fs ...*ipc.StreamFrame) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	for _, f := range fs {
		if err := ipc.WriteRelayFrame(&buf, f); err != nil {
			t.Fatal(err)
		}
	}
	return &buf
}

func TestPumpFromStreamHandlesEveryOpcode(t *testing.T) {
	in := frames(t,
		&ipc.StreamFrame{Opcode: ipc.OpStreamData, StreamID: StreamID, Payload: []byte("a")},
		&ipc.StreamFrame{Opcode: ipc.OpStreamData, StreamID: StreamID},
		&ipc.StreamFrame{Opcode: ipc.OpStreamResize, StreamID: StreamID, Payload: []byte{0, 80, 0, 24}},
		&ipc.StreamFrame{Opcode: ipc.OpStreamResize, StreamID: StreamID, Payload: []byte{0, 80}},
		&ipc.StreamFrame{Opcode: ipc.OpStreamProgress, StreamID: StreamID},
		&ipc.StreamFrame{Opcode: ipc.OpStreamData, StreamID: StreamID, Payload: []byte("b")},
		&ipc.StreamFrame{Opcode: ipc.OpStreamAbort, StreamID: StreamID},
		&ipc.StreamFrame{Opcode: ipc.OpStreamData, StreamID: StreamID, Payload: []byte("never")},
	)
	var out bytes.Buffer
	var resizes [][2]uint16
	err := PumpFromStream(in, &out, func(cols, rows uint16) { resizes = append(resizes, [2]uint16{cols, rows}) })
	if err != nil {
		t.Fatalf("PumpFromStream: %v", err)
	}
	if out.String() != "ab" {
		t.Fatalf("data = %q, want ab", out.String())
	}
	if len(resizes) != 1 || resizes[0] != [2]uint16{80, 24} {
		t.Fatalf("resizes = %v, want one 80x24", resizes)
	}
}

func TestPumpFromStreamReportsReadFailure(t *testing.T) {
	if err := PumpFromStream(&bytes.Buffer{}, io.Discard, nil); !errors.Is(err, io.EOF) {
		t.Fatalf("err = %v, want EOF", err)
	}
}

func TestPumpToStreamDistinguishesWriteFailure(t *testing.T) {
	if err := PumpToStream(io.Discard, strings.NewReader("x"), 0); !errors.Is(err, io.EOF) || errors.Is(err, ErrStreamWrite) {
		t.Fatalf("read end: err = %v, want plain EOF", err)
	}
	failing := writerFunc(func([]byte) (int, error) { return 0, io.ErrClosedPipe })
	if err := PumpToStream(failing, strings.NewReader("x"), 0); !errors.Is(err, ErrStreamWrite) {
		t.Fatalf("write end: err = %v, want ErrStreamWrite", err)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func TestRunWritesCloseFrameWhenLocalEnds(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	var tornDown atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(context.Background(), server, Local{
			Reader:   strings.NewReader("hi"),
			Writer:   io.Discard,
			Teardown: func() { tornDown.Add(1) },
		})
	}()

	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	data, err := ipc.ReadRelayFrame(client)
	if err != nil || data.Opcode != ipc.OpStreamData || string(data.Payload) != "hi" {
		t.Fatalf("data frame = %+v, err %v", data, err)
	}
	closeFrame, err := ipc.ReadRelayFrame(client)
	if err != nil || closeFrame.Opcode != ipc.OpStreamClose {
		t.Fatalf("close frame = %+v, err %v", closeFrame, err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the local end finished")
	}
	if tornDown.Load() != 1 {
		t.Fatalf("Teardown ran %d times, want 1", tornDown.Load())
	}
}

func TestRunTearsDownWhenStreamCloses(t *testing.T) {
	server, client := net.Pipe()
	pr, pw := io.Pipe()
	var tornDown atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(context.Background(), server, Local{
			Reader: pr,
			Writer: io.Discard,
			Teardown: func() {
				tornDown.Add(1)
				_ = pw.Close()
			},
		})
	}()
	_ = client.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the stream closed")
	}
	if tornDown.Load() != 1 {
		t.Fatalf("Teardown ran %d times, want 1", tornDown.Load())
	}
}

func TestRunStopsWhenContextIsCanceled(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	pr, pw := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(ctx, server, Local{Reader: pr, Writer: io.Discard, Teardown: func() { _ = pw.Close() }})
	}()
	cancel()
	// Drain the close frame Run writes once the local end ends.
	go func() { _, _ = io.Copy(io.Discard, client) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context was canceled")
	}
}

func TestWriteErrorUsesBridgeCode(t *testing.T) {
	if got := ErrorCode(bridgeipc.NewError("bad", 400)); got != 400 {
		t.Fatalf("ErrorCode(bridge 400) = %d", got)
	}
	if got := ErrorCode(errors.New("boom")); got != 500 {
		t.Fatalf("ErrorCode(plain) = %d", got)
	}
	var buf bytes.Buffer
	if err := WriteError(&buf, 0, bridgeipc.NewError("bad", 404)); err != nil {
		t.Fatal(err)
	}
	result, err := ipc.ReadRelayFrame(&buf)
	if err != nil || result.Opcode != ipc.OpStreamResult {
		t.Fatalf("result frame = %+v, err %v", result, err)
	}
	closeFrame, err := ipc.ReadRelayFrame(&buf)
	if err != nil || closeFrame.Opcode != ipc.OpStreamClose {
		t.Fatalf("close frame = %+v, err %v", closeFrame, err)
	}
}

func TestWriteErrorUnlessCanceledJustClosesCanceledStreams(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteErrorUnlessCanceled(context.Background(), &buf, 0, context.Canceled); err != nil {
		t.Fatal(err)
	}
	frame, err := ipc.ReadRelayFrame(&buf)
	if err != nil || frame.Opcode != ipc.OpStreamClose {
		t.Fatalf("frame = %+v, err %v; want a bare close", frame, err)
	}
	if buf.Len() != 0 {
		t.Fatalf("%d extra bytes written", buf.Len())
	}
}
