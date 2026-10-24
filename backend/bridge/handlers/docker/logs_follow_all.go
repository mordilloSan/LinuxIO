package docker

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dockerclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/streamrelay"
	"github.com/mordilloSan/LinuxIO/backend/common/dockerupdate"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

const routeDockerLogsFollowAll = "docker.logs.follow_all"

// maxDockerLogLineBytes caps the carried partial line; a longer line without a
// newline is emitted as several records.
const maxDockerLogLineBytes = 1 << 20

// dockerLogRecord is one NDJSON line on the merged log Channel.
type dockerLogRecord struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	TS     string `json:"ts"`
	Line   string `json:"line"`
	Stderr bool   `json:"stderr,omitzero"`
}

// dockerLogStream is the shared sink for every follower goroutine of one
// Channel. A go-yamux stream write is not atomic under backpressure, so
// unsynchronised writers could interleave the bytes of two relay frames.
type dockerLogStream struct {
	mu sync.Mutex
	w  io.Writer
}

func (s *dockerLogStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}

// dockerLogLineWriter turns one container's log bytes into NDJSON records.
// Every Write emits the whole lines it completes as one relay frame; a
// trailing partial line is carried into the next Write and flushed by Close.
type dockerLogLineWriter struct {
	stream  io.Writer
	id      string
	name    string
	stderr  bool
	partial []byte
}

func (w *dockerLogLineWriter) Write(p []byte) (int, error) {
	w.partial = append(w.partial, p...)
	data := w.partial
	w.partial = nil
	var frame bytes.Buffer
	for {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		w.appendRecord(&frame, data[:idx])
		data = data[idx+1:]
	}
	for len(data) > maxDockerLogLineBytes {
		// Cut on a rune boundary so a multi-byte rune is not split across records.
		cut := maxDockerLogLineBytes
		for cut > maxDockerLogLineBytes-utf8.UTFMax && !utf8.RuneStart(data[cut]) {
			cut--
		}
		w.appendRecord(&frame, data[:cut])
		data = data[cut:]
	}
	if len(data) > 0 {
		w.partial = append([]byte(nil), data...)
	}
	return len(p), w.writeFrame(&frame)
}

// Close flushes a final line that had no trailing newline.
func (w *dockerLogLineWriter) Close() error {
	if len(w.partial) == 0 {
		return nil
	}
	var frame bytes.Buffer
	w.appendRecord(&frame, w.partial)
	w.partial = nil
	return w.writeFrame(&frame)
}

func (w *dockerLogLineWriter) writeFrame(frame *bytes.Buffer) error {
	if frame.Len() == 0 {
		return nil
	}
	return relay.WriteRelayFrame(w.stream, &relay.StreamFrame{Opcode: relay.OpStreamData, StreamID: 0, Payload: frame.Bytes()})
}

func (w *dockerLogLineWriter) appendRecord(frame *bytes.Buffer, raw []byte) {
	// ANSI colours are kept: the logs page renders them.
	line := strings.TrimRight(string(raw), "\r")
	// Docker prefixes each line with an RFC3339Nano timestamp when Timestamps is set.
	ts, text, _ := strings.Cut(line, " ")
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		ts, text = "", line
	}
	encoded, err := json.Marshal(dockerLogRecord{ID: w.id, Name: w.name, TS: ts, Line: text, Stderr: w.stderr}, jsontext.AllowInvalidUTF8(true))
	if err != nil {
		return
	}
	frame.Write(encoded)
	frame.WriteByte('\n')
}

// copyContainerLogs drains one container's log stream into the line writers
// and flushes their trailing partial lines.
func copyContainerLogs(reader io.Reader, tty bool, stdout, stderr *dockerLogLineWriter) error {
	err := demuxContainerLogs(reader, tty, stdout, stderr)
	if closeErr := stdout.Close(); err == nil {
		err = closeErr
	}
	if stderr != nil {
		if closeErr := stderr.Close(); err == nil {
			err = closeErr
		}
	}
	return err
}

