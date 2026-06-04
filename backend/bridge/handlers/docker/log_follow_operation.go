package docker

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"

	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routeDockerLogsFollow = RouteLogsFollow.Route

var dockerLogANSIRegex = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// runDockerLogsJob streams container logs through the bridge job lifecycle.
func runDockerLogsJob(ctx context.Context, _ runtime.Runtime, job *bridgeipc.Job, req apischema.DockerLogsFollowRequest) (any, error) {
	if req.ContainerID == "" {
		err := bridgeipc.NewError("missing containerID", 400)
		slog.Error("invalid docker logs job request", "component", "docker", "route", routeDockerLogsFollow, "job_id", job.ID(), "error", err)
		return nil, err
	}
	tail := "100"
	if req.Tail != nil && *req.Tail != "" {
		tail = *req.Tail
	}
	slog.Debug("starting docker log job", "component", "docker", "route", routeDockerLogsFollow, "job_id", job.ID(), "container", req.ContainerID, "mode", tail)

	cli, err := getClient()
	if err != nil {
		slog.Error("failed to get docker client", "component", "docker", "route", routeDockerLogsFollow, "job_id", job.ID(), "container", req.ContainerID, "error", err)
		return nil, err
	}
	defer releaseClient(cli)

	options := client.ContainerLogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Timestamps: false,
		Follow:     true,
		Tail:       tail,
	}

	reader, err := cli.ContainerLogs(ctx, req.ContainerID, options)
	if err != nil {
		slog.Error("failed to get container logs", "component", "docker", "route", routeDockerLogsFollow, "job_id", job.ID(), "container", req.ContainerID, "error", err)
		return nil, err
	}
	defer reader.Close()

	if err := streamDockerLogs(ctx, job, reader); err != nil {
		return nil, err
	}
	return map[string]any{"status": "stopped"}, nil
}

func streamDockerLogs(ctx context.Context, job *bridgeipc.Job, reader io.Reader) error {
	header := make([]byte, 8)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		payload, done, err := readDockerLogFrame(reader, header)
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
		job.ReportData(string(payload))
	}

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
