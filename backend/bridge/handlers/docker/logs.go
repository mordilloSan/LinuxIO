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

	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

const routeDockerLogsFollow = "docker.logs.follow"

var dockerLogANSIRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// streamDockerLogsChannel streams container logs through a direct channel.
func streamDockerLogsChannel(parent context.Context, stream net.Conn, _ runtime.Runtime, req apischema.DockerLogsFollowRequest) error {
	ctx, cleanup := bridgeipc.ReceiveOnlyChannelContext(parent, stream)
	defer cleanup()
	if req.ContainerID == "" {
		err := bridgeipc.NewError("missing containerID", 400)
		slog.Error("invalid docker logs request", "component", "docker", "route", routeDockerLogsFollow, "error", err)
		return writeDockerLogError(stream, err)
	}
	tail := "100"
	if req.Tail != nil && *req.Tail != "" {
		tail = *req.Tail
	}
	slog.Debug("starting docker log channel", "component", "docker", "route", routeDockerLogsFollow, "container", req.ContainerID, "mode", tail)

	cli, err := getClient()
	if err != nil {
		return writeDockerLogErrorUnlessCanceled(ctx, stream, err, "failed to get docker client", "route", routeDockerLogsFollow, "container", req.ContainerID)
	}
	defer releaseClient(cli)

	// TTY containers return raw bytes instead of the stdout/stderr multiplexed stream.
	inspect, err := cli.ContainerInspect(ctx, req.ContainerID, client.ContainerInspectOptions{})
	if err != nil {
		return writeDockerLogErrorUnlessCanceled(ctx, stream, err, "failed to inspect container for logs", "route", routeDockerLogsFollow, "container", req.ContainerID)
	}
	tty := inspect.Container.Config != nil && inspect.Container.Config.Tty

	options := client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: false,
		Follow:     true,
		Tail:       tail,
	}

	reader, err := cli.ContainerLogs(ctx, req.ContainerID, options)
	if err != nil {
		return writeDockerLogErrorUnlessCanceled(ctx, stream, err, "failed to get container logs", "route", routeDockerLogsFollow, "container", req.ContainerID)
	}
	defer reader.Close()

	if err := streamDockerLogs(ctx, stream, reader, tty); err != nil {
		return writeDockerLogErrorUnlessCanceled(ctx, stream, err, "container log stream failed", "route", routeDockerLogsFollow, "container", req.ContainerID)
	}
	return relay.WriteResultOKAndClose(stream, 0, map[string]any{"status": "stopped"})
}

func streamDockerLogs(ctx context.Context, stream net.Conn, reader io.Reader, tty bool) error {
	readFrame := readDockerLogFrame
	buf := make([]byte, 8)
	if tty {
		readFrame = readDockerRawLogChunk
		buf = make([]byte, 32*1024)
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		payload, done, err := readFrame(reader, buf)
		if err != nil {
			slog.Debug("docker log follow ended with read error", "component", "docker", "route", routeDockerLogsFollow, "error", err)
			return err
		}
		if done {
			break
		}
		if len(payload) == 0 {
			continue
		}
		if err := relay.WriteRelayFrame(stream, &relay.StreamFrame{Opcode: relay.OpStreamData, StreamID: 0, Payload: payload}); err != nil {
			return err
		}
	}

	return nil
}

func writeDockerLogError(stream net.Conn, err error) error {
	code := 500
	var bridgeErr *bridgeipc.Error
	if errors.As(err, &bridgeErr) && bridgeErr.Code != 0 {
		code = bridgeErr.Code
	}
	return relay.WriteResultErrorAndClose(stream, 0, err.Error(), code)
}

// writeDockerLogErrorUnlessCanceled reports err to the Channel and the log. A
// Channel the client already closed is not a failure: it is logged at debug
// and the stream is just closed.
func writeDockerLogErrorUnlessCanceled(ctx context.Context, stream net.Conn, err error, msg string, attrs ...any) error {
	attrs = append([]any{"component", "docker"}, attrs...)
	attrs = append(attrs, "error", err)
	if errors.Is(err, context.Canceled) || ctx.Err() != nil {
		slog.Debug(msg+" (channel closed)", attrs...)
		return relay.WriteStreamClose(stream, 0)
	}
	slog.Error(msg, attrs...)
	return writeDockerLogError(stream, err)
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

// readDockerRawLogChunk reads the unframed log stream Docker returns for TTY containers.
// ponytail: an ANSI sequence split across two chunks is not stripped; buffer the tail if that shows up.
func readDockerRawLogChunk(reader io.Reader, buf []byte) ([]byte, bool, error) {
	n, err := reader.Read(buf)
	if n > 0 {
		return dockerLogANSIRegex.ReplaceAll(buf[:n], nil), false, nil
	}
	if err == nil {
		return nil, false, nil
	}
	if err == io.EOF || errors.Is(err, context.Canceled) {
		return nil, true, nil
	}
	return nil, false, fmt.Errorf("read data: %w", err)
}
