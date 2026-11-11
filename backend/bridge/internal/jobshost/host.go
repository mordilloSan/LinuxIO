// Package jobshost is the per-user job host, linuxio-jobs@<uid>.service: the
// bridge binary in a second mode that runs Mutation jobs for the bridges of
// one uid and records them in the job store.
package jobshost

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/alerts"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/common/systemd"
)

// streamOpenTimeout bounds the wait for a stream's open frame, the hello's
// included.
const streamOpenTimeout = 5 * time.Second

type Config struct {
	UID, GID   uint32
	Username   string
	SocketPath string
	// Listener is the socket systemd passed the host; nil makes Serve listen
	// on SocketPath.
	Listener *net.UnixListener
	Store    *jobs.Store
	// Scheduled is the scheduled runs' store; nil lists none.
	Scheduled *jobs.Store
	// UnitActive answers for Scheduled's running records; nil means
	// systemd.UnitRunsInvocation.
	UnitActive func(ctx context.Context, unit, invocationID string) (bool, error)
	Router     *bridgeipc.Router
	// Handlers builds the router whose handlers act for sess, the user a
	// connection's hello names; nil serves every connection with Router. The
	// root host runs every admin's jobs, each as their own user with their
	// own config store, as their privileged bridge would.
	Handlers func(sess *session.Session) (*bridgeipc.Router, error)
	// Tasks is the router's task service; Serve installs the job recorder on it.
	Tasks *bridgeipc.TaskService
	// IdleExit is how long the host stays up with no bridge and no running job.
	IdleExit time.Duration
	// MaxRunning caps the jobs running at once.
	MaxRunning int
	// Notify sends sd_notify states; nil means systemd.Notify.
	Notify func(string) error
	// Alert raises a host alert for a failed or interrupted job, and the
	// notice of a long one that completed when Notifications asks; nil raises
	// none.
	Alert func(ctx context.Context, obs alerts.Observation) error
	// ResolveAlert clears the alert of a resumed job that completed; nil clears none.
	ResolveAlert func(ctx context.Context, source, key string) error
	// Notifications returns the notification settings; nil means the defaults.
	Notifications func() alerts.NotificationSettings
	// Now is the host clock; nil means time.Now.
	Now func() time.Time
	// Peer reads the peer credentials of a connection; nil means
	// peercred.Read. Tests replace it.
	Peer func(*net.UnixConn) (peercred.Cred, error)
}

// Host serves one uid's bridges. Serve creates it.
type Host struct {
	cfg      Config
	rec      *recorder
	executor jobs.Executor
	conns    sync.WaitGroup
	// jobStreams counts the open Mutation streams; shutdown lets them write
	// their terminal frame before it closes the sessions.
	jobStreams sync.WaitGroup

	// pruned is when the store was last pruned; only the serve goroutine
	// reads or writes it.
	pruned time.Time

	mu       sync.Mutex
	bridges  int
	lastBusy time.Time
	sessions map[*relay.YamuxSession]struct{}
	closing  bool
	// owners holds, on the root host, the uid each connected session ref
	// says its user has: the owner a job record names. Permission checks
	// keep the host uid, so every admin sees the root host's jobs.
	owners map[string]ownerClaim
}

type ownerClaim struct {
	uid, gid uint32
	conns    int
}

// Serve serves bridges on cfg.Listener, or on cfg.SocketPath (0600) when
// there is none, until ctx is done, when it interrupts the running jobs, or
// until the host has been idle for cfg.IdleExit. It returns nil on either.
func Serve(ctx context.Context, cfg Config) error {
	return newHost(cfg).serve(ctx)
}