// streamDockerLogsFollowAllChannel merges the logs of every running container
// into one Channel and attaches containers that start while it is open.
func streamDockerLogsFollowAllChannel(parent context.Context, stream net.Conn, _ runtime.Runtime, req apischema.DockerLogsFollowAllRequest) error {
	ctx, cleanup := bridgeipc.ReceiveOnlyChannelContext(parent, stream)
	defer cleanup()
	tail := "100"
	if req.Tail != nil && *req.Tail != "" {
		tail = *req.Tail
	}
	slog.Debug("starting merged docker log channel", "component", "docker", "route", routeDockerLogsFollowAll, "mode", tail)

	cli, err := dockerclient.Get()
	if err != nil {
		return streamrelay.WriteErrorUnlessCanceled(ctx, stream, 0, fmt.Errorf("get docker client for merged logs: %w", err))
	}
	defer dockerclient.Release(cli)

	// Subscribe before listing so a container that starts in between is not missed.
	events := cli.Events(ctx, client.EventsListOptions{
		Filters: make(client.Filters).Add("type", "container").Add("event", "start"),
	})
	containers, err := cli.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return streamrelay.WriteErrorUnlessCanceled(ctx, stream, 0, fmt.Errorf("list containers for merged logs: %w", err))
	}

	listedAt := time.Now().UnixNano()
	listed := make(map[string]struct{}, len(containers.Items))
	out := &dockerLogStream{w: stream}
	var followers sync.WaitGroup
	follow := func(id, name string, sinceStart bool) {
		followers.Go(func() {
			followContainerLogs(ctx, cli, out, id, name, tail, sinceStart)
		})
	}
	for _, c := range containers.Items {
		listed[c.ID] = struct{}{}
		follow(c.ID, dockerupdate.PrimaryContainerName(c), false)
	}

	eventsErr := attachStartedContainers(ctx, events, listed, listedAt, follow)
	followers.Wait()
	if eventsErr != nil {
		return streamrelay.WriteErrorUnlessCanceled(ctx, stream, 0, fmt.Errorf("docker event stream: %w", eventsErr))
	}
	if ctx.Err() != nil {
		return relay.WriteStreamClose(stream, 0)
	}
	return relay.WriteResultOKAndClose(stream, 0, map[string]any{"status": "stopped"})
}

// attachStartedContainers follows every container that starts until the
// Channel ends or the event stream fails. A start that happened before the
// initial list returned is already followed through the list, so it is skipped.
func attachStartedContainers(ctx context.Context, events client.EventsResult, listed map[string]struct{}, listedAtNano int64, follow func(id, name string, sinceStart bool)) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-events.Messages:
			if !ok {
				return nil
			}
			if _, ok := listed[msg.Actor.ID]; ok && msg.TimeNano <= listedAtNano {
				continue
			}
			follow(msg.Actor.ID, msg.Actor.Attributes["name"], true)
		case err, ok := <-events.Err:
			if ok && err != nil && !errors.Is(err, io.EOF) && ctx.Err() == nil {
				return err
			}
			return nil
		}
	}
}

// followContainerLogs streams one container until it stops or the Channel ends.
// Failures are logged, not returned: one container must not end the merged view.
func followContainerLogs(ctx context.Context, cli *client.Client, stream io.Writer, id, name, tail string, sinceStart bool) {
	inspect, err := cli.ContainerInspect(ctx, id, client.ContainerInspectOptions{})
	if err != nil {
		slog.Debug("skipping container logs", "component", "docker", "route", routeDockerLogsFollowAll, "container", id, "error", err)
		return
	}
	tty := inspect.Container.Config != nil && inspect.Container.Config.Tty
	options := client.ContainerLogsOptions{ShowStdout: true, ShowStderr: true, Timestamps: true, Follow: true, Tail: tail}
	if sinceStart {
		// Everything from this run's start: the daemon emits the start event
		// after the process is already writing, and a restarted container must
		// not replay its previous run.
		options.Tail = "0"
		if inspect.Container.State != nil && inspect.Container.State.StartedAt != "" {
			options.Tail = "all"
			options.Since = inspect.Container.State.StartedAt
		}
	}
	reader, err := cli.ContainerLogs(ctx, id, options)
	if err != nil {
		slog.Debug("skipping container logs", "component", "docker", "route", routeDockerLogsFollowAll, "container", id, "error", err)
		return
	}
	defer reader.Close()
	shortID := id
	if len(shortID) > 12 {
		shortID = shortID[:12]
	}
	stdout := &dockerLogLineWriter{stream: stream, id: shortID, name: name}
	stderr := &dockerLogLineWriter{stream: stream, id: shortID, name: name, stderr: true}
	if err := copyContainerLogs(reader, tty, stdout, stderr); err != nil && ctx.Err() == nil {
		slog.Debug("container log follow ended", "component", "docker", "route", routeDockerLogsFollowAll, "container", id, "error", err)
	}
}
