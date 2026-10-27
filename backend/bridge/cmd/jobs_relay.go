package cmd

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/jobsclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/jobslauncher"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// Seams for tests.
var (
	jobsLauncherSocket = jobslauncher.LauncherSocketPath
	jobsHostSocket     = jobslauncher.HostSocketPath
	jobsConnect        = jobsclient.Connect
	effectiveUID       = func() uint32 { return uint32(os.Geteuid()) }
)

const (
	jobsRetryMin = time.Second
	jobsRetryMax = 30 * time.Second
)

var (
	errJobHostConnecting = errors.New("job host connection not ready")
	errJobHostGone       = errors.New("job host connection closed")
)

// relayForwarder runs the bridge's Mutation streams on the job host.
type relayForwarder struct {
	client func() (*jobsclient.Client, error)
}

// Forward relays stream to a host stream opened with the original open
// payload. Without a host it answers the client itself, so the client never
// sees a bare close.
func (f relayForwarder) Forward(ctx context.Context, stream net.Conn, open relay.StreamOpenEnvelope, raw []byte) error {
	c, err := f.client()
	var host net.Conn
	if err == nil {
		host, err = c.Open(ctx, raw)
	}
	if err != nil {
		slog.Warn("mutation not forwarded to the job host", "route", open.Route, "error", err)
		msg, code := "job host unavailable", 503
		if errors.Is(err, jobsclient.ErrUpdatePending) {
			msg, code = "update_pending: LinuxIO update waits for running jobs", 409
		}
		_ = relay.WriteResultErrorAndClose(stream, 0, msg, code)
		return nil
	}
	return c.Relay(ctx, stream, host)
}

// jobsRelay keeps a bridge session connected to its job host, reconnecting
// in the background, and serves Mutation streams and tasks.* for host jobs
// over that connection.
type jobsRelay struct {
	relayForwarder
	cancel context.CancelFunc
	done   chan struct{}

	mu     sync.Mutex
	client *jobsclient.Client
	err    error
	// changed is closed and replaced on every change of client.
	changed chan struct{}
}

// newJobsRelay starts connecting sess to its job host until ctx ends or
// Close is called. The host is the one of the bridge's own uid, which the
// host checks: a privileged bridge runs as root and shares the root host
// with every admin. The session's user is only the job owner it reports.
func newJobsRelay(ctx context.Context, sess *session.Session) *jobsRelay {
	ctx, cancel := context.WithCancel(ctx)
	j := &jobsRelay{cancel: cancel, done: make(chan struct{}), err: errJobHostConnecting, changed: make(chan struct{})}
	j.relayForwarder.client = j.current
	hello := bridgeipc.HelloSession{
		Username:   sess.User.Username,
		UID:        sess.User.UID,
		Privileged: sess.Privileged,
		SessionRef: session.DiagnosticRef(sess.SessionID),
	}
	go j.run(ctx, effectiveUID(), hello)
	return j
}

// run connects, waits for the connection to end, and reconnects, backing
// off from 1 s to 30 s while connecting fails.
func (j *jobsRelay) run(ctx context.Context, uid uint32, hello bridgeipc.HelloSession) {
	defer close(j.done)
	retry := jobsRetryMin
	for {
		c, err := jobsConnect(ctx, uid, hello, jobsLauncherSocket, jobsHostSocket(uid))
		if ctx.Err() != nil {
			if c != nil {
				_ = c.Close()
			}
			return
		}
		if err == nil {
			retry = jobsRetryMin
			j.set(c, nil)
			select {
			case <-c.Done():
				j.set(nil, errJobHostGone)
				slog.Warn("job host connection lost", "user", hello.Username)
			case <-ctx.Done():
				j.set(nil, ctx.Err())
				_ = c.Close()
				return
			}
		} else {
			j.set(nil, err)
			if retry == jobsRetryMin {
				slog.Warn("job host connection failed", "user", hello.Username, "error", err)
			} else {
				slog.Debug("job host connection retry failed", "user", hello.Username, "retry_in", retry, "error", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(retry):
		}
		retry = min(retry*2, jobsRetryMax)
	}
}

func (j *jobsRelay) set(c *jobsclient.Client, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.client, j.err = c, err
	close(j.changed)
	j.changed = make(chan struct{})
}

// connectedAfter waits for a connected client other than prev and returns
// it, or nil once ctx ends or the relay closes.
func (j *jobsRelay) connectedAfter(ctx context.Context, prev *jobsclient.Client) *jobsclient.Client {
	for {
		j.mu.Lock()
		c, changed := j.client, j.changed
		j.mu.Unlock()
		if c != nil && c != prev {
			return c
		}
		select {
		case <-ctx.Done():
			return nil
		case <-j.done:
			return nil
		case <-changed:
		}
	}
}

// current returns the connected client, or why there is none.
func (j *jobsRelay) current() (*jobsclient.Client, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.client, j.err
}

// Close stops reconnecting and closes the host connection, which ends every
// stream relayed over it.
func (j *jobsRelay) Close() {
	j.cancel()
	<-j.done
}

// openRoute opens a stream of route with request on the host of c.
func openRoute(ctx context.Context, c *jobsclient.Client, route string, request any) (net.Conn, error) {
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode %s request: %w", route, err)
	}
	raw, err := json.Marshal(relay.StreamOpenEnvelope{Route: route, Request: body})
	if err != nil {
		return nil, fmt.Errorf("encode %s open: %w", route, err)
	}
	return c.Open(ctx, raw)
}

// snapshot calls route for job id on the host. Without a host the job is
// not found.
func (j *jobsRelay) snapshot(ctx context.Context, route, id string) (bridgeipc.TaskSnapshot, error) {
	var snapshot bridgeipc.TaskSnapshot
	c, err := j.current()
	if err != nil {
		return snapshot, fmt.Errorf("%w: task %s: %w", bridgeipc.ErrNotFound, id, err)
	}
	data, err := c.Call(ctx, route, map[string]string{"taskId": id})
	if err != nil {
		return snapshot, err
	}
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return snapshot, fmt.Errorf("decode %s result: %w", route, err)
	}
	return snapshot, nil
}