func newHost(cfg Config) *Host {
	if cfg.Notify == nil {
		cfg.Notify = systemd.Notify
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Peer == nil {
		cfg.Peer = peercred.Read
	}
	if cfg.UnitActive == nil {
		cfg.UnitActive = systemd.UnitRunsInvocation
	}
	if cfg.Notifications == nil {
		cfg.Notifications = alerts.DefaultNotifications
	}
	h := &Host{cfg: cfg, sessions: make(map[*relay.YamuxSession]struct{}), owners: make(map[string]ownerClaim)}
	h.rec = &recorder{h: h, jobs: make(map[string]*job)}
	return h
}

func (h *Host) serve(ctx context.Context) error {
	pid := int32(os.Getpid())
	start, err := jobs.ProcessStart(pid)
	if err != nil {
		return fmt.Errorf("read job host start time: %w", err)
	}
	boot, err := jobs.BootID()
	if err != nil {
		return fmt.Errorf("read boot id: %w", err)
	}
	h.executor = jobs.Executor{Kind: "host", PID: pid, PIDStart: start, BootID: boot}
	resumes, err := h.reconcile(ctx)
	if err != nil {
		return err
	}
	ln := h.cfg.Listener
	if ln == nil {
		if ln, err = listen(h.cfg.SocketPath); err != nil {
			return err
		}
	}
	h.cfg.Tasks.SetRecorder(h.rec)
	h.cfg.Tasks.SetRemote(storedJobs{h})
	h.mu.Lock()
	h.lastBusy = h.cfg.Now()
	h.mu.Unlock()

	acceptErr := make(chan error, 1)
	h.conns.Go(func() { acceptErr <- h.accept(ctx, ln) })
	defer h.rec.stop() // after closeAll: no progress timer outlives Serve
	defer h.closeAll(ln)
	h.notify("READY=1")
	slog.Debug("job host listening", "user", h.cfg.Username, "socket", ln.Addr().String())
	h.resumeEarlierBoot(ctx, resumes)
	return h.run(ctx, ln, acceptErr)
}

func listen(path string) (*net.UnixListener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("remove stale job host socket: %w", err)
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("listen on job host socket: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("restrict job host socket: %w", err)
	}
	return ln, nil
}

func (h *Host) accept(ctx context.Context, ln *net.UnixListener) error {
	for {
		conn, err := ln.AcceptUnix()
		if err != nil {
			return err
		}
		h.conns.Go(func() { h.serveConn(ctx, conn) })
	}
}

// closeAll stops accepting, closes every bridge session and waits for their
// goroutines.
func (h *Host) closeAll(ln *net.UnixListener) {
	_ = ln.Close()
	h.mu.Lock()
	h.closing = true
	sessions := make([]*relay.YamuxSession, 0, len(h.sessions))
	for sess := range h.sessions {
		sessions = append(sessions, sess)
	}
	h.mu.Unlock()
	for _, sess := range sessions {
		_ = sess.Close()
	}
	h.conns.Wait()
}

// track registers sess for closeAll, or reports false once the host is closing.
func (h *Host) track(sess *relay.YamuxSession) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return false
	}
	h.sessions[sess] = struct{}{}
	return true
}

func (h *Host) untrack(sess *relay.YamuxSession) {
	h.mu.Lock()
	delete(h.sessions, sess)
	h.mu.Unlock()
	_ = sess.Close()
}

// serveConn admits a connection from the host's own uid only, checked before
// anything is read from it, then serves its hello and streams.
func (h *Host) serveConn(ctx context.Context, conn *net.UnixConn) {
	cred, err := h.cfg.Peer(conn)
	if err != nil {
		slog.Warn("job host refused a connection", "error", err)
		_ = conn.Close()
		return
	}
	if cred.UID != h.cfg.UID {
		slog.Warn("job host refused a connection", "peer_uid", cred.UID)
		_ = conn.Close()
		return
	}
	ymux, err := relay.NewYamuxServer(conn)
	if err != nil {
		slog.Warn("job host yamux setup failed", "error", err)
		_ = conn.Close()
		return
	}
	if !h.track(ymux) {
		_ = ymux.Close()
		return
	}
	defer h.untrack(ymux)

	sess, router, ok := h.hello(ymux)
	if !ok {
		return
	}
	defer h.releaseOwner(sess.SessionID) // claimed by hello
	if !h.join() {
		return
	}
	defer h.leave()

	var streams sync.WaitGroup
	defer streams.Wait()
	for {
		stream, err := ymux.Accept()
		if err != nil {
			return
		}
		streams.Go(func() {
			defer stream.Close()
			h.serveStream(ctx, stream, sess, router)
		})
	}
}

