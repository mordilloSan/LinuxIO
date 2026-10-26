package bridgeipc

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/goroutinelabel"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type Mode string

const (
	ModeCall   Mode = "call"
	ModeTask   Mode = "task"
	ModeDuplex Mode = "duplex"
)

const InitialTaskSettleTimeout = 25 * time.Millisecond

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
	Identity   TaskIdentityBuilder
}

type RouteOption func(*Route)

func Privileged(r *Route) {
	r.Privileged = true
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

// WithTaskLifetime declares the owner scope for a Task route.
func WithTaskLifetime(lifetime TaskLifetime) RouteOption {
	return func(r *Route) {
		if lifetime != TaskLifetimeSession && lifetime != TaskLifetimeDurable {
			panic("bridge task route has invalid lifetime: " + string(lifetime))
		}
		r.Lifetime = lifetime
	}
}

// WithTaskIdentity declares a stable Task ID and safe request fingerprint.
// It is reserved for durable routes whose persistent operation store enforces
// the same identity across bridge processes.
func WithTaskIdentity(build TaskIdentityBuilder) RouteOption {
	return func(r *Route) {
		if build == nil {
			panic("bridge task identity builder cannot be nil")
		}
		r.Identity = build
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

type Router struct {
	mu                sync.RWMutex
	routes            map[string]Route
	registry          *TaskService
	activeByRoute     map[string]int
	activeTargets     map[string]int
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
		activeByRoute:     make(map[string]int),
		activeTargets:     make(map[string]int),
		pendingIdentities: make(map[string]chan struct{}),
	}
}

// TaskService returns the task service used by this router.
func (r *Router) TaskService() *TaskService {
	return r.registry
}

// RecoverDurableTask reattaches a persisted operation to its registered route
// without treating recovery as a new admission. The recovered Task counts as
// active so the exclusive and same-target checks still protect new starts.
func (r *Router) RecoverDurableTask(routeName string, request any, owner TaskOwner, identity TaskIdentity) (*Task, bool, error) {
	route, ok := r.lookup(routeName)
	if !ok || route.Mode != ModeTask || route.Lifetime != TaskLifetimeDurable || route.Identity == nil {
		return nil, false, fmt.Errorf("%w: durable task route %s", ErrRouteNotFound, routeName)
	}
	if owner.Username == "" || owner.SessionID == "" {
		return nil, false, fmt.Errorf("%w: durable task recovery requires an authenticated owner", ErrForbidden)
	}
	expected, err := route.Identity(request)
	if err != nil {
		return nil, false, err
	}
	if identity.ID == "" || identity.Fingerprint == "" || expected != identity {
		return nil, false, fmt.Errorf("%w: durable task recovery identity mismatch", ErrInvalidArgs)
	}
	metadata := buildTaskMetadata(route, request)
	task, created, err := r.registry.ClaimForOwnerWithIdentity(routeName, request, owner, TaskLifetimeDurable, identity, metadata)
	if err != nil || !created {
		return task, created, err
	}
	target := admissionTarget(routeName, metadata)
	r.mu.Lock()
	r.markActiveLocked(routeName, target)
	r.mu.Unlock()
	r.startTrackedTask(route, task, owner, target)
	return task, true, nil
}

// Call registers a bounded request-response route. The handler returns one
// result directly; the router writes its result or error frame and closes the
// stream.
func (r *Router) Call(name string, handler CallFunc, opts ...RouteOption) {
	r.register(Route{Name: name, Mode: ModeCall, Call: handler}, opts...)
}

// TaskRunner registers a background task route. The runner receives the *Task
// directly for progress reporting. If policy.Name is empty, TaskDefault is used.
func (r *Router) TaskRunner(name string, runner TaskRunner, policy TaskPolicy, opts ...RouteOption) {
	if policy.Name == "" {
		policy = TaskDefault
	}
	r.register(Route{Name: name, Mode: ModeTask, Runner: runner, Policy: policy}, opts...)
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
	case ModeTask:
		err = r.dispatchTask(ctx, stream, route, req)
	case ModeDuplex:
		err = route.Duplex(ctx, stream, req)
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
	switch route.Mode {
	case ModeTask:
		if route.Lifetime == "" {
			route.Lifetime = TaskLifetimeSession
		}
		validateTaskRouteOptions(route)
	default:
		validateNonTaskRouteOptions(route)
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
	case ModeTask:
		if route.Runner == nil {
			panic("bridge task route handler cannot be nil: " + route.Name)
		}
	case ModeDuplex:
		if route.Duplex == nil {
			panic("bridge duplex route handler cannot be nil: " + route.Name)
		}
	default:
		panic("bridge route has invalid mode: " + string(route.Mode))
	}
}

func validateTaskRouteOptions(route Route) {
	if route.Lifetime != TaskLifetimeSession && route.Lifetime != TaskLifetimeDurable {
		panic("bridge task route has invalid lifetime: " + string(route.Lifetime))
	}
	if route.Identity != nil && route.Lifetime != TaskLifetimeDurable {
		panic("bridge stable task identity requires durable lifetime: " + route.Name)
	}
}

func validateNonTaskRouteOptions(route Route) {
	if route.Lifetime != "" {
		panic("bridge task lifetime is allowed only on task routes: " + route.Name)
	}
	if route.Identity != nil {
		panic("bridge task identity is allowed only on task routes: " + route.Name)
	}
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

func (r *Router) dispatchTask(ctx context.Context, stream net.Conn, route Route, req Request) error {
	task, started, err := r.startTask(route, req)
	if err != nil {
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), StatusCode(err))
		return err
	}
	if started {
		select {
		case <-task.Done():
		case <-time.After(InitialTaskSettleTimeout):
		case <-ctx.Done():
		}
	}
	return relay.WriteResultOKAndClose(stream, 0, task.Snapshot())
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

func (r *Router) startTask(route Route, req Request) (*Task, bool, error) {
	if route.Identity == nil {
		return r.startTaskWithIdentity(route, req, TaskIdentity{})
	}
	identity, err := route.Identity(req.DecodedValue)
	if err != nil {
		return nil, false, err
	}
	if identity.ID == "" || identity.Fingerprint == "" {
		return nil, false, fmt.Errorf("%w: durable task identity is incomplete", ErrInvalidArgs)
	}

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
	r.startTrackedTask(route, task, req.Owner, target)
	return task, true, nil
}

// admit counts a task in before it is claimed, refusing a second task of an
// exclusive route or a second task on the same target.
func (r *Router) admit(route Route, target string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if normalizedPolicy(route.Policy).Exclusive && r.activeByRoute[route.Name] > 0 {
		return fmt.Errorf("%w: %s", ErrDuplicateActive, route.Name)
	}
	if target != "" && r.activeTargets[target] > 0 {
		return fmt.Errorf("%w: %s is already running on this target", ErrDuplicateActive, route.Name)
	}
	r.markActiveLocked(route.Name, target)
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

func buildTaskMetadata(route Route, request any) *TaskMetadata {
	if route.Metadata == nil {
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

func (r *Router) markActiveLocked(routeName, target string) {
	r.activeByRoute[routeName]++
	if target != "" {
		r.activeTargets[target]++
	}
}

func (r *Router) release(routeName, target string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.activeByRoute[routeName] > 1 {
		r.activeByRoute[routeName]--
	} else {
		delete(r.activeByRoute, routeName)
	}
	if r.activeTargets[target] > 1 {
		r.activeTargets[target]--
	} else {
		delete(r.activeTargets, target)
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
