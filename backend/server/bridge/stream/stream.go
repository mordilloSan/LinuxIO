package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
	"github.com/mordilloSan/LinuxIO/backend/server/web"
)

type progressPayload struct {
	Percent        float64 `json:"percent"`
	BytesProcessed int64   `json:"bytesProcessed"`
	TotalBytes     int64   `json:"totalBytes"`
}

// CallWithProgress invokes a bridge command that may emit streaming progress
// frames. Any ipc.MsgTypeStream frames are forwarded to GlobalProgressBroadcaster
// using the provided progressKey. The final JSON frame is decoded into result.
// If ctx is non-nil, it can be used to cancel the stream.
func CallWithProgress(ctx context.Context, sess *session.Session, subsystem, command string, args []string, progressKey string, result interface{}) error {
	if ctx == nil {
		ctx = context.Background()
	}
	stream, err := bridge.StreamWithSession(sess, subsystem, command, args)
	if err != nil {
		return err
	}
	defer stream.Close()

	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = stream.Close()
		case <-done:
		}
	}()
	defer close(done)

	var finalResp *ipc.Response
	for {
		resp, msgType, readErr := stream.Read()
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
			return fmt.Errorf("stream read failed: %w", readErr)
		}

		switch msgType {
		case ipc.MsgTypeStream:
			if progressKey == "" || len(resp.Output) == 0 {
				continue
			}
			var payload progressPayload
			if err := json.Unmarshal(resp.Output, &payload); err != nil {
				logger.Debugf("invalid %s progress payload: %v", command, err)
				continue
			}
			web.GlobalProgressBroadcaster.Send(progressKey, web.ProgressUpdate{
				Type:           resp.Status,
				Percent:        payload.Percent,
				BytesProcessed: payload.BytesProcessed,
				TotalBytes:     payload.TotalBytes,
			})
		case ipc.MsgTypeJSON:
			finalResp = resp
			goto DONE
		default:
			logger.Warnf("unexpected frame type from bridge for %s: 0x%02x", command, msgType)
		}
	}

DONE:
	if finalResp == nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		return fmt.Errorf("bridge error: empty response")
	}
	if !strings.EqualFold(finalResp.Status, "ok") {
		if finalResp.Error == "" {
			return fmt.Errorf("bridge error: unknown")
		}
		return fmt.Errorf("bridge error: %s", finalResp.Error)
	}
	if result == nil {
		return nil
	}
	if len(finalResp.Output) == 0 {
		return ipc.ErrEmptyBridgeOutput
	}
	if err := json.Unmarshal(finalResp.Output, result); err != nil {
		return fmt.Errorf("decode bridge output: %w", err)
	}
	return nil
}
