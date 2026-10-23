package terminal

import (
	"context"
	"fmt"
	"log/slog"
	"net"

	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dockerclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/streamrelay"
)

// HandleContainerTerminalSession handles a yamux stream for container terminal I/O.
func HandleContainerTerminalSession(parent context.Context, rt runtime.Runtime, stream net.Conn, req apischema.ContainerOpenRequest) error {
	sess := rt.Session
	if req.ContainerID == "" || req.Shell == "" {
		err := fmt.Errorf("missing containerID or shell")
		slog.Error("invalid container terminal request", "error", err)
		streamrelay.WriteClose(stream)
		return err
	}
	cols, rows := initialSize(req.Cols, req.Rows)
	slog.Debug("starting container terminal stream", "container", req.ContainerID, "shell", req.Shell, "user", sess.User.Username)

	ctx, cancel := context.WithCancel(parent)
	defer cancel()

	cli, err := dockerclient.Get()
	if err != nil {
		slog.Error("container terminal docker client error", "error", err)
		streamrelay.WriteClose(stream)
		return err
	}
	defer dockerclient.Release(cli)

	execResp, err := createContainerExec(ctx, cli, req.ContainerID, req.Shell, cols, rows)
	if err != nil {
		slog.Error("container terminal exec create failed", "container", req.ContainerID, "shell", req.Shell, "error", err)
		streamrelay.WriteClose(stream)
		return err
	}

	attachResp, err := attachContainerExec(ctx, cli, execResp.ID, cols, rows)
	if err != nil {
		slog.Error("container terminal exec attach failed", "container", req.ContainerID, "exec_id", execResp.ID, "error", err)
		streamrelay.WriteClose(stream)
		return err
	}
	defer attachResp.Close()

	resizeContainerExec(ctx, cli, execResp.ID, cols, rows)

	streamrelay.Run(ctx, stream, streamrelay.Local{
		Reader:  attachResp.Reader,
		Writer:  attachResp.Conn,
		BufSize: ptyBufSize,
		Resize: func(cols, rows uint16) {
			resizeContainerExec(ctx, cli, execResp.ID, int(cols), int(rows))
		},
		Teardown: func() {
			cancel()
			attachResp.Close()
		},
	})
	return nil
}

func createContainerExec(ctx context.Context, cli *client.Client, containerID, shell string, cols, rows int) (client.ExecCreateResult, error) {
	consoleSize := client.ConsoleSize{Height: uint(rows), Width: uint(cols)}
	return cli.ExecCreate(ctx, containerID, client.ExecCreateOptions{
		TTY:          true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Env:          []string{"TERM=xterm-256color"},
		ConsoleSize:  consoleSize,
		Cmd:          containerShellArgs(shell),
	})
}

func attachContainerExec(ctx context.Context, cli *client.Client, execID string, cols, rows int) (client.HijackedResponse, error) {
	consoleSize := client.ConsoleSize{Height: uint(rows), Width: uint(cols)}
	result, err := cli.ExecAttach(ctx, execID, client.ExecAttachOptions{
		TTY:         true,
		ConsoleSize: consoleSize,
	})
	return result.HijackedResponse, err
}

func containerShellArgs(shell string) []string {
	if shell == "bash" {
		return []string{"bash", "-il"}
	}
	return []string{shell, "-i"}
}

func resizeContainerExec(ctx context.Context, cli *client.Client, execID string, cols, rows int) {
	if _, err := cli.ExecResize(ctx, execID, client.ExecResizeOptions{
		Width:  uint(cols),
		Height: uint(rows),
	}); err != nil {
		slog.Debug("failed to resize container exec tty", "exec_id", execID, "cols", cols, "rows", rows, "error", err)
	}
}
