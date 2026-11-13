package cmd

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/jobsclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/jobshost"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// Seams for tests.
var (
	jobsHostSocket = jobsclient.HostSocketPath
	jobsHostStore  = hostStore
	jobsConnect    = jobsclient.Connect
	effectiveUID   = func() uint32 { return uint32(os.Geteuid()) }
	// hostWaitTimeout bounds how long a tasks.* request for a host job waits
	// for the host connection, which a client reattaching after a lost
	// connection often finds reconnecting.
	hostWaitTimeout = 10 * time.Second
)

const (
	jobsRetryMin = time.Second
	jobsRetryMax = 30 * time.Second
)

var (
	errJobHostConnecting  = errors.New("job host connection not ready")
	errJobHostGone        = errors.New("job host connection closed")
	errJobHostUnavailable = bridgeipc.NewError("job host unavailable", 503)
)

// hostStore is the store of uid's job host, in the StateDirectory= and
// RuntimeDirectory= of linuxio-jobs@<uid>.service. The bridge runs as that uid.
func hostStore(uid uint32) *jobs.Store {
	dir := strconv.FormatUint(uint64(uid), 10)
	return jobs.NewStore(filepath.Join(version.DataDir, "jobs", dir), filepath.Join(version.RunDir, "jobs", dir))
}

// relayForwarder runs the bridge's Mutation streams on the job host.
type relayForwarder struct {
	client func() (*jobsclient.Client, error)
	// waited lists the jobs an update waits for, read from the host's
	// records; false when they could not be read.
	waited func(ctx context.Context) ([]jobs.Record, bool)
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
			records, read := f.waited(ctx)
			msg, code = updatePendingMessage(len(records), read), 409
		}
		_ = relay.WriteResultErrorAndClose(stream, 0, msg, code)
		return nil
	}
	return c.Relay(ctx, stream, host)
}

// updatePendingMessage refuses a new job while an update waits for running
// jobs of the older job host, naming how many when they could be read. With
// none left the host is about to exit and the update to finish.
func updatePendingMessage(running int, read bool) string {
	switch {
	case !read:
		return "update_pending: LinuxIO update waits for running jobs"
	case running == 0:
		return "update_pending: LinuxIO was updated; new jobs can start shortly"
	case running == 1:
		return "update_pending: LinuxIO update waits for 1 running job"
	default:
		return fmt.Sprintf("update_pending: LinuxIO update waits for %d running jobs", running)
	}
}

// jobsRelay keeps a bridge session connected to its job host, reconnecting
// in the background, and serves Mutation streams and tasks.* for host jobs
// over that connection.
type jobsRelay struct {
	relayForwarder
	cancel context.CancelFunc
	done   chan struct{}
	// store is the job host's, read while an update waits for the host.
	store *jobs.Store

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
	uid := effectiveUID()
	j := &jobsRelay{cancel: cancel, done: make(chan struct{}), store: jobsHostStore(uid), err: errJobHostConnecting, changed: make(chan struct{})}
	j.relayForwarder.client = j.current
	j.waited = j.waitedRecords
	hello := bridgeipc.HelloSession{
		Username:   sess.User.Username,
		UID:        sess.User.UID,
		GID:        sess.User.GID,
		Privileged: sess.Privileged,
		SessionRef: session.DiagnosticRef(sess.SessionID),
	}
	go j.run(ctx, uid, hello)
	return j
}