// hello reads the first stream, which must be a jobs.hello of this protocol,
// and returns the session the connection acts for and the router serving it.
func (h *Host) hello(ymux *relay.YamuxSession) (*session.Session, *bridgeipc.Router, bool) {
	stream, err := ymux.Accept()
	if err != nil {
		return nil, nil, false
	}
	defer stream.Close()
	hello, err := readHello(stream)
	if err == nil && hello.Protocol != jobs.Protocol {
		err = fmt.Errorf("protocol %d, want %d", hello.Protocol, jobs.Protocol)
	}
	if err != nil {
		slog.Warn("job host refused a bridge hello", "error", err)
		refuseHello(stream, "unsupported jobs protocol", 426)
		return nil, nil, false
	}
	sess := &session.Session{
		SessionID: hello.Session.SessionRef,
		// The uid is the host's own, never the one the bridge claims.
		User:       session.User{Username: hello.Session.Username, UID: h.cfg.UID, GID: h.cfg.GID},
		Privileged: hello.Session.Privileged && h.cfg.UID == 0,
		Timing:     session.Timing{CreatedAt: h.cfg.Now()},
	}
	router := h.cfg.Router
	if h.cfg.Handlers != nil && h.cfg.UID == 0 {
		// A root host's peers are root, so the user their hello names is trusted.
		user := *sess
		user.User = session.User{Username: hello.Session.Username, UID: hello.Session.UID, GID: hello.Session.GID}
		if router, err = h.cfg.Handlers(&user); err != nil {
			slog.Error("job host refused a bridge hello", "user", user.User.Username, "error", err)
			refuseHello(stream, "job host cannot act for this user", 500)
			return nil, nil, false
		}
	}
	if err := relay.WriteResultOKAndClose(stream, 0, bridgeipc.Hello{Protocol: jobs.Protocol}); err != nil {
		slog.Debug("job host hello answer failed", "error", err)
		return nil, nil, false
	}
	h.claimOwner(hello.Session)
	return sess, router, true
}

// refuseHello answers a hello with an error and lets the bridge read it
// before the session closes under it.
func refuseHello(stream net.Conn, msg string, code int) {
	_ = relay.WriteResultErrorAndClose(stream, 0, msg, code)
	_ = stream.SetReadDeadline(time.Now().Add(streamOpenTimeout))
	_, _ = io.Copy(io.Discard, stream)
}

// claimOwner remembers the user of the connections with the hello's session
// ref. Only the root host, which admins share, records it: a user host's jobs
// are its own uid's.
func (h *Host) claimOwner(hello bridgeipc.HelloSession) {
	if h.cfg.UID != 0 || hello.SessionRef == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.owners[hello.SessionRef] = ownerClaim{uid: hello.UID, gid: hello.GID, conns: h.owners[hello.SessionRef].conns + 1}
}

// releaseOwner forgets a connection's claim once its last connection closes.
func (h *Host) releaseOwner(ref string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	claim, ok := h.owners[ref]
	switch {
	case !ok:
	case claim.conns <= 1:
		delete(h.owners, ref)
	default:
		claim.conns--
		h.owners[ref] = claim
	}
}

