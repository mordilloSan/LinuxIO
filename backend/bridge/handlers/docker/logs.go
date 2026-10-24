package docker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"regexp"

	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dockerclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/streamrelay"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

const routeDockerLogsFollow = "docker.logs.follow"

var dockerLogANSIRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// streamDockerLogsChannel streams container logs through a direct channel.
func streamDockerLogsChannel(parent context.Context, stream net.Conn, _ runtime.Runtime, req apischema.DockerLogsFollowRequest) error {
	ctx, cleanup := bridgeipc.ReceiveOnlyChannelContext(parent, stream)
	defer cleanup()
	if req.ContainerID == "" {
		return streamrelay.WriteError(stream, 0, bridgeipc.NewError("missing containerID", 400))
	}
	tail := "100"
	if req.Tail != nil && *req.Tail != "" {
		tail = *req.Tail
	}
	slog.Debug("starting docker log channel", "component", "docker", "route", routeDockerLogsFollow, "container", req.ContainerID, "mode", tail)

	cli, err := dockerclient.Get()
	if err != nil {
		return streamrelay.WriteErrorUnlessCanceled(ctx, stream, 0, fmt.Errorf("get docker client for container %s logs: %w", req.ContainerID, err))
	}
	defer dockerclient.Release(cli)

	// TTY containers return raw bytes instead of the stdout/stderr multiplexed stream.
	inspect, err := cli.ContainerInspect(ctx, req.ContainerID, client.ContainerInspectOptions{})
	if err != nil {
		return streamrelay.WriteErrorUnlessCanceled(ctx, stream, 0, fmt.Errorf("inspect container %s for logs: %w", req.ContainerID, err))
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
		return streamrelay.WriteErrorUnlessCanceled(ctx, stream, 0, fmt.Errorf("get container %s logs: %w", req.ContainerID, err))
	}
	defer reader.Close()

	if err := streamDockerLogs(ctx, stream, reader, tty); err != nil {
		return streamrelay.WriteErrorUnlessCanceled(ctx, stream, 0, fmt.Errorf("container %s log stream: %w", req.ContainerID, err))
	}
	return relay.WriteResultOKAndClose(stream, 0, map[string]any{"status": "stopped"})
}

// streamDockerLogs relays one container's logs as data frames with ANSI
// escapes stripped.
func streamDockerLogs(ctx context.Context, stream net.Conn, reader io.Reader, tty bool) error {
	out := dockerLogFrameWriter{stream: stream}
	err := demuxContainerLogs(reader, tty, out, out)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		slog.Debug("docker log follow ended with read error", "component", "docker", "route", routeDockerLogsFollow, "error", err)
	}
	return err
}

// demuxContainerLogs copies one container's log stream into stdout and stderr.
// TTY containers return raw bytes; the others are stdout/stderr multiplexed.
func demuxContainerLogs(reader io.Reader, tty bool, stdout, stderr io.Writer) error {
	if tty {
		_, err := io.Copy(stdout, reader)
		return err
	}
	_, err := stdcopy.StdCopy(stdout, stderr, reader)
	return err
}

// dockerLogFrameWriter sends every Write as one data frame, minus ANSI escapes.
// shortcut: an ANSI sequence split across two writes is not stripped, buffer the tail if that shows up.
type dockerLogFrameWriter struct{ stream net.Conn }

func (w dockerLogFrameWriter) Write(p []byte) (int, error) {
	payload := dockerLogANSIRegex.ReplaceAll(p, nil)
	if len(payload) == 0 {
		return len(p), nil
	}
	return len(p), relay.WriteRelayFrame(w.stream, &relay.StreamFrame{Opcode: relay.OpStreamData, StreamID: 0, Payload: payload})
}
