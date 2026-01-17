package docker

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const (
	StreamTypeDockerLogs    = "docker-logs"
	StreamTypeDockerCompose = "docker-compose"
	StreamTypeDockerReindex = "docker-reindex"
)

// indexerStreamClient is an HTTP client for SSE connections to the indexer.
// It has no timeout since SSE streams can run for a long time.
var indexerStreamClient = &http.Client{
	Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return dialer.DialContext(ctx, "unix", "/var/run/indexer.sock")
		},
	},
	// No timeout for SSE streams
}

// RegisterStreamHandlers registers all docker stream handlers.
func RegisterStreamHandlers(handlers map[string]func(*session.Session, net.Conn, []string) error) {
	handlers[StreamTypeDockerLogs] = HandleDockerLogsStream
	handlers[StreamTypeDockerCompose] = HandleDockerComposeStream
	handlers[StreamTypeDockerReindex] = HandleDockerReindexStream
}

// HandleDockerLogsStream streams container logs in real-time.
// Args: [containerID, tail] where tail is the number of lines to start with (default "100")
func HandleDockerLogsStream(sess *session.Session, stream net.Conn, args []string) error {
	if len(args) < 1 {
		logger.Errorf("[DockerLogs] missing containerID")
		sendStreamClose(stream)
		return errors.New("missing containerID")
	}

	containerID := args[0]
	tail := "100"
	if len(args) >= 2 && args[1] != "" {
		tail = args[1]
	}

	logger.Debugf("[DockerLogs] Starting stream for container=%s tail=%s", containerID, tail)

	cli, err := getClient()
	if err != nil {
		logger.Errorf("[DockerLogs] docker client error: %v", err)
		sendStreamClose(stream)
		return err
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("[DockerLogs] failed to close Docker client: %v", cerr)
		}
	}()

	// Create a context that we can cancel when the stream closes
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: false,
		Follow:     true,
		Tail:       tail,
	}

	reader, err := cli.ContainerLogs(ctx, containerID, options)
	if err != nil {
		logger.Errorf("[DockerLogs] failed to get logs: %v", err)
		sendStreamClose(stream)
		return err
	}
	defer reader.Close()

	// Monitor for client disconnect in background
	go func() {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil || frame.Opcode == ipc.OpStreamClose {
			cancel()
		}
	}()

	// ANSI escape code regex for stripping colors
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

	// Stream docker logs to client
	header := make([]byte, 8)
	for {
		// Check if context was cancelled
		select {
		case <-ctx.Done():
			sendStreamClose(stream)
			return nil
		default:
		}

		// Read docker log frame header (8 bytes: [STREAM][0,0,0][SIZE(4 bytes)])
		_, err := io.ReadFull(reader, header)
		if err != nil {
			if err == io.EOF || errors.Is(err, context.Canceled) {
				break
			}
			logger.Debugf("[DockerLogs] read header error: %v", err)
			break
		}

		size := int(binary.BigEndian.Uint32(header[4:]))
		if size == 0 {
			continue
		}

		// Read the actual log data
		data := make([]byte, size)
		_, err = io.ReadFull(reader, data)
		if err != nil {
			if err == io.EOF || errors.Is(err, context.Canceled) {
				break
			}
			logger.Debugf("[DockerLogs] read data error: %v", err)
			break
		}

		// Strip ANSI escape codes
		cleanData := ansiRegex.ReplaceAll(data, nil)

		// Send to client as stream data
		frame := &ipc.StreamFrame{
			Opcode:   ipc.OpStreamData,
			StreamID: 1,
			Payload:  cleanData,
		}
		if err := ipc.WriteRelayFrame(stream, frame); err != nil {
			break
		}
	}

	sendStreamClose(stream)
	return nil
}

func sendStreamClose(stream net.Conn) {
	frame := &ipc.StreamFrame{
		Opcode:   ipc.OpStreamClose,
		StreamID: 1,
	}
	_ = ipc.WriteRelayFrame(stream, frame)
}

// ComposeStreamMessage represents a message sent during compose streaming
type ComposeStreamMessage struct {
	Type    string `json:"type"`    // "stdout", "stderr", "error", "complete"
	Message string `json:"message"` // The actual message content
	Code    int    `json:"code,omitempty"`
}