// recordOwner is the owner a job record names for a task: on the root host
// the user its connection claimed, else the task's own user.
func (h *Host) recordOwner(owner bridgeipc.TaskOwner) jobs.Owner {
	h.mu.Lock()
	defer h.mu.Unlock()
	if claim, ok := h.owners[owner.SessionID]; ok && owner.SessionID != "" {
		return jobs.Owner{UID: claim.uid, GID: claim.gid, Username: owner.Username}
	}
	return jobs.Owner{UID: owner.UID, GID: h.cfg.GID, Username: owner.Username}
}

func readHello(stream net.Conn) (bridgeipc.Hello, error) {
	var hello bridgeipc.Hello
	open, _, err := readOpen(stream)
	if err != nil {
		return hello, err
	}
	if open.Route != bridgeipc.HelloRoute {
		return hello, errors.New("first stream is not a hello")
	}
	if err := json.Unmarshal(open.Request, &hello); err != nil {
		return hello, fmt.Errorf("decode hello: %w", err)
	}
	return hello, nil
}

// readOpen reads a stream's open frame and returns its envelope and raw payload.
func readOpen(stream net.Conn) (relay.StreamOpenEnvelope, []byte, error) {
	_ = stream.SetReadDeadline(time.Now().Add(streamOpenTimeout))
	frame, err := relay.ReadRelayFrameProgressive(stream)
	// The deadline is for the open frame only; handlers own later stream I/O.
	_ = stream.SetReadDeadline(time.Time{})
	if err != nil {
		return relay.StreamOpenEnvelope{}, nil, fmt.Errorf("read stream open frame: %w", err)
	}
	if frame.Opcode != relay.OpStreamOpen {
		return relay.StreamOpenEnvelope{}, nil, fmt.Errorf("%w: expected stream open frame", relay.ErrInvalidRequest)
	}
	open, err := relay.ParseStreamOpenPayload(frame.Payload)
	return open, frame.Payload, err
}

// serveStream dispatches one stream to router as the bridge's own stream
// loop does, refusing a new job when the host is stopping or full.
func (h *Host) serveStream(ctx context.Context, stream net.Conn, sess *session.Session, router *bridgeipc.Router) {
	open, raw, err := readOpen(stream)
	if err != nil {
		slog.Warn("job host stream open failed", "error", err)
		if errors.Is(err, relay.ErrInvalidRequest) {
			_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), 400)
		}
		return
	}
	// A watch, like a mutation, carries a job to its end; the drain lets it.
	if route, ok := router.Route(open.Route); ok && (route.Mode == bridgeipc.ModeMutation || route.Name == "tasks.watch") && h.trackJobStream() {
		defer h.jobStreams.Done()
		defer awaitPeerClose(ctx, stream)
	}
	if h.newMutation(open) {
		if h.answerStored(ctx, stream, open) {
			return
		}
		if msg, code := h.refusal(ctx); code != 0 {
			slog.Warn("job host refused a job", "reason", msg, "code", code)
			_ = relay.WriteResultErrorAndClose(stream, 0, msg, code)
			return
		}
	}
	// Dispatch logs every failed route itself.
	_ = router.Dispatch(ctx, stream, bridgeipc.Request{
		Route:      open.Route,
		RawRequest: open.Request,
		ID:         open.ID,
		OpenRaw:    raw,
		Session:    sess,
	})
}

// trackJobStream counts a Mutation stream for the shutdown drain, unless the
// host is already closing.
func (h *Host) trackJobStream() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closing {
		return false
	}
	h.jobStreams.Add(1)
	return true
}

// awaitPeerClose lets the bridge read the end of a job stream before a host
// shutdown closes the session under it: once ctx is done it half-closes the
// stream and waits, at most streamDrainTimeout, for the bridge to close its end.
func awaitPeerClose(ctx context.Context, stream net.Conn) {
	if ctx.Err() == nil {
		return
	}
	// A past deadline first ends the watch's client monitor, which may still
	// be reading: a yamux stream wakes only one reader when its peer closes.
	_ = stream.SetReadDeadline(time.Now())
	if s, ok := stream.(interface{ CloseWrite() error }); ok {
		_ = s.CloseWrite()
	}
	_ = stream.SetReadDeadline(time.Now().Add(streamDrainTimeout))
	_, _ = io.Copy(io.Discard, stream)
}

