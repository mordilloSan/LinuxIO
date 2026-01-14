package generic

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// contextKey is a custom type for context keys to avoid collisions
type contextKey string

const sessionContextKey contextKey = "session"

// HandleBridgeStream is the universal entry point for all bridge streams.
// It replaces both HandleJSONStream and individual stream handlers.
//
// Protocol: "bridge\0handlerType\0command\0arg1\0arg2..."
// Example: "bridge\0system\0get_drive_info"
func HandleBridgeStream(sess *session.Session, stream net.Conn, args []string) error {
	if len(args) < 2 {
		err := fmt.Errorf("invalid bridge args: expected [type, command, ...args], got %v", args)
		_ = ipc.WriteResultError(stream, 0, err.Error(), 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return err
	}

	handlerType := args[0]
	command := args[1]
	handlerArgs := args[2:]

	// Look up handler
	h, ok := ipc.Get(handlerType, command)
	if !ok {
		err := fmt.Errorf("handler not found: %s.%s", handlerType, command)
		_ = ipc.WriteResultError(stream, 0, err.Error(), 404)
		_ = ipc.WriteStreamClose(stream, 0)
		return err
	}

	// Create context with session for handlers that need runtime session access
	ctx := context.WithValue(context.Background(), sessionContextKey, sess)

	// Check if bidirectional
	if bidirHandler, ok := h.(ipc.BidirectionalHandler); ok {
		return handleBidirectional(ctx, stream, bidirHandler, handlerArgs)
	}

	// Standard unidirectional handler
	return handleUnidirectional(ctx, stream, h, handlerArgs)
}

func handleUnidirectional(ctx context.Context, stream net.Conn, h ipc.Handler, args []string) error {
	emit := newEventEmitter(stream)

	// Execute handler
	if err := h.Execute(ctx, args, emit); err != nil {
		// Handler returned error - send error result
		_ = emit.Error(err, 500)
		_ = emit.Close("handler error")
		return err
	}

	// Success - close stream
	_ = emit.Close("")
	return nil
}

func handleBidirectional(ctx context.Context, stream net.Conn, h ipc.BidirectionalHandler, args []string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	resizeChan := make(chan ipc.ResizeEvent, 1)
	ctx = ipc.WithResizeChannel(ctx, resizeChan)

	emit := newEventEmitter(stream)
	inputChan := make(chan []byte, 16)

	// Start goroutine to read client data
	go func() {
		defer close(resizeChan)
		defer close(inputChan)
		for {
			frame, err := ipc.ReadRelayFrame(stream)
			if err != nil {
				cancel()
				return
			}

			switch frame.Opcode {
			case ipc.OpStreamData:
				// Forward client data to handler
				select {
				case inputChan <- frame.Payload:
				case <-ctx.Done():
					return
				}
			case ipc.OpStreamClose, ipc.OpStreamAbort:
				// Client closed or aborted
				cancel()
				return
			case ipc.OpStreamResize:
				if len(frame.Payload) < 4 {
					continue
				}
				cols := binary.BigEndian.Uint16(frame.Payload[0:2])
				rows := binary.BigEndian.Uint16(frame.Payload[2:4])
				select {
				case resizeChan <- ipc.ResizeEvent{Cols: cols, Rows: rows}:
				default:
				}
			}
		}
	}()

	// Execute handler with input channel
	if err := h.ExecuteWithInput(ctx, args, emit, inputChan); err != nil {
		_ = emit.Error(err, 500)
		_ = emit.Close("handler error")
		return err
	}

	_ = emit.Close("")
	return nil
}

// eventEmitter implements ipc.Events
type eventEmitter struct {
	stream net.Conn
}

func newEventEmitter(stream net.Conn) *eventEmitter {
	return &eventEmitter{stream: stream}
}

func (e *eventEmitter) Data(chunk []byte) error {
	return ipc.WriteRelayFrame(e.stream, &ipc.StreamFrame{
		Opcode:  ipc.OpStreamData,
		Payload: chunk,
	})
}

func (e *eventEmitter) Progress(progress any) error {
	return ipc.WriteProgress(e.stream, 0, progress)
}

func (e *eventEmitter) Result(result any) error {
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal result: %w", err)
	}
	return ipc.WriteResultFrame(e.stream, 0, &ipc.ResultFrame{
		Status: "ok",
		Data:   data,
	})
}

func (e *eventEmitter) Error(err error, code int) error {
	return ipc.WriteResultError(e.stream, 0, err.Error(), code)
}

func (e *eventEmitter) Close(reason string) error {
	// Note: We could extend the protocol to include a reason in the close frame
	// For now, just send the close opcode
	return ipc.WriteStreamClose(e.stream, 0)
}
