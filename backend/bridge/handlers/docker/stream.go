package docker

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"regexp"

	"github.com/docker/docker/api/types/container"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

const StreamTypeDockerLogs = "docker-logs"

var dockerLogANSIRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// HandleDockerLogsStream streams container logs in real time.
// Args: [containerID, tail] where tail is the number of lines to start with (default "100")
func HandleDockerLogsStream(_ *session.Session, stream net.Conn, args []string) error {
	containerID, tail, err := parseDockerLogsArgs(args)
	if err != nil {
		slog.Error("invalid docker logs stream args", "component", "docker", "stream_type", StreamTypeDockerLogs, "error", err)
		writeDockerLogsClose(stream)
		return err
	}
	slog.Debug("starting docker log stream", "component", "docker", "stream_type", StreamTypeDockerLogs, "container", containerID, "mode", tail)

	cli, err := getClient()
	if err != nil {
		slog.Error("failed to get docker client", "component", "docker", "stream_type", StreamTypeDockerLogs, "container", containerID, "error", err)
		writeDockerLogsClose(stream)
		return err
	}
	defer releaseClient(cli)

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
		slog.Error("failed to get container logs", "component", "docker", "stream_type", StreamTypeDockerLogs, "container", containerID, "error", err)
		writeDockerLogsClose(stream)
		return err
	}
	defer reader.Close()

	monitorDockerLogDisconnect(stream, cancel)
	return streamDockerLogs(ctx, stream, reader)
}

func parseDockerLogsArgs(args []string) (string, string, error) {
	if len(args) < 1 {
		return "", "", errors.New("missing containerID")
	}

	tail := "100"
	if len(args) >= 2 && args[1] != "" {
		tail = args[1]
	}
	return args[0], tail, nil
}

func monitorDockerLogDisconnect(stream net.Conn, cancel context.CancelFunc) {
	go func() {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil || frame.Opcode == ipc.OpStreamClose {
			cancel()
		}
	}()
}

func streamDockerLogs(ctx context.Context, stream net.Conn, reader io.Reader) error {
	header := make([]byte, 8)
	for {
		if ctx.Err() != nil {
			writeDockerLogsClose(stream)
			return nil
		}

		payload, done, err := readDockerLogFrame(reader, header)
		if err != nil {
			slog.Debug("docker log stream ended with read error", "component", "docker", "stream_type", StreamTypeDockerLogs, "error", err)
			break
		}
		if done {
			break
		}
		if len(payload) == 0 {
			continue
		}
		if err := ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
			Opcode:   ipc.OpStreamData,
			StreamID: 1,
			Payload:  payload,
		}); err != nil {
			break
		}
	}

	writeDockerLogsClose(stream)
	return nil
}

func readDockerLogFrame(reader io.Reader, header []byte) ([]byte, bool, error) {
	if _, err := io.ReadFull(reader, header); err != nil {
		if err == io.EOF || errors.Is(err, context.Canceled) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("read header: %w", err)
	}

	size := int(binary.BigEndian.Uint32(header[4:]))
	if size == 0 {
		return nil, false, nil
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(reader, data); err != nil {
		if err == io.EOF || errors.Is(err, context.Canceled) {
			return nil, true, nil
		}
		return nil, false, fmt.Errorf("read data: %w", err)
	}

	return dockerLogANSIRegex.ReplaceAll(data, nil), false, nil
}

func writeDockerLogsClose(stream net.Conn) {
	if err := ipc.WriteStreamClose(stream, 1); err != nil {
		slog.Debug("failed to write stream close frame", "component", "docker", "error", err)
	}
}
