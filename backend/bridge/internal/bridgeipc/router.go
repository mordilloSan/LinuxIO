package bridgeipc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/goroutinelabel"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type Mode string

const (
	ModeCall     Mode = "call"
	ModeDuplex   Mode = "duplex"
	ModeMutation Mode = "mutation"
)

var (
	ErrInvalidArgs     = errors.New("invalid arguments")
	ErrForbidden       = errors.New("forbidden")
	ErrNotFound        = errors.New("not found")
	ErrConflict        = errors.New("conflict")
	ErrRouteNotFound   = errors.New("route not found")
	ErrDuplicateActive = errors.New("task already active")
)

type CallFunc func(ctx context.Context, request Request) (any, error)
type DuplexFunc func(ctx context.Context, stream net.Conn, request Request) error

type RequestDecoder func(raw jsontext.Value) (any, error)

type Request struct {
	Route        string
	RawRequest   jsontext.Value
	DecodedValue any
	Session      *session.Session
	Owner        TaskOwner
	// ID is the client-chosen job ID of a Mutation request.
	ID string
	// OpenRaw is the stream-open payload as received, for forwarding.
	OpenRaw []byte
}

type Route struct {
	Name       string
	Mode       Mode
	Call       CallFunc
	Runner     TaskRunner
	Duplex     DuplexFunc
	Privileged bool
	Policy     TaskPolicy
	Decode     RequestDecoder
	Lifetime   TaskLifetime
	Metadata   TaskMetadataBuilder

	// Mutation routes only.
	BrowserBound bool
	Target       func(request any) []string
	Secrets      []string
	Suspend      SuspendFunc
	Resume       ResumeFunc
}

type RouteOption func(*Route)

func Privileged(r *Route) {
	r.Privileged = true
}

// Exclusive admits one task of the route at a time, whatever its target.
func Exclusive(r *Route) {
	r.Policy = TaskExclusive
}

func WithRequestDecoder(decode RequestDecoder) RouteOption {
	return func(r *Route) {
		r.Decode = decode
	}
}

// WithTaskMetadata declares the only request-derived data that may be exposed
// through public task snapshots. Routes without this option expose no metadata.
func WithTaskMetadata(build TaskMetadataBuilder) RouteOption {
	return func(r *Route) {
		r.Metadata = build
	}
}

// WithBrowserBound keeps a Mutation route in the bridge: its bytes come from
// or go to the browser.
func WithBrowserBound() RouteOption {
	return func(r *Route) {
		r.BrowserBound = true
	}
}

// WithTarget names what a Mutation request works on, so a second run on the
// same target can be refused while the first runs.
func WithTarget(fn func(any) []string) RouteOption {
	return func(r *Route) {
		r.Target = fn
	}
}

// WithSecrets names the request members a Mutation's job record leaves out.
func WithSecrets(members ...string) RouteOption {
	return func(r *Route) {
		r.Secrets = members
	}
}

// RecordedRequest returns the request a job record keeps: raw without the
// route's secret members. It returns nil for a request that is not an object.
func (r Route) RecordedRequest(raw jsontext.Value) jsontext.Value {
	if len(r.Secrets) == 0 {
		return raw
	}
	var members map[string]jsontext.Value
	if err := json.Unmarshal(raw, &members); err != nil {
		return nil
	}
	for _, name := range r.Secrets {
		delete(members, name)
	}
	// Sorted, so a resend compares equal to the record.
	data, err := json.Marshal(members, json.Deterministic(true))
	if err != nil {
		return nil
	}
	return data
}

// WithResume declares the hooks that suspend a Mutation job at shutdown and
// resume it afterwards.
func WithResume(s SuspendFunc, r ResumeFunc) RouteOption {
	if s == nil || r == nil {
		panic("bridge resume hooks cannot be nil")
	}
	return func(route *Route) {
		route.Suspend = s
		route.Resume = r
	}
}

// TaskPolicy says which tasks of a route may run side by side. Any number
// may, except that a second task on the same target, named by its metadata
// identity (a pool, a disk, a set of paths), is refused while the first runs,
// as the kernel or device would refuse it. Exclusive allows one task of the
// route at a time, for work that owns a whole system resource such as the
// package manager. There is no queue, no rate limit and no runtime cap: a
// task ends when it finishes or is canceled.
type TaskPolicy struct {
	Name      string
	Exclusive bool
}

