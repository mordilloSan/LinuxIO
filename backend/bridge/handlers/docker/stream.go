package docker

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/docker/docker/api/types/container"
	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/config"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const (
	StreamTypeDockerLogs          = "docker-logs"
	StreamTypeDockerCompose       = "docker-compose"
	StreamTypeDockerIndexer       = "docker-indexer"
	StreamTypeDockerIndexerAttach = "docker-indexer-attach"
)

// RegisterStreamHandlers registers all docker stream handlers.
func RegisterStreamHandlers(handlers map[string]func(*session.Session, net.Conn, []string) error) {
	handlers[StreamTypeDockerLogs] = HandleDockerLogsStream
	handlers[StreamTypeDockerCompose] = HandleDockerComposeStream
	handlers[StreamTypeDockerIndexer] = HandleDockerIndexerStream
	handlers[StreamTypeDockerIndexerAttach] = HandleDockerIndexerAttachStream
}

// HandleDockerLogsStream streams container logs in real-time.
// Args: [containerID, tail] where tail is the number of lines to start with (default "100")
func HandleDockerLogsStream(sess *session.Session, stream net.Conn, args []string) error {
	if len(args) < 1 {
		logger.Errorf("missing containerID")
		if err := ipc.WriteStreamClose(stream, 1); err != nil {
			logger.Debugf("failed to write stream close frame: %v", err)
		}
		return errors.New("missing containerID")
	}

	containerID := args[0]
	tail := "100"
	if len(args) >= 2 && args[1] != "" {
		tail = args[1]
	}

	logger.Debugf("Starting stream for container=%s tail=%s", containerID, tail)

	cli, err := getClient()
	if err != nil {
		logger.Errorf("docker client error: %v", err)
		if closeErr := ipc.WriteStreamClose(stream, 1); closeErr != nil {
			logger.Debugf("failed to write stream close frame: %v", closeErr)
		}
		return err
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
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
		logger.Errorf("failed to get logs: %v", err)
		if closeErr := ipc.WriteStreamClose(stream, 1); closeErr != nil {
			logger.Debugf("failed to write stream close frame: %v", closeErr)
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
				logger.Debugf("failed to write stream close frame: %v", err)
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
			logger.Debugf("read header error: %v", err)
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
			logger.Debugf("read data error: %v", err)
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
		logger.Debugf("failed to write stream close frame: %v", err)
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
		logger.Errorf("missing required arguments")
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

	logger.Infof("action=%s project=%s composePath=%s", action, projectName, composePath)

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

	// Create context that can be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Monitor for client disconnect in background
	go func() {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil || frame.Opcode == ipc.OpStreamClose {
			logger.Debugf("client disconnected, cancelling command")
			cancel()
		}
	}()

	// Stream compose SDK events and serialize writes to avoid interleaving frames.
	var streamMu sync.Mutex
	emit := func(msgType, message string) {
		if strings.TrimSpace(message) == "" {
			return
		}
		streamMu.Lock()
		sendComposeMessage(stream, msgType, message)
		streamMu.Unlock()
	}

	var err error
	switch action {
	case "up":
		err = composeUpWithSDK(ctx, projectName, configFile, workingDir, false, emit)
	case "down":
		err = composeDownWithSDK(ctx, projectName, configFile, workingDir, false, emit)
	case "stop":
		err = composeStopWithSDK(ctx, projectName, configFile, workingDir, emit)
	case "restart":
		// Use up+remove-orphans semantics for restart so compose file changes are applied.
		err = composeUpWithSDK(ctx, projectName, configFile, workingDir, true, emit)
	default:
		sendComposeError(stream, "unsupported action: "+action)
		return errors.New("unsupported action")
	}

	if err != nil {
		if ctx.Err() == context.Canceled {
			emit("error", "operation cancelled")
		} else {
			sendComposeError(stream, "command failed: "+err.Error())
		}
		return err
	}

	// Send completion message
	streamMu.Lock()
	sendComposeMessage(stream, "complete", "operation completed successfully")
	if err := ipc.WriteStreamClose(stream, 1); err != nil {
		logger.Debugf("failed to write stream close frame: %v", err)
	}
	streamMu.Unlock()
	return nil
}

func sendComposeMessage(stream net.Conn, msgType, message string) {
	msg := ComposeStreamMessage{
		Type:    msgType,
		Message: message,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		logger.Errorf("failed to marshal message: %v", err)
		return
	}

	frame := &ipc.StreamFrame{
		Opcode:   ipc.OpStreamData,
		StreamID: 1,
		Payload:  data,
	}

	if err := ipc.WriteRelayFrame(stream, frame); err != nil {
		logger.Debugf("failed to write frame: %v", err)
	}
}

func sendComposeError(stream net.Conn, message string) {
	sendComposeMessage(stream, "error", message)
	if err := ipc.WriteStreamClose(stream, 1); err != nil {
		logger.Debugf("failed to write stream close frame: %v", err)
	}
}

// HandleDockerIndexerStream triggers indexing of the user's docker folder and streams progress.
// Args: none - uses the docker folder from user config
func HandleDockerIndexerStream(sess *session.Session, stream net.Conn, args []string) error {
	username := sess.User.Username

	cfg, _, err := config.Load(username)
	if err != nil {
		if writeErr := ipc.WriteResultErrorAndClose(stream, 0, "failed to load user config", 500); writeErr != nil {
			logger.Debugf("failed to write error+close frame: %v", writeErr)
		}
		return err
	}

	if cfg.Docker.Folder == "" {
		if writeErr := ipc.WriteResultErrorAndClose(stream, 0, "docker folder not configured", 400); writeErr != nil {
			logger.Debugf("failed to write error+close frame: %v", writeErr)
		}
		return errors.New("docker folder not configured")
	}

	dockerFolder := string(cfg.Docker.Folder)

	ctx, _, cleanup := ipc.AbortContext(context.Background(), stream)
	defer cleanup()

	cb := indexer.IndexerCallbacks{
		OnProgress: func(p indexer.IndexerProgress) error {
			return ipc.WriteProgress(stream, 0, p)
		},
		OnResult: func(r indexer.IndexerResult) error {
			if err := ipc.WriteResultOK(stream, 0, r); err != nil {
				return err
			}
			if err := ipc.WriteStreamClose(stream, 0); err != nil {
				return err
			}
			logger.Infof("Indexing complete: path=%s files=%d dirs=%d duration=%dms",
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

	return indexer.StreamIndexer(ctx, dockerFolder, cb)
}

// HandleDockerIndexerAttachStream attaches to an already-running indexer operation.
func HandleDockerIndexerAttachStream(sess *session.Session, stream net.Conn, args []string) error {
	ctx, _, cleanup := ipc.AbortContext(context.Background(), stream)
	defer cleanup()

	cb := indexer.IndexerCallbacks{
		OnProgress: func(p indexer.IndexerProgress) error {
			return ipc.WriteProgress(stream, 0, p)
		},
		OnResult: func(r indexer.IndexerResult) error {
			if err := ipc.WriteResultOK(stream, 0, r); err != nil {
				return err
			}
			if err := ipc.WriteStreamClose(stream, 0); err != nil {
				return err
			}
			logger.Infof("Attach complete: files=%d dirs=%d duration=%dms",
				r.FilesIndexed, r.DirsIndexed, r.DurationMs)
			return nil
		},
		OnError: func(msg string, code int) error {
			if err := ipc.WriteResultError(stream, 0, msg, code); err != nil {
				return err
			}
			return ipc.WriteStreamClose(stream, 0)
		},
	}

	return indexer.StreamIndexerAttach(ctx, cb)
}
