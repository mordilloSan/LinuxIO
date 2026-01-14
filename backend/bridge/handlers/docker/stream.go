package docker

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"regexp"

	"github.com/docker/docker/api/types/container"
	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const StreamTypeDockerLogs = "docker-logs"

// RegisterStreamHandlers registers all docker stream handlers.
func RegisterStreamHandlers(handlers map[string]func(*session.Session, net.Conn, []string) error) {
	handlers[StreamTypeDockerLogs] = HandleDockerLogsStream
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