// HandleDockerComposeStream streams docker compose command output in real-time.
// Args: [action, projectName, composePath (optional)]
// action can be: "up", "down", "stop", "restart"
func HandleDockerComposeStream(sess *session.Session, stream net.Conn, args []string) error {
	if len(args) < 2 {
		logger.Errorf("[DockerCompose] missing required arguments")
		sendComposeError(stream, "missing required arguments: action, projectName")
		return errors.New("missing required arguments")
	}

	action := args[0]
	projectName := args[1]
	username := sess.User.Username
	var composePath string
	if len(args) >= 3 {
		composePath = args[2]
	}

	logger.Debugf("[DockerCompose] action=%s project=%s composePath=%s", action, projectName, composePath)

	// Determine config file and working directory
	var configFile string
	var workingDir string

	if composePath != "" {
		configFile = composePath
		workingDir = filepath.Dir(composePath)
	} else {
		// Try to find the compose file
		var err error
		configFile, workingDir, err = findComposeFile(username, projectName)
		if err != nil {
			sendComposeError(stream, "compose file not found: "+err.Error())
			return err
		}
	}

	// Build docker compose command based on action
	var cmdArgs []string
	switch action {
	case "up":
		cmdArgs = []string{"compose", "-f", configFile, "-p", projectName, "up", "-d"}
	case "down":
		cmdArgs = []string{"compose", "-f", configFile, "-p", projectName, "down"}
	case "stop":
		cmdArgs = []string{"compose", "-f", configFile, "-p", projectName, "stop"}
	case "restart":
		cmdArgs = []string{"compose", "-f", configFile, "-p", projectName, "restart"}
	default:
		sendComposeError(stream, "unsupported action: "+action)
		return errors.New("unsupported action")
	}

	// Create context that can be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Monitor for client disconnect in background
	go func() {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil || frame.Opcode == ipc.OpStreamClose {
			logger.Debugf("[DockerCompose] client disconnected, cancelling command")
			cancel()
		}
	}()

	// Execute docker compose command
	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	cmd.Dir = workingDir

	// Get stdout and stderr pipes
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		sendComposeError(stream, "failed to create stdout pipe: "+err.Error())
		return err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		sendComposeError(stream, "failed to create stderr pipe: "+err.Error())
		return err
	}

	// Start the command
	if err = cmd.Start(); err != nil {
		sendComposeError(stream, "failed to start command: "+err.Error())
		return err
	}

	// ANSI escape code regex for stripping colors
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

	// Stream stdout in a goroutine
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			// Strip ANSI codes
			cleanLine := ansiRegex.ReplaceAllString(line, "")
			sendComposeMessage(stream, "stdout", cleanLine)
		}
	}()

	// Stream stderr in a goroutine
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			// Strip ANSI codes
			cleanLine := ansiRegex.ReplaceAllString(line, "")
			sendComposeMessage(stream, "stderr", cleanLine)
		}
	}()

	// Wait for command to complete
	err = cmd.Wait()
	if err != nil {
		if ctx.Err() == context.Canceled {
			sendComposeMessage(stream, "error", "operation cancelled")
		} else {
			sendComposeError(stream, "command failed: "+err.Error())
		}
		return err
	}

	// Send completion message
	sendComposeMessage(stream, "complete", "operation completed successfully")
	sendStreamClose(stream)
	return nil
}

func sendComposeMessage(stream net.Conn, msgType, message string) {
	msg := ComposeStreamMessage{
		Type:    msgType,
		Message: message,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		logger.Errorf("[DockerCompose] failed to marshal message: %v", err)
		return
	}

	frame := &ipc.StreamFrame{
		Opcode:   ipc.OpStreamData,
		StreamID: 1,
		Payload:  data,
	}

	if err := ipc.WriteRelayFrame(stream, frame); err != nil {
		logger.Debugf("[DockerCompose] failed to write frame: %v", err)
	}
}

func sendComposeError(stream net.Conn, message string) {
	sendComposeMessage(stream, "error", message)
	sendStreamClose(stream)
}

// ReindexProgress represents progress for docker folder reindex operations
type ReindexProgress struct {
	FilesIndexed int64  `json:"files_indexed"`
	DirsIndexed  int64  `json:"dirs_indexed"`
	CurrentPath  string `json:"current_path,omitempty"`
	Phase        string `json:"phase,omitempty"`
}

// ReindexResult represents the final result of a reindex operation
type ReindexResult struct {
	Path         string `json:"path"`
	FilesIndexed int64  `json:"files_indexed"`
	DirsIndexed  int64  `json:"dirs_indexed"`
	DurationMs   int64  `json:"duration_ms"`
}