// newMutation reports whether open would start a job: a Mutation route with
// a valid ID this host has no task for. A resend of a known ID only attaches,
// and Dispatch answers an invalid ID with 400.
func (h *Host) newMutation(open relay.StreamOpenEnvelope) bool {
	route, ok := h.cfg.Router.Route(open.Route)
	if !ok || route.Mode != bridgeipc.ModeMutation || jobs.ValidateID(open.ID) != nil {
		return false
	}
	_, known := h.cfg.Tasks.Get(open.ID)
	return !known
}

// answerStored answers the resend of a job this host ran before, from its
// record, without running it again. It reports false when there is no record.
func (h *Host) answerStored(ctx context.Context, stream net.Conn, open relay.StreamOpenEnvelope) bool {
	// A plain file read: a resend is still answered while the host stops.
	record, err := h.cfg.Store.Get(context.WithoutCancel(ctx), open.ID)
	if errors.Is(err, jobs.ErrNotFound) {
		return false
	}
	if err != nil {
		slog.Error("job record unreadable for a resend", "job_id", open.ID, "error", err)
		_ = relay.WriteResultErrorAndClose(stream, 0, "job record unreadable", 500)
		return true
	}
	// A record too large to keep its request is matched by route alone.
	route, _ := h.cfg.Router.Route(open.Route)
	if record.Route != open.Route || (len(record.Request) > 0 && !sameJSON(record.Request, route.RecordedRequest(open.Request))) {
		slog.Warn("job host refused a reused job id", "job_id", open.ID, "route", record.Route)
		_ = relay.WriteResultErrorAndClose(stream, 0, "job id was used for another request", 409)
		return true
	}
	if record.State == jobs.StateRunning && record.Executor.Kind == "host" {
		if _, live := h.cfg.Tasks.Get(open.ID); live {
			return false // started since newMutation looked: Dispatch attaches
		}
		record = h.markLost(ctx, record)
	}
	writeStored(stream, record)
	return true
}

// writeStored answers a resend or a watch of a stored job: its snapshot,
// then its outcome.
func writeStored(stream net.Conn, record jobs.Record) {
	snapshot := snapshotFromRecord(record)
	if relay.WriteProgress(stream, 0, bridgeipc.TaskEvent{Type: bridgeipc.TaskEventSnapshot, Task: snapshot}) != nil {
		return
	}
	if snapshot.Error != nil {
		_ = relay.WriteResultErrorAndClose(stream, 0, snapshot.Error.Message, snapshot.Error.Code)
	} else {
		_ = relay.WriteResultOKAndClose(stream, 0, snapshot.Result)
	}
}

// storedJobs answers tasks.get, tasks.watch and tasks.cancel of a job the host
// has no task for from its record: a job that ended before the memory sweep,
// or before this host started. Records a host did not run, such as the root
// bridge's network handoffs, are not its jobs. On the root host it also lists
// and follows the scheduled runs (scheduled.go).
type storedJobs struct{ h *Host }

func (s storedJobs) record(ctx context.Context, id string) (jobs.Record, error) {
	record, err := s.h.cfg.Store.Get(ctx, id)
	if errors.Is(err, jobs.ErrNotFound) && s.h.cfg.Scheduled != nil {
		return s.scheduledRecord(ctx, id)
	}
	if errors.Is(err, jobs.ErrNotFound) || (err == nil && record.Executor.Kind != "host") {
		return record, bridgeipc.NewError(fmt.Sprintf("task not found: %s", id), 404)
	}
	if err != nil {
		return record, fmt.Errorf("read job %s: %w", id, err)
	}
	if record.State == jobs.StateRunning {
		if _, live := s.h.cfg.Tasks.Get(id); live {
			// Its task is in memory but not the caller's to see.
			return record, bridgeipc.NewError(fmt.Sprintf("task not found: %s", id), 404)
		}
		record = s.h.markLost(ctx, record)
	}
	return record, nil
}

