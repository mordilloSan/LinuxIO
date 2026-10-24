// Package streamrelay relays a yamux duplex stream to a local connection
// (PTY, VNC socket, docker exec, agent stdio) and writes stream-level errors.
// Callers keep what is specific to them: starting the local end and tearing it
// down.
package streamrelay

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sync"

	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

// StreamID is the id carried by every frame of a duplex session stream.
const StreamID uint32 = 1

const defaultBufSize = 32 * 1024

// ErrStreamWrite marks a PumpToStream failure caused by the stream, not by the
// local reader.
var ErrStreamWrite = errors.New("write relay frame")

// WriteData sends payload as one data frame.
func WriteData(stream io.Writer, payload []byte) error {
	return ipc.WriteRelayFrame(stream, &ipc.StreamFrame{Opcode: ipc.OpStreamData, StreamID: StreamID, Payload: payload})
}

// WriteClose sends a close frame; a stream that is already gone is not an error.
func WriteClose(stream io.Writer) {
	if err := ipc.WriteStreamClose(stream, StreamID); err != nil {
		slog.Debug("failed to write stream close frame", "error", err)
	}
}

// PumpToStream copies src to the stream as data frames until src fails or the
// stream write fails. It returns the read error (io.EOF included), or an error
// wrapping ErrStreamWrite. It never writes a close frame.
func PumpToStream(stream io.Writer, src io.Reader, bufSize int) error {
	if bufSize <= 0 {
		bufSize = defaultBufSize
	}
	buf := make([]byte, bufSize)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if werr := WriteData(stream, buf[:n]); werr != nil {
				return fmt.Errorf("%w: %w", ErrStreamWrite, werr)
			}
		}
		if err != nil {
			return err
		}
	}
}

// PumpFromStream reads relay frames from the stream, writing data payloads to
// dst and passing resize frames ([cols:2][rows:2]) to resize when it is not
// nil. A close or abort frame returns nil; a read or write failure returns the
// error. Unknown opcodes are ignored.
func PumpFromStream(stream io.Reader, dst io.Writer, resize func(cols, rows uint16)) error {
	for {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil {
			return err
		}
		switch frame.Opcode {
		case ipc.OpStreamData:
			if len(frame.Payload) > 0 {
				if _, err := dst.Write(frame.Payload); err != nil {
					return err
				}
			}
		case ipc.OpStreamResize:
			if resize != nil && len(frame.Payload) >= 4 {
				resize(binary.BigEndian.Uint16(frame.Payload[0:2]), binary.BigEndian.Uint16(frame.Payload[2:4]))
			}
		case ipc.OpStreamClose, ipc.OpStreamAbort:
			return nil
		}
	}
}

// Local is the local end of a relay.
type Local struct {
	Reader  io.Reader               // local output, relayed to the stream
	Writer  io.Writer               // receives the stream's data frames
	Resize  func(cols, rows uint16) // optional
	BufSize int                     // Reader buffer size; 0 means 32 KiB
	// Teardown stops the local end and must unblock Reader and Writer. Run
	// calls it once, and then closes the stream.
	Teardown func()
}

// Run relays between the stream and local until either direction ends or ctx
// is done, then tears both down and waits for both directions to return. When
// the local end finishes on its own (EOF or read error) a close frame is
// written first.
func Run(ctx context.Context, stream net.Conn, local Local) {
	var once sync.Once
	teardown := func() {
		once.Do(func() {
			if local.Teardown != nil {
				local.Teardown()
			}
			if err := stream.Close(); err != nil {
				slog.Debug("failed to close relay stream", "error", err)
			}
		})
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			teardown()
		case <-done:
		}
	}()

	var wg sync.WaitGroup
	wg.Go(func() {
		defer teardown()
		err := PumpToStream(stream, local.Reader, local.BufSize)
		if !errors.Is(err, ErrStreamWrite) {
			WriteClose(stream)
		}
		slog.Debug("local output relay ended", "error", err)
	})
	wg.Go(func() {
		defer teardown()
		err := PumpFromStream(stream, local.Writer, local.Resize)
		slog.Debug("stream input relay ended", "error", err)
	})
	wg.Wait()
}