func (j *jobsRelay) Get(ctx context.Context, id string) (bridgeipc.TaskSnapshot, error) {
	return j.snapshot(ctx, "tasks.get", id)
}

func (j *jobsRelay) Cancel(ctx context.Context, id string) (bridgeipc.TaskSnapshot, error) {
	return j.snapshot(ctx, "tasks.cancel", id)
}

// List returns the host's jobs, or none without a host.
func (j *jobsRelay) List(ctx context.Context) ([]bridgeipc.TaskSnapshot, error) {
	c, err := j.current()
	if err != nil {
		return nil, nil
	}
	data, err := c.Call(ctx, "tasks.list", map[string]string{"status": ""})
	if err != nil {
		return nil, err
	}
	var snapshots []bridgeipc.TaskSnapshot
	if err := json.Unmarshal(data, &snapshots); err != nil {
		return nil, fmt.Errorf("decode tasks.list result: %w", err)
	}
	return snapshots, nil
}

// Watch relays the host's tasks.watch of job id onto stream. Without a host
// the job is not found.
func (j *jobsRelay) Watch(ctx context.Context, stream net.Conn, id string) error {
	c, err := j.current()
	var host net.Conn
	if err == nil {
		host, err = openRoute(ctx, c, "tasks.watch", map[string]string{"taskId": id})
	}
	if err != nil {
		slog.Debug("job host unavailable for tasks watch", "task_id", id, "error", err)
		return relay.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("task not found: %s", id), 404)
	}
	return c.Relay(ctx, stream, host)
}

// Events copies the host's tasks.events frames onto stream until ctx ends,
// the relay closes or stream fails. When the host connection drops it waits
// for the next one and reopens the host's stream there, which re-sends the
// host's active jobs, so an open stream keeps seeing host jobs.
func (j *jobsRelay) Events(ctx context.Context, stream net.Conn) error {
	var prev *jobsclient.Client
	for {
		c := j.connectedAfter(ctx, prev)
		if c == nil {
			return nil
		}
		prev = c
		if err := copyHostEvents(ctx, c, stream); err != nil {
			return err
		}
	}
}

// copyHostEvents copies one host tasks.events stream onto stream. Only event
// frames pass: the bridge reads stream for the client's close itself, and a
// host stream that ends must not end the client's. It returns an error only
// when stream fails.
func copyHostEvents(ctx context.Context, c *jobsclient.Client, stream net.Conn) error {
	host, err := openRoute(ctx, c, "tasks.events", struct{}{})
	if err != nil {
		slog.Debug("job host events unavailable", "error", err)
		return nil
	}
	defer host.Close()
	stop := context.AfterFunc(ctx, func() { _ = host.Close() })
	defer stop()
	for {
		frame, err := relay.ReadRelayFrame(host)
		if err != nil {
			if ctx.Err() == nil && !errors.Is(err, io.EOF) {
				slog.Debug("job host events ended", "error", err)
			}
			return nil
		}
		if frame.Opcode != relay.OpStreamProgress {
			continue
		}
		if err := relay.WriteRelayFrame(stream, frame); err != nil {
			return err
		}
	}
}