var (
	TaskDefault   = TaskPolicy{Name: "default"}
	TaskExclusive = TaskPolicy{Name: "exclusive", Exclusive: true}
)

// MutationForwarder hands a Mutation stream to the job host that runs it.
// open is the parsed stream-open envelope and raw the payload as received.
// Forward owns the stream's error frame: a failing forward must write it,
// the router writes none.
type MutationForwarder interface {
	Forward(ctx context.Context, stream net.Conn, open relay.StreamOpenEnvelope, raw []byte) error
}

type Router struct {
	mu                sync.RWMutex
	routes            map[string]Route
	registry          *TaskService
	forwarder         MutationForwarder
	pendingIdentities map[string]chan struct{}
}

type runnerResult struct {
	result any
	err    error
}

func NewRouter(registry *TaskService) *Router {
	if registry == nil {
		registry = DefaultTaskService
	}
	return &Router{
		routes:            make(map[string]Route),
		registry:          registry,
		pendingIdentities: make(map[string]chan struct{}),
	}
}

// SetMutationForwarder sends every Mutation route that is not browser-bound
// to f instead of running it in this process.
func (r *Router) SetMutationForwarder(f MutationForwarder) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forwarder = f
}

// TaskService returns the task service used by this router.
func (r *Router) TaskService() *TaskService {
	return r.registry
}

// Call registers a bounded request-response route. The handler returns one
// result directly; the router writes its result or error frame and closes the
// stream.
func (r *Router) Call(name string, handler CallFunc, opts ...RouteOption) {
	r.register(Route{Name: name, Mode: ModeCall, Call: handler}, opts...)
}

// Mutation registers a route that changes the host. It runs as a durable task
// admitted under TaskDefault.
func (r *Router) Mutation(name string, runner TaskRunner, opts ...RouteOption) {
	r.register(Route{Name: name, Mode: ModeMutation, Runner: runner, Policy: TaskDefault}, opts...)
}

// Duplex registers a full-duplex streaming route. The handler receives the raw
// net.Conn, allowing bidirectional communication for the lifetime of the stream.
func (r *Router) Duplex(name string, handler DuplexFunc, opts ...RouteOption) {
	r.register(Route{Name: name, Mode: ModeDuplex, Duplex: handler}, opts...)
}

// Dispatch routes an incoming request to the appropriate handler based on the
// request route, enforcing privilege checks and logging request lifecycle events.
func (r *Router) Dispatch(ctx context.Context, stream net.Conn, req Request) (dispatchErr error) {
	req.Owner = ownerFromSession(req.Session)
	startedAt := time.Now()
	routeName := ""
	defer func() {
		// A failed route is logged here and nowhere else: handlers return
		// errors instead of logging them.
		msg, level := "route completed", slog.LevelDebug
		switch {
		case dispatchErr == nil:
		case errors.Is(dispatchErr, context.Canceled):
			msg, level = "route canceled", slog.LevelInfo
		default:
			msg, level = "route failed", failureLevel(dispatchErr)
		}
		slog.Log(ctx, level, msg,
			"route", routeName,
			"user", req.Owner.Username,
			"duration", time.Since(startedAt),
			"error", dispatchErr)
	}()

	route, ok := r.lookup(req.Route)
	if !ok {
		err := fmt.Errorf("%w: %s", ErrRouteNotFound, req.Route)
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), StatusCode(err))
		return err
	}
	// Label only the canonical registered route. req.Route is client-controlled
	// and must not be copied into production diagnostics before validation.
	ctx = goroutinelabel.With(ctx, "route", route.Name)
	routeName = route.Name
	if route.Privileged && (req.Session == nil || !req.Session.Privileged) {
		err := fmt.Errorf("%w: privileged route %s requires elevated bridge", ErrForbidden, req.Route)
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), StatusCode(err))
		return err
	}
	if route.Decode != nil {
		decoded, err := route.Decode(req.RawRequest)
		if err != nil {
			err = fmt.Errorf("%w: %s: %v", ErrInvalidArgs, req.Route, err)
			_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), StatusCode(err))
			return err
		}
		req.DecodedValue = decoded
	}

	var err error
	switch route.Mode {
	case ModeCall:
		err = r.dispatchCall(ctx, stream, route, req)
	case ModeDuplex:
		err = route.Duplex(ctx, stream, req)
	case ModeMutation:
		err = r.dispatchMutation(ctx, stream, route, req)
	default:
		err = fmt.Errorf("unsupported route mode: %s", route.Mode)
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), 500)
	}

	return err
}

