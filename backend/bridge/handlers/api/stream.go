package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// HandleAPIStream handles a yamux stream for JSON API calls.
// This allows API calls to bypass HTTP and use the same stream infrastructure
// as terminal and file transfers.
//
// args format: [type, command, ...handlerArgs]
// - type: handler group (e.g., "system", "docker", "filebrowser")
// - command: handler command (e.g., "get_cpu_info", "list_containers")
// - handlerArgs: remaining args passed to the handler
//
// Response: OpStreamResult with JSON data, then OpStreamClose
func HandleAPIStream(stream net.Conn, args []string) error {
	logger.Debugf("[APIStream] Starting args=%v", args)

	// Validate args
	if len(args) < 2 {
		errMsg := "api stream requires at least [type, command]"
		logger.Warnf("[APIStream] %s, got: %v", errMsg, args)
		_ = ipc.WriteResultError(stream, 0, errMsg, 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New(errMsg)
	}

	handlerType := args[0]
	command := args[1]
	handlerArgs := args[2:]

	// Look up handler group
	group, found := handlers.HandlersByType[handlerType]
	if !found {
		errMsg := fmt.Sprintf("unknown handler type: %s", handlerType)
		logger.Warnf("[APIStream] %s", errMsg)
		_ = ipc.WriteResultError(stream, 0, errMsg, 404)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New(errMsg)
	}

	// Look up handler
	handler, ok := group[command]
	if !ok {
		errMsg := fmt.Sprintf("unknown command: %s/%s", handlerType, command)
		logger.Warnf("[APIStream] %s", errMsg)
		_ = ipc.WriteResultError(stream, 0, errMsg, 404)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New(errMsg)
	}

	// Execute handler
	// Note: We pass nil for RequestContext since API stream handlers
	// don't need streaming responses (they use the stream directly)
	result, err := handler(nil, handlerArgs)
	if err != nil {
		logger.Warnf("[APIStream] Handler error %s/%s: %v", handlerType, command, err)
		_ = ipc.WriteResultError(stream, 0, err.Error(), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return err
	}

	// Marshal result
	var data json.RawMessage
	if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			logger.Warnf("[APIStream] Marshal error: %v", err)
			_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("marshal error: %v", err), 500)
			_ = ipc.WriteStreamClose(stream, 0)
			return err
		}
		data = b
	}

	// Send result
	logger.Debugf("[APIStream] Success %s/%s, data len=%d", handlerType, command, len(data))
	_ = ipc.WriteResultFrame(stream, 0, &ipc.ResultFrame{
		Status: "ok",
		Data:   data,
	})
	_ = ipc.WriteStreamClose(stream, 0)

	return nil
}