// HandleDockerReindexStream triggers a reindex of the user's docker folder and streams progress.
// Args: none - uses the docker folder from user config
func HandleDockerReindexStream(sess *session.Session, stream net.Conn, args []string) error {
	username := sess.User.Username

	// Get the user's docker folder from config
	cfg, _, err := config.Load(username)
	if err != nil {
		_ = ipc.WriteResultError(stream, 0, "failed to load user config", 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return err
	}

	if cfg.Docker.Folder == "" {
		_ = ipc.WriteResultError(stream, 0, "docker folder not configured", 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New("docker folder not configured")
	}

	dockerFolder := string(cfg.Docker.Folder)

	// Set up abort monitoring
	cancelFn, cleanup := ipc.AbortMonitor(stream)
	defer cleanup()

	// Create a cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Monitor for abort in background
	go func() {
		for {
			if cancelFn() {
				cancel()
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()

	// Build request to indexer's SSE endpoint
	reindexURL := "http://unix/reindex/stream?path=" + url.QueryEscape(dockerFolder)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reindexURL, nil)
	if err != nil {
		_ = ipc.WriteResultError(stream, 0, "failed to create request", 500)
		_ = ipc.WriteStreamClose(stream, 0)
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	// Send initial progress
	_ = ipc.WriteProgress(stream, 0, ReindexProgress{
		Phase: "connecting",
	})

	// Make request to indexer
	resp, err := indexerStreamClient.Do(req)
	if err != nil {
		if ctx.Err() == context.Canceled {
			logger.Infof("[DockerReindex] Reindex aborted: %s", dockerFolder)
			_ = ipc.WriteResultError(stream, 0, "operation aborted", 499)
			_ = ipc.WriteStreamClose(stream, 0)
			return errors.New("reindex aborted")
		}
		_ = ipc.WriteResultError(stream, 0, "indexer connection failed", 503)
		_ = ipc.WriteStreamClose(stream, 0)
		return err
	}
	defer resp.Body.Close()

	// Check for conflict (another operation running)
	if resp.StatusCode == http.StatusConflict {
		_ = ipc.WriteResultError(stream, 0, "another index operation is already running", 409)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New("indexer conflict")
	}

	// Check for bad request
	if resp.StatusCode == http.StatusBadRequest {
		_ = ipc.WriteResultError(stream, 0, "invalid path", 400)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New("invalid path")
	}

	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		_ = ipc.WriteResultError(stream, 0, "indexer error: "+resp.Status, resp.StatusCode)
		_ = ipc.WriteStreamClose(stream, 0)
		return errors.New("indexer error")
	}

	// Read SSE events
	reader := bufio.NewReader(resp.Body)
	var currentEvent string

	for {
		// Check for cancellation
		if cancelFn() {
			logger.Infof("[DockerReindex] Reindex aborted: %s", dockerFolder)
			_ = ipc.WriteResultError(stream, 0, "operation aborted", 499)
			_ = ipc.WriteStreamClose(stream, 0)
			return errors.New("reindex aborted")
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			if ctx.Err() == context.Canceled {
				logger.Infof("[DockerReindex] Reindex aborted: %s", dockerFolder)
				_ = ipc.WriteResultError(stream, 0, "operation aborted", 499)
				_ = ipc.WriteStreamClose(stream, 0)
				return errors.New("reindex aborted")
			}
			_ = ipc.WriteResultError(stream, 0, "read error", 500)
			_ = ipc.WriteStreamClose(stream, 0)
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse SSE format
		if strings.HasPrefix(line, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			continue
		}

		if strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))

			switch currentEvent {
			case "started":
				_ = ipc.WriteProgress(stream, 0, ReindexProgress{
					Phase: "indexing",
				})

			case "progress":
				var progress ReindexProgress
				if err := json.Unmarshal([]byte(data), &progress); err == nil {
					progress.Phase = "indexing"
					_ = ipc.WriteProgress(stream, 0, progress)
				}

			case "complete":
				var result ReindexResult
				if err := json.Unmarshal([]byte(data), &result); err == nil {
					_ = ipc.WriteResultOK(stream, 0, result)
					_ = ipc.WriteStreamClose(stream, 0)
					logger.Infof("[DockerReindex] Reindex complete: path=%s files=%d dirs=%d duration=%dms",
						result.Path, result.FilesIndexed, result.DirsIndexed, result.DurationMs)
					return nil
				}

			case "error":
				var errData struct {
					Message string `json:"message"`
				}
				if err := json.Unmarshal([]byte(data), &errData); err == nil {
					_ = ipc.WriteResultError(stream, 0, errData.Message, 500)
					_ = ipc.WriteStreamClose(stream, 0)
					return errors.New("indexer error: " + errData.Message)
				}
			}
		}
	}

	// If we got here without a complete event, something went wrong
	_ = ipc.WriteResultError(stream, 0, "indexer stream ended unexpectedly", 500)
	_ = ipc.WriteStreamClose(stream, 0)
	return errors.New("indexer stream ended unexpectedly")
}