func (r *Router) register(route Route, opts ...RouteOption) {
	r.registerRoute(route, false, opts...)
}

func (r *Router) registerTaskServiceRoute(service *TaskService, route Route, opts ...RouteOption) {
	if service == nil || service != r.registry {
		panic("bridge task service route must use the router task service")
	}
	r.registerRoute(route, true, opts...)
}

func (r *Router) registerRoute(route Route, allowTaskServiceRoute bool, opts ...RouteOption) {
	if route.Name == "" {
		panic("bridge route cannot be empty")
	}
	if strings.HasPrefix(route.Name, "tasks.") && !allowTaskServiceRoute {
		panic("bridge route uses reserved tasks.* namespace: " + route.Name)
	}
	validateRouteHandler(route)
	for _, opt := range opts {
		opt(&route)
	}
	if route.Mode == ModeMutation {
		route.Lifetime = TaskLifetimeDurable
		if route.BrowserBound {
			// Its bytes ride the session, so the session's end cancels it and
			// its cleanup removes the partial upload or the archive.
			route.Lifetime = TaskLifetimeSession
		}
	}
	if route.Mode != ModeMutation && (route.BrowserBound || route.Target != nil || route.Suspend != nil || route.Resume != nil) {
		panic("bridge mutation options are allowed only on mutation routes: " + route.Name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.routes[route.Name]; exists {
		panic("bridge route already registered: " + route.Name)
	}
	r.routes[route.Name] = route
}

func validateRouteHandler(route Route) {
	switch route.Mode {
	case ModeCall:
		if route.Call == nil {
			panic("bridge call route handler cannot be nil: " + route.Name)
		}
	case ModeMutation:
		if route.Runner == nil {
			panic("bridge mutation route handler cannot be nil: " + route.Name)
		}
	case ModeDuplex:
		if route.Duplex == nil {
			panic("bridge duplex route handler cannot be nil: " + route.Name)
		}
	default:
		panic("bridge route has invalid mode: " + string(route.Mode))
	}
}

// Route returns the registered route named name.
func (r *Router) Route(name string) (Route, bool) {
	return r.lookup(name)
}

func (r *Router) lookup(route string) (Route, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	found, ok := r.routes[route]
	return found, ok
}

func (r *Router) dispatchCall(ctx context.Context, stream net.Conn, route Route, request Request) error {
	ctx, cleanup := requestAbortContext(ctx, stream)
	defer cleanup()
	result, err := route.Call(ctx, request)
	if err != nil {
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), StatusCode(err))
		return err
	}
	return relay.WriteResultOKAndClose(stream, 0, result)
}

// dispatchMutation forwards a Mutation to the job host or, in the host or for
// a browser-bound route, runs it as the task named by the client's ID and
// streams it until its result. A resend of the same ID attaches to that task.
func (r *Router) dispatchMutation(ctx context.Context, stream net.Conn, route Route, req Request) error {
	r.mu.RLock()
	forwarder := r.forwarder
	r.mu.RUnlock()
	if forwarder != nil && !route.BrowserBound {
		return forwarder.Forward(ctx, stream, relay.StreamOpenEnvelope{Route: req.Route, Request: req.RawRequest, ID: req.ID}, req.OpenRaw)
	}
	if err := jobs.ValidateID(req.ID); err != nil {
		err = fmt.Errorf("%w: %s: %w", ErrInvalidArgs, route.Name, err)
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), StatusCode(err))
		return err
	}
	task, _, err := r.startStableTask(route, req, mutationIdentity(route.Name, req.ID, req.RawRequest))
	if err != nil {
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), StatusCode(err))
		return err
	}
	return WatchTaskStreamWithSnapshot(stream, task)
}

// mutationIdentity is the task identity of Mutation job id: a resend of the
// same route and request bytes attaches to its task.
func mutationIdentity(route, id string, request jsontext.Value) TaskIdentity {
	sum := sha256.Sum256([]byte(route + "\x00" + string(request)))
	return TaskIdentity{ID: id, Fingerprint: hex.EncodeToString(sum[:])}
}

