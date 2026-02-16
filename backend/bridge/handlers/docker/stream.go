package docker

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/docker/docker/api/types/container"
	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const (
	StreamTypeDockerLogs    = "docker-logs"
	StreamTypeDockerCompose = "docker-compose"
	StreamTypeDockerReindex = "docker-reindex"
)

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
		if err := ipc.WriteStreamClose(stream, 1); err != nil {
			logger.Debugf("[DockerLogs] failed to write stream close frame: %v", err)
		}
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
		if closeErr := ipc.WriteStreamClose(stream, 1); closeErr != nil {
			logger.Debugf("[DockerLogs] failed to write stream close frame: %v", closeErr)
		}
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
		if closeErr := ipc.WriteStreamClose(stream, 1); closeErr != nil {
			logger.Debugf("[DockerLogs] failed to write stream close frame: %v", closeErr)
		}
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
			if err := ipc.WriteStreamClose(stream, 1); err != nil {
				logger.Debugf("[DockerLogs] failed to write stream close frame: %v", err)
			}
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

	if err := ipc.WriteStreamClose(stream, 1); err != nil {
		logger.Debugf("[DockerLogs] failed to write stream close frame: %v", err)
	}
	return nil
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
		// Use 'up -d --remove-orphans' instead of 'restart' to apply compose file changes
		cmdArgs = []string{"compose", "-f", configFile, "-p", projectName, "up", "-d", "--remove-orphans"}
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
	if err := ipc.WriteStreamClose(stream, 1); err != nil {
		logger.Debugf("[DockerCompose] failed to write stream close frame: %v", err)
	}
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
	if err := ipc.WriteStreamClose(stream, 1); err != nil {
		logger.Debugf("[DockerCompose] failed to write stream close frame: %v", err)
	}
}

// HandleDockerReindexStream triggers a reindex of the user's docker folder and streams progress.
// Args: none - uses the docker folder from user config
func HandleDockerReindexStream(sess *session.Session, stream net.Conn, args []string) error {
	username := sess.User.Username

	cfg, _, err := config.Load(username)
	if err != nil {
		if writeErr := ipc.WriteResultErrorAndClose(stream, 0, "failed to load user config", 500); writeErr != nil {
			logger.Debugf("[DockerReindex] failed to write error+close frame: %v", writeErr)
		}
		return err
	}

	if cfg.Docker.Folder == "" {
		if writeErr := ipc.WriteResultErrorAndClose(stream, 0, "docker folder not configured", 400); writeErr != nil {
			logger.Debugf("[DockerReindex] failed to write error+close frame: %v", writeErr)
		}
		return errors.New("docker folder not configured")
	}

	dockerFolder := string(cfg.Docker.Folder)

	ctx, _, cleanup := ipc.AbortContext(context.Background(), stream)
	defer cleanup()

	cb := indexer.ReindexCallbacks{
		OnProgress: func(p indexer.ReindexProgress) error {
			return ipc.WriteProgress(stream, 0, p)
		},
		OnResult: func(r indexer.ReindexResult) error {
			if err := ipc.WriteResultOK(stream, 0, r); err != nil {
				return err
			}
			if err := ipc.WriteStreamClose(stream, 0); err != nil {
				return err
			}
			logger.Infof("[DockerReindex] Reindex complete: path=%s files=%d dirs=%d duration=%dms",
				r.Path, r.FilesIndexed, r.DirsIndexed, r.DurationMs)
			return nil
		},
		OnError: func(msg string, code int) error {
			if err := ipc.WriteResultError(stream, 0, msg, code); err != nil {
				return err
			}
			return ipc.WriteStreamClose(stream, 0)
		},
	}

	return indexer.StreamReindex(ctx, dockerFolder, cb)
}
