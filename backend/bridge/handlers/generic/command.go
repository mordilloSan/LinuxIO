package generic

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"time"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// StreamTypeExec is the stream type for executing commands with real-time output streaming.
const StreamTypeExec = "exec"

// StreamTypeJSON is the stream type for JSON response calls over yamux.
const StreamTypeJSON = "json"

// RegisterStreamHandlers registers all generic stream handlers.
func RegisterStreamHandlers(handlers map[string]func(*session.Session, net.Conn, []string) error, jsonHandlers map[string]map[string]func([]string) (any, error)) {
	handlers[StreamTypeExec] = HandleExecStream
	handlers[StreamTypeJSON] = func(sess *session.Session, conn net.Conn, args []string) error {
		return HandleJSONStream(sess, conn, args, jsonHandlers)
	}
}

func CommandHandlers() map[string]func([]string) (any, error) {
	return map[string]func([]string) (any, error){
		// NOTE: Direct command execution is DISABLED for security
		// Commands must be defined in module YAML files
		// Use ExecCommandDirect() from module loader instead
		"exec": disabledExecHandler,
	}
}

// disabledExecHandler returns an error explaining that direct execution is disabled
func disabledExecHandler(args []string) (any, error) {
	return nil, fmt.Errorf("direct command execution is disabled - commands must be defined in module YAML files")
}

// ExecCommandDirect executes a command directly (used by module loader)
// This bypasses security checks and should only be called by whitelisted module handlers
func ExecCommandDirect(command, timeoutStr string) (any, error) {
	timeout := 10 * time.Second

	if timeoutStr != "" {
		if t, err := strconv.Atoi(timeoutStr); err == nil {
			timeout = time.Duration(t) * time.Second
		}
	}

	// Execute with timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	output, err := cmd.CombinedOutput()

	if err != nil {
		exitCode := -1
		if cmd.ProcessState != nil {
			exitCode = cmd.ProcessState.ExitCode()
		}
		return map[string]interface{}{
			"exitCode": exitCode,
			"stdout":   string(output),
			"error":    err.Error(),
		}, nil
	}

	result := string(output)

	// Try to parse as JSON if it looks like JSON
	var jsonResult interface{}
	if err := json.Unmarshal([]byte(result), &jsonResult); err == nil {
		return jsonResult, nil
	}

	// Return as plain string
	return map[string]interface{}{
		"exitCode": 0,
		"stdout":   result,
	}, nil
}

// HandleExecStream handles streaming command execution.
// args format: [command, arg1, arg2, ...]
// - Executes the command with arguments
// - Streams stdout/stderr as OpStreamData frames (raw bytes)
// - Sends OpStreamResult with exit code when done
func HandleExecStream(sess *session.Session, stream net.Conn, args []string) error {
	logger.Debugf("[ExecStream] Starting with %d args", len(args))

	if len(args) == 0 {
		_ = ipc.WriteResultError(stream, 0, "no command specified", 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return fmt.Errorf("no command specified")
	}

	cmdString := args[0]
	cmdArgs := args[1:]

	logger.Infof("[ExecStream] Executing: %s %v", cmdString, cmdArgs)

	// Create command with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, cmdString, cmdArgs...)

	// Get stdout and stderr pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("failed to get stdout pipe: %v", err), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("failed to get stderr pipe: %v", err), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return err
	}

	// Start command
	if err = cmd.Start(); err != nil {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("failed to start command: %v", err), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return err
	}

	// ANSI escape code remover
	ansiRE := regexp.MustCompile(`\x1B\[[0-9;]*[A-Za-z]`)

	// Stream stdout and stderr to client as raw data
	done := make(chan struct{})
	streamOutput := func(r io.Reader, prefix string) {
		defer func() { done <- struct{}{} }()

		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()

			// Remove ANSI escape codes
			cleanLine := ansiRE.ReplaceAllString(line, "")

			// Log to server
			logger.Infof("[ExecStream] %s%s", prefix, cleanLine)

			// Send to client as raw data (with newline)
			payload := []byte(cleanLine + "\n")
			_ = ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
				Opcode:   ipc.OpStreamData,
				StreamID: 0,
				Payload:  payload,
			})
		}
	}

	// Stream both stdout and stderr
	go streamOutput(stdout, "[stdout] ")
	go streamOutput(stderr, "[stderr] ")

	// Wait for both streams to finish
	go func() { <-done }()
	go func() { <-done }()

	// Wait for command to complete
	err = cmd.Wait()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
		logger.Warnf("[ExecStream] Command failed: %v (exit code: %d)", err, exitCode)
	} else {
		logger.Infof("[ExecStream] Command completed successfully")
	}

	// Send result with exit code
	if exitCode == 0 {
		_ = ipc.WriteResultOK(stream, 0, map[string]interface{}{
			"exit_code": exitCode,
		})
	} else {
		_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("command exited with code %d", exitCode), exitCode)
	}

	_ = ipc.WriteStreamClose(stream, 0)
	return nil
}

// HandleJSONStream handles a yamux stream for JSON response calls.
// These are handler functions that return JSON-serializable data.
//
// args format: [type, command, ...handlerArgs]
// - type: handler group (e.g., "system", "docker", "filebrowser")
// - command: handler command (e.g., "get_cpu_info", "list_containers")
// - handlerArgs: remaining args passed to the handler
//
// Response: OpStreamResult with JSON data, then OpStreamClose
func HandleJSONStream(sess *session.Session, stream net.Conn, args []string, handlersByType map[string]map[string]func([]string) (any, error)) error {
	logger.Debugf("[JSONStream] Starting args=%v", args)

	// Validate args
	if len(args) < 2 {
		errMsg := "json stream requires at least [type, command]"
		logger.Warnf("[JSONStream] %s, got: %v", errMsg, args)
		_ = ipc.WriteResultError(stream, 0, errMsg, 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New(errMsg)
	}

	handlerType := args[0]
	command := args[1]
	handlerArgs := args[2:]

	// Look up handler group
	group, found := handlersByType[handlerType]
	if !found {
		errMsg := fmt.Sprintf("unknown handler type: %s", handlerType)
		logger.Warnf("[JSONStream] %s", errMsg)
		_ = ipc.WriteResultError(stream, 0, errMsg, 404)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New(errMsg)
	}

	// Look up handler
	handler, ok := group[command]
	if !ok {
		errMsg := fmt.Sprintf("unknown command: %s/%s", handlerType, command)
		logger.Warnf("[JSONStream] %s", errMsg)
		_ = ipc.WriteResultError(stream, 0, errMsg, 404)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New(errMsg)
	}

	// Execute handler
	result, err := handler(handlerArgs)
	if err != nil {
		logger.Warnf("[JSONStream] Handler error %s/%s: %v", handlerType, command, err)
		_ = ipc.WriteResultError(stream, 0, err.Error(), 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return err
	}

	// Marshal result
	var data json.RawMessage
	if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			logger.Warnf("[JSONStream] Marshal error: %v", err)
			_ = ipc.WriteResultError(stream, 0, fmt.Sprintf("marshal error: %v", err), 500)
			_ = ipc.WriteStreamClose(stream, 0)
			return err
		}
		data = b
	}

	// Send result
	logger.Debugf("[JSONStream] Success %s/%s, data len=%d", handlerType, command, len(data))
	_ = ipc.WriteResultFrame(stream, 0, &ipc.ResultFrame{
		Status: "ok",
		Data:   data,
	})
	_ = ipc.WriteStreamClose(stream, 0)

	return nil
}