// StartResume starts the task that continues the interrupted Mutation job id
// by running route's Resume hook on record, the job's stored record. The task
// is admitted, recorded and identified as the job's first run was, so it is
// visible through tasks.* and a resend of the job attaches to it. It skips
// Dispatch's Privileged check by design: only the root host's jobs.resume,
// which admits root peers only, reaches it.
func (r *Router) StartResume(route Route, record jsontext.Value, owner TaskOwner, id string) (*Task, error) {
	if route.Mode != ModeMutation || route.Resume == nil {
		return nil, fmt.Errorf("%w: %s cannot resume", ErrConflict, route.Name)
	}
	var stored struct {
		Request jsontext.Value `json:"request"`
	}
	if err := json.Unmarshal(record, &stored); err != nil {
		return nil, fmt.Errorf("decode job record: %w", err)
	}
	req := Request{Route: route.Name, RawRequest: stored.Request, Owner: owner, ID: id}
	if route.Decode != nil {
		decoded, err := route.Decode(stored.Request)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %v", ErrInvalidArgs, route.Name, err)
		}
		req.DecodedValue = decoded
	}
	resume := route.Resume
	route.Runner = func(ctx context.Context, _ *Task, _ any) (any, error) { return resume(ctx, record) }
	task, _, err := r.startStableTask(route, req, mutationIdentity(route.Name, id, stored.Request))
	return task, err
}