func (s storedJobs) Get(ctx context.Context, id string) (bridgeipc.TaskSnapshot, error) {
	record, err := s.record(ctx, id)
	if err != nil {
		return bridgeipc.TaskSnapshot{}, err
	}
	return s.snapshot(record), nil
}

// Cancel finds a stored job ended, with nothing left to cancel. A running
// scheduled run is stopped on the page that owns it.
func (s storedJobs) Cancel(ctx context.Context, id string) (bridgeipc.TaskSnapshot, error) {
	record, err := s.record(ctx, id)
	if err != nil {
		return bridgeipc.TaskSnapshot{}, err
	}
	if scheduledRunning(record) {
		return bridgeipc.TaskSnapshot{}, bridgeipc.NewError("a scheduled run is stopped on its page", 409)
	}
	return s.snapshot(record), nil
}

// Status counts the jobs the host runs, as its idle rule does. The host is of
// this build, so no update waits for it.
func (s storedJobs) Status(context.Context) (bridgeipc.TaskHostStatus, error) {
	return bridgeipc.TaskHostStatus{Running: s.h.rec.running()}, nil
}

func (s storedJobs) Watch(ctx context.Context, stream net.Conn, id string) error {
	record, err := s.record(ctx, id)
	if err == nil && scheduledRunning(record) {
		// It has no task to watch; tasks.events reads its progress file.
		err = bridgeipc.NewError("a running scheduled run is followed through tasks.events", 409)
	}
	if err != nil {
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), bridgeipc.StatusCode(err))
		return err
	}
	writeStored(stream, record)
	return nil
}

// markLost marks interrupted a record a host left running with no task in
// this host to finish it, such as one whose finish failed to write, and
// returns it.
func (h *Host) markLost(ctx context.Context, record jobs.Record) jobs.Record {
	marked := false
	updated, err := h.cfg.Store.Update(context.WithoutCancel(ctx), record.ID, func(r *jobs.Record) error {
		if r.State == jobs.StateRunning && r.Executor.Kind == "host" {
			r.State, r.Reason, r.FinishedAt = jobs.StateInterrupted, jobs.ReasonCrash, h.cfg.Now().UTC()
			marked = true
		}
		return nil
	})
	if err != nil {
		slog.Warn("lost job not marked interrupted", "job_id", record.ID, "error", err)
		return record
	}
	if marked {
		slog.Info("job marked interrupted", "job_id", record.ID, "route", record.Route, "reason", updated.Reason)
	}
	return updated
}

// RunningSnapshot is the snapshot of a record whose job still runs. progress
// is the newest progress the job wrote, its store's progress file; without
// one the record's own stands.
func RunningSnapshot(r jobs.Record, progress jsontext.Value) bridgeipc.TaskSnapshot {
	if len(progress) > 0 {
		r.Progress = progress
	}
	return recordSnapshot(r)
}

// WaitedSnapshot is the snapshot of a running job an update waits for, as
// tasks.host lists it: without the paths or packages the job acts on.
func WaitedSnapshot(r jobs.Record, progress jsontext.Value) bridgeipc.TaskSnapshot {
	snapshot := RunningSnapshot(r, progress)
	withoutTargets(r, &snapshot)
	return snapshot
}

// withoutTargets drops from the snapshot of a host job's record the paths or
// packages the job acts on, which can run to thousands and which no list of
// jobs shows: its label names them. A scheduled run's identity names its
// schedule and stays.
func withoutTargets(r jobs.Record, snapshot *bridgeipc.TaskSnapshot) {
	if snapshot.Metadata == nil || r.Executor.Kind == jobs.ExecutorUnit {
		return
	}
	snapshot.Metadata.Identity, snapshot.Metadata.PackageIDs = nil, nil
}