// run connects, waits for the connection to end, and reconnects, backing
// off from 1 s to 30 s while connecting fails.
func (j *jobsRelay) run(ctx context.Context, uid uint32, hello bridgeipc.HelloSession) {
	defer close(j.done)
	retry := jobsRetryMin
	// warned: this outage's first failure was logged at Warn.
	warned := false
	for {
		c, err := jobsConnect(ctx, uid, hello, jobsHostSocket(uid))
		if ctx.Err() != nil {
			if c != nil {
				_ = c.Close()
			}
			return
		}
		if err == nil {
			retry, warned = jobsRetryMin, false
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
			if !warned {
				warned = true
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

// waitConnected returns the connected client, waiting up to hostWaitTimeout
// for one.
func (j *jobsRelay) waitConnected(ctx context.Context) (*jobsclient.Client, error) {
	waitCtx, cancel := context.WithTimeout(ctx, hostWaitTimeout)
	defer cancel()
	if c := j.connectedAfter(waitCtx, nil); c != nil {
		return c, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errJobHostUnavailable
}

// waitSettled waits up to hostWaitTimeout until the relay has a client or its
// last connection attempt found a job host of an older build, and returns the
// client or jobsclient.ErrUpdatePending; errJobHostUnavailable when neither
// came in time.
func (j *jobsRelay) waitSettled(ctx context.Context) (*jobsclient.Client, error) {
	waitCtx, cancel := context.WithTimeout(ctx, hostWaitTimeout)
	defer cancel()
	for {
		j.mu.Lock()
		c, err, changed := j.client, j.err, j.changed
		j.mu.Unlock()
		if c != nil || errors.Is(err, jobsclient.ErrUpdatePending) {
			return c, err
		}
		select {
		case <-waitCtx.Done():
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			return nil, errJobHostUnavailable
		case <-j.done:
			return nil, errJobHostUnavailable
		case <-changed:
		}
	}
}

// waitedRecords reads, from the running records of a job host an update waits
// for, the jobs it still runs: its own, whose process lives. A record whose
// process is gone is left for the next host, which marks it interrupted. An
// unreadable store reads as none, and false.
func (j *jobsRelay) waitedRecords(ctx context.Context) ([]jobs.Record, bool) {
	records, err := j.store.ListActive(ctx)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("job host records unreadable while an update waits", "error", err)
		}
		return nil, false
	}
	return slices.DeleteFunc(records, func(r jobs.Record) bool {
		return r.Executor.Kind != "host" || !jobs.ProcessAlive(r.Executor)
	}), true
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

// snapshot calls route for job id on the host.
func (j *jobsRelay) snapshot(ctx context.Context, route, id string) (bridgeipc.TaskSnapshot, error) {
	var snapshot bridgeipc.TaskSnapshot
	c, err := j.waitConnected(ctx)
	if err != nil {
		return snapshot, err
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

// History returns a page of the host's ended jobs once the first connection
// attempt has ended. While an update waits it returns none: the older host
// cannot be asked, and no other host comes until it exits.
func (j *jobsRelay) History(ctx context.Context, req bridgeipc.TaskHistoryRequest) (bridgeipc.TaskHistory, error) {
	var history bridgeipc.TaskHistory
	c, err := j.waitSettled(ctx)
	if errors.Is(err, jobsclient.ErrUpdatePending) {
		return history, nil
	}
	if err != nil {
		return history, err
	}
	data, err := c.Call(ctx, "tasks.history", req)
	if err != nil {
		return history, err
	}
	if err := json.Unmarshal(data, &history); err != nil {
		return history, fmt.Errorf("decode tasks.history result: %w", err)
	}
	return history, nil
}

// Status says whether an update waits for the job host, once the first
// connection attempt has ended. A host of an older build refuses the hello and
// cannot be asked, so its running jobs are read from its records; a host of
// this build is asked. With no host within the host wait nothing waits.
func (j *jobsRelay) Status(ctx context.Context) (bridgeipc.TaskHostStatus, error) {
	var status bridgeipc.TaskHostStatus
	c, err := j.waitSettled(ctx)
	switch {
	case errors.Is(err, jobsclient.ErrUpdatePending):
		status.UpdatePending = true
		records, _ := j.waitedRecords(ctx)
		for _, record := range records {
			status.Jobs = append(status.Jobs, jobshost.WaitedSnapshot(record, j.progress(record.ID)))
		}
		status.Running = len(status.Jobs)
		return status, nil
	case errors.Is(err, errJobHostUnavailable):
		return status, nil
	case err != nil:
		return status, err
	}
	data, err := c.Call(ctx, "tasks.host", struct{}{})
	if err != nil {
		return status, err
	}
	if err := json.Unmarshal(data, &status); err != nil {
		return status, fmt.Errorf("decode tasks.host result: %w", err)
	}
	return status, nil
}

// progress is the progress the job host last wrote for its job id, nil when
// there is none.
func (j *jobsRelay) progress(id string) jsontext.Value {
	progress, err := j.store.ReadProgress(id)
	if err != nil {
		if !errors.Is(err, jobs.ErrNotFound) {
			slog.Debug("job progress unreadable while an update waits", "job_id", id, "error", err)
		}
		return nil
	}
	return progress
}

// Output returns a chunk of a saved output, which only the host keeps, waiting
// for the host as tasks.get does: without one the output is unknown, not empty.
func (j *jobsRelay) Output(ctx context.Context, req bridgeipc.TaskOutputRequest) (bridgeipc.TaskOutput, error) {
	var output bridgeipc.TaskOutput
	c, err := j.waitConnected(ctx)
	if err != nil {
		return output, err
	}
	data, err := c.Call(ctx, "tasks.output", req)
	if err != nil {
		return output, err
	}
	if err := json.Unmarshal(data, &output); err != nil {
		return output, fmt.Errorf("decode tasks.output result: %w", err)
	}
	return output, nil
}

// Watch relays the host's tasks.watch of job id onto stream.
func (j *jobsRelay) Watch(ctx context.Context, stream net.Conn, id string) error {
	c, err := j.waitConnected(ctx)
	var host net.Conn
	if err == nil {
		host, err = openRoute(ctx, c, "tasks.watch", map[string]string{"taskId": id})
	}
	if err != nil {
		slog.Debug("job host unavailable for tasks watch", "task_id", id, "error", err)
		return relay.WriteResultErrorAndClose(stream, 0, "job host unavailable", 503)
	}
	return c.Relay(ctx, stream, host)
}

// Events copies the host's tasks.events frames onto stream until ctx ends,
// the relay closes or stream fails. When the host connection drops it waits
// for the next one and reopens the host's stream there, which re-sends the
// host's active jobs, so an open stream keeps seeing host jobs.
func (j *jobsRelay) Events(ctx context.Context, stream net.Conn) error {
	var prev *jobsclient.Client
	// running holds the host jobs this stream last saw running. One that ended
	// while no host was connected sent no event; the next connection sends
	// its state.
	running := make(map[string]struct{})
	for {
		c := j.connectedAfter(ctx, prev)
		if c == nil {
			return nil
		}
		prev = c
		if err := copyHostEvents(ctx, c, stream, running); err != nil {
			return err
		}
	}
}

// copyHostEvents copies one host tasks.events stream onto stream. The stream
// is asked for the end of the jobs in running, and refills running from its
// snapshots and events. Only event frames pass: the bridge reads stream for
// the client's close itself, and a host stream that ends must not end the
// client's. It returns an error only when stream fails.
func copyHostEvents(ctx context.Context, c *jobsclient.Client, stream net.Conn, running map[string]struct{}) error {
	host, err := openRoute(ctx, c, "tasks.events", bridgeipc.TaskEventsRequest{IDs: slices.Collect(maps.Keys(running))})
	if err != nil {
		slog.Debug("job host events unavailable", "error", err)
		return nil
	}
	clear(running)
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
		if frame.Opcode == relay.OpStreamClose {
			return nil
		}
		if frame.Opcode != relay.OpStreamProgress {
			continue
		}
		trackRunning(running, frame.Payload)
		if err := relay.WriteRelayFrame(stream, frame); err != nil {
			return err
		}
	}
}

// trackRunning keeps running up to date with the job an event frame reports.
func trackRunning(running map[string]struct{}, payload []byte) {
	var event struct {
		Task struct {
			ID    string              `json:"id"`
			State bridgeipc.TaskState `json:"state"`
		} `json:"task"`
	}
	if json.Unmarshal(payload, &event) != nil || event.Task.ID == "" {
		return
	}
	if event.Task.State.Terminal() {
		delete(running, event.Task.ID)
	} else {
		running[event.Task.ID] = struct{}{}
	}
}