// routeRunner reports physical completion through executionDone. A canceled
// task becomes terminal as soon as its context ends, even when its handler
// ignores cancellation, so admission accounting must wait for this signal
// rather than relying on Task.Done alone.
func (r *Router) routeRunner(route Route, executionDone chan<- struct{}) TaskRunner {
	return func(ctx context.Context, task *Task, request any) (any, error) {
		done := make(chan runnerResult, 1)
		go func() {
			defer close(executionDone)
			result, err := route.Runner(ctx, task, request)
			done <- runnerResult{result: result, err: err}
		}()

		select {
		case result := <-done:
			return result.result, result.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// startStableTask serialises the starts of one stable identity: a retry of an
// existing task attaches to it without being admitted again.
func (r *Router) startStableTask(route Route, req Request, identity TaskIdentity) (*Task, bool, error) {
	claimKey := route.Name + "\x00" + identity.ID
	for {
		r.mu.Lock()
		if pending, ok := r.pendingIdentities[claimKey]; ok {
			r.mu.Unlock()
			<-pending
			continue
		}
		if _, ok := r.registry.Get(identity.ID); ok {
			r.mu.Unlock()
			task, _, claimErr := r.registry.ClaimForOwnerWithIdentity(
				req.Route,
				req.DecodedValue,
				req.Owner,
				normalizedTaskLifetime(route.Lifetime),
				identity,
			)
			return task, false, claimErr
		}
		pending := make(chan struct{})
		r.pendingIdentities[claimKey] = pending
		r.mu.Unlock()

		task, started, startErr := r.startTaskWithIdentity(route, req, identity)
		r.mu.Lock()
		delete(r.pendingIdentities, claimKey)
		close(pending)
		r.mu.Unlock()
		return task, started, startErr
	}
}

func (r *Router) startTaskWithIdentity(route Route, req Request, identity TaskIdentity) (*Task, bool, error) {
	metadata := buildTaskMetadata(route, req.DecodedValue)
	target := admissionTarget(route.Name, metadata)
	if err := r.admit(route, target); err != nil {
		return nil, false, err
	}
	task, created, err := r.registry.ClaimForOwnerWithIdentity(req.Route, req.DecodedValue, req.Owner, normalizedTaskLifetime(route.Lifetime), identity, metadata)
	if err != nil || !created {
		r.release(route.Name, target)
		return task, false, err
	}
	r.registry.recordStarted(task, route.RecordedRequest(req.RawRequest))
	r.startTrackedTask(route, task, req.Owner, target)
	return task, true, nil
}

// admit counts a task in before it is claimed, refusing a second task of an
// exclusive route or a second task on the same target. The counts are the
// task service's, so routers sharing it, such as a root job host's one per
// user, admit against each other.
func (r *Router) admit(route Route, target string) error {
	s := r.registry
	s.admitMu.Lock()
	defer s.admitMu.Unlock()
	if normalizedPolicy(route.Policy).Exclusive && s.activeByRoute[route.Name] > 0 {
		return fmt.Errorf("%w: %s", ErrDuplicateActive, route.Name)
	}
	if target != "" && s.activeTargets[target] > 0 {
		return fmt.Errorf("%w: %s is already running on this target", ErrDuplicateActive, route.Name)
	}
	s.markActiveLocked(route.Name, target)
	return nil
}

// admissionTarget names what a task works on, from its metadata identity, or
// returns "" when the route names nothing.
func admissionTarget(routeName string, metadata *TaskMetadata) string {
	if metadata == nil || len(metadata.Identity) == 0 {
		return ""
	}
	return routeName + "\x00" + strings.Join(metadata.Identity, "\x00")
}

// buildTaskMetadata returns the route's public metadata or, for a Mutation
// route that only names its target, that target as the admission identity.
func buildTaskMetadata(route Route, request any) *TaskMetadata {
	if route.Metadata == nil {
		if route.Target != nil {
			return &TaskMetadata{Identity: route.Target(request)}
		}
		return nil
	}
	value := route.Metadata(request)
	return &value
}

func normalizedPolicy(policy TaskPolicy) TaskPolicy {
	if policy.Name == "" {
		return TaskDefault
	}
	return policy
}

func normalizedTaskLifetime(lifetime TaskLifetime) TaskLifetime {
	if lifetime == "" {
		return TaskLifetimeSession
	}
	return lifetime
}

func (s *TaskService) markActiveLocked(routeName, target string) {
	s.activeByRoute[routeName]++
	if target != "" {
		s.activeTargets[target]++
	}
}

func (r *Router) release(routeName, target string) {
	s := r.registry
	s.admitMu.Lock()
	defer s.admitMu.Unlock()
	if s.activeByRoute[routeName] > 1 {
		s.activeByRoute[routeName]--
	} else {
		delete(s.activeByRoute, routeName)
	}
	if s.activeTargets[target] > 1 {
		s.activeTargets[target]--
	} else {
		delete(s.activeTargets, target)
	}
}

// startTrackedTask runs task and releases its admission only once its handler
// has exited, not when the task turns terminal: a canceled handler that
// ignores its context must not overlap a successor on the same target.
func (r *Router) startTrackedTask(route Route, task *Task, owner TaskOwner, target string) {
	executionDone := make(chan struct{})
	if !task.Start(r.routeRunner(route, executionDone)) {
		r.release(route.Name, target)
		return
	}
	go func() {
		goroutinelabel.WithSession(context.Background(), owner.SessionID, owner.UID,
			"component", "task-tracker",
			"route", route.Name,
			"task_id", task.ID(),
		)

		<-task.Done()
		<-executionDone
		r.release(route.Name, target)
	}()
}

func ownerFromSession(sess *session.Session) TaskOwner {
	if sess == nil {
		return TaskOwner{}
	}
	return TaskOwner{
		SessionID: sess.SessionID,
		Username:  sess.User.Username,
		UID:       sess.User.UID,
	}
}

// StatusCode is the wire status code of err: the bridgeipc.Error code when set, else a code for the known sentinels, else 500.
// failureLevel maps a failed route or task to its log level: server faults
// are errors, client mistakes (4xx) are warnings. Anything else, including
// exit codes below 400, is a failed operation someone must look at.
func failureLevel(err error) slog.Level {
	if code := StatusCode(err); code >= 400 && code < 500 {
		return slog.LevelWarn
	}
	return slog.LevelError
}

func StatusCode(err error) int {
	if err == nil {
		return 0
	}
	var taskErr *Error
	if errors.As(err, &taskErr) && taskErr.Code != 0 {
		return taskErr.Code
	}
	switch {
	case errors.Is(err, ErrInvalidArgs):
		return 400
	case errors.Is(err, ErrForbidden):
		return 403
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrRouteNotFound):
		return 404
	case errors.Is(err, ErrConflict):
		return 409
	case errors.Is(err, ErrDuplicateActive):
		return 409
	case errors.Is(err, ErrTaskIdentityConflict):
		return 409
	case errors.Is(err, context.DeadlineExceeded):
		return 504
	case errors.Is(err, context.Canceled):
		return 499
	default:
		return 500
	}
}