// snapshotFromRecord is the task snapshot of a stored job, terminal but for a
// scheduled run that runs. Any other record still running with no task here
// was left by a crash: it reads as interrupted.
func snapshotFromRecord(r jobs.Record) bridgeipc.TaskSnapshot {
	snapshot := recordSnapshot(r)
	switch r.State {
	case jobs.StateCompleted:
		if len(r.Result) > 0 {
			snapshot.Result = r.Result
		}
	case jobs.StateFailed:
		snapshot.Error = bridgeipc.NewError("job failed", 500)
		if r.Error != nil {
			snapshot.Error.Message = r.Error.Message
			snapshot.Error.Code = cmp.Or(r.Error.Code, 500)
		}
	case jobs.StateCanceled:
		snapshot.Error = bridgeipc.NewError("operation aborted", 499)
		if r.Error != nil {
			snapshot.Error.Message = r.Error.Message
		}
	default: // interrupted, or running with no task left to finish it
		if scheduledRunning(r) {
			break
		}
		snapshot.State = bridgeipc.TaskStateInterrupted
		snapshot.Error = bridgeipc.NewError("job was interrupted", 409)
	}
	return snapshot
}

// recordSnapshot is what every snapshot of a record shows, whatever its
// state: never the record's request, which may hold secrets.
func recordSnapshot(r jobs.Record) bridgeipc.TaskSnapshot {
	snapshot := bridgeipc.TaskSnapshot{
		ID:        r.ID,
		Type:      r.Route,
		Owner:     bridgeipc.TaskOwner{Username: r.Owner.Username, UID: r.Owner.UID},
		State:     bridgeipc.TaskState(r.State),
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.CreatedAt,
	}
	if !r.StartedAt.IsZero() {
		snapshot.StartedAt, snapshot.UpdatedAt = &r.StartedAt, r.StartedAt
	}
	if !r.FinishedAt.IsZero() {
		snapshot.FinishedAt, snapshot.UpdatedAt = &r.FinishedAt, r.FinishedAt
	}
	if len(r.Metadata) > 0 {
		var metadata bridgeipc.TaskMetadata
		if err := json.Unmarshal(r.Metadata, &metadata); err != nil {
			slog.Debug("job record metadata not decoded", "job_id", r.ID, "error", err)
		} else {
			// A host job's record keeps its identity once, as its target; a
			// scheduled run's metadata carries its own.
			if metadata.Identity == nil && r.Executor.Kind != jobs.ExecutorUnit {
				metadata.Identity = r.Target
			}
			snapshot.Metadata = &metadata
		}
	}
	if len(r.Progress) > 0 {
		var progress bridgeipc.TaskProgress
		if err := json.Unmarshal(r.Progress, &progress); err != nil {
			slog.Debug("job record progress not decoded", "job_id", r.ID, "error", err)
		} else {
			snapshot.Progress = &progress
		}
	}
	return snapshot
}

// sameJSON compares two JSON values ignoring insignificant whitespace.
func sameJSON(a, b jsontext.Value) bool {
	a, b = a.Clone(), b.Clone()
	return a.Compact() == nil && b.Compact() == nil && bytes.Equal(a, b)
}

// refusal says why a new job may not start now, or returns code 0.
func (h *Host) refusal(ctx context.Context) (string, int) {
	if ctx.Err() != nil {
		return "job host is stopping", 503
	}
	// shortcut: checked before Dispatch, so concurrent starts can pass the cap by a few; make admission atomic in the router if that matters.
	if _, running := h.counts(); running >= h.cfg.MaxRunning {
		return "too many running jobs", 409
	}
	return "", 0
}

func (h *Host) notify(state string) {
	if err := h.cfg.Notify(state); err != nil {
		slog.Warn("job host notify failed", "state", state, "error", err)
	}
}

// alertSource is the alerts source of every job alert; the job ID is its key.
const alertSource = "jobs"

// alert raises a host alert about the job of r when the host raises alerts,
// titled with the job's name and what happened to it, linking to the job in
// the history.
func (h *Host) alert(ctx context.Context, r jobs.Record, severity, happened, message string) {
	if h.cfg.Alert == nil {
		return
	}
	obs := alerts.Observation{
		Source: alertSource, Key: r.ID, Severity: severity, Title: jobName(r) + " " + happened,
		Message: message, Link: "/history?job=" + r.ID,
	}
	if err := h.cfg.Alert(ctx, obs); err != nil {
		slog.Warn("job alert failed", "job_id", r.ID, "error", err)
	}
}

// fileActions names the file browser actions whose route does not read as one.
var fileActions = map[string]string{
	"filebrowser.trash_batch":   "Move to Trash",
	"filebrowser.trash_restore": "Restore from Trash",
	"filebrowser.trash_purge":   "Delete from Trash",
	"filebrowser.chmod_batch":   "Change permissions",
}

// jobName names a job in its alerts: its label, or its route when it has none.
// A file browser job's label names only what it acts on, such as "12 items",
// so its action comes first: one fileActions names, or else its route's,
// without a trailing "_batch": filebrowser.copy_batch gives "Copy: 12 items".
func jobName(r jobs.Record) string {
	var metadata bridgeipc.TaskMetadata
	if len(r.Metadata) > 0 {
		if err := json.Unmarshal(r.Metadata, &metadata); err != nil {
			slog.Debug("job record metadata not decoded", "job_id", r.ID, "error", err)
		}
	}
	if metadata.Label == "" {
		return r.Route
	}
	if action, ok := fileActions[r.Route]; ok {
		return action + ": " + metadata.Label
	}
	if !strings.HasPrefix(r.Route, "filebrowser.") {
		return metadata.Label
	}
	action := strings.TrimSuffix(r.Route[strings.LastIndexByte(r.Route, '.')+1:], "_batch")
	action = strings.ReplaceAll(action, "_", " ")
	if action == "" {
		return metadata.Label
	}
	return strings.ToUpper(action[:1]) + action[1:] + ": " + metadata.Label
}

// resolveAlert clears the alert about job id, such as the one its
// interruption raised, when the host clears alerts.
func (h *Host) resolveAlert(ctx context.Context, id string) {
	if h.cfg.ResolveAlert == nil {
		return
	}
	if err := h.cfg.ResolveAlert(ctx, alertSource, id); err != nil {
		slog.Warn("job alert not resolved", "job_id", id, "error", err)
	}
}

// alertInterrupted raises the alert of a job its host stopped before it
// finished, whatever the report settings say: a warning for a shutdown or a
// reboot, and an error for a crash, whose outcome is unknown. A warning
// follows the levels grid like any other.
func (h *Host) alertInterrupted(ctx context.Context, r jobs.Record) {
	severity := alerts.SeverityWarning
	if r.Reason == jobs.ReasonCrash {
		severity = alerts.SeverityError
	}
	h.alert(ctx, r, severity, "was interrupted", fmt.Sprintf("The job host stopped before the job finished (%s).", r.Reason))
}

// alertLongJob raises the notice of a job that completed after running for
// alerts.LongJob or longer, from its record's start to its end, when the
// settings ask for long jobs.
func (h *Host) alertLongJob(ctx context.Context, r jobs.Record) {
	ran := r.FinishedAt.Sub(r.StartedAt)
	if h.cfg.Alert == nil || ran < alerts.LongJob || h.cfg.Notifications().ReportFor(alerts.KindBrowserJobs) != alerts.ReportLongJobs {
		return
	}
	h.alert(ctx, r, alerts.SeverityInfo, "finished", alerts.RanFor(ran))
}
