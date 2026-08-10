package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/goroutinelabel"
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
	ErrRouteNotFound   = errors.New("route not found")
	ErrRateLimited     = errors.New("rate limit exceeded")
	ErrQueueFull       = errors.New("task queue full")
	ErrDuplicateActive = errors.New("task already active")
)

type CallFunc func(ctx context.Context, request Request) (any, error)
type DuplexFunc func(ctx context.Context, stream net.Conn, request Request) error

type RequestDecoder func(raw json.RawMessage) (any, error)

type Request struct {
	Route        string
	RawRequest   json.RawMessage
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

type TaskPolicy struct {
	Name                    string
	MaxActivePerRoute       int
	MaxActivePerOwnerRoute  int
	QueueLimit              int
	StartRatePerMinuteOwner int
	// Timeout is the maximum runtime after a task starts. Queue time is not counted.
	// When it expires, bridgeipc cancels the runner context and fails the task with 504.
	Timeout               time.Duration
	DuplicateActiveReject bool
}

var (
	TaskDefault = TaskPolicy{
		Name:                   "action_default",
		MaxActivePerRoute:      4,
		MaxActivePerOwnerRoute: 1,
		QueueLimit:             16,
		// StartRatePerMinuteOwner is 0 (disabled): copy/move/delete and other
		// action tasks are user-initiated with one task per item, so a per-minute
		// start cap rejected large multi-selections mid-batch. The frontend
		// runs these sequentially and MaxActivePerOwnerRoute=1 still serializes
		// execution, so there is no runaway-task risk.
		StartRatePerMinuteOwner: 0,
		Timeout:                 120 * time.Minute,
	}
	TaskSingletonSystem = TaskPolicy{
		Name:                    "singleton_system",
		MaxActivePerRoute:       1,
		MaxActivePerOwnerRoute:  1,
		QueueLimit:              1,
		StartRatePerMinuteOwner: 10,
		DuplicateActiveReject:   true,
	}
	TaskStreamDefault = TaskPolicy{
		Name:                   "stream_default",
		MaxActivePerRoute:      64,
		MaxActivePerOwnerRoute: 8,
		QueueLimit:             0,
		// StartRatePerMinuteOwner is 0 (disabled): file transfers are
		// user-initiated with one task per file, so a per-minute start cap
		// rejected large folder uploads mid-batch. Concurrency is still
		// bounded by MaxActivePerRoute / MaxActivePerOwnerRoute.
		StartRatePerMinuteOwner: 0,
	}
)

type Router struct {
	mu                   sync.RWMutex
	routes               map[string]Route
	registry             *TaskService
	activeByRoute        map[string]int
	activeByOwnerRoute   map[string]int
	queuedByRoute        map[string][]queuedTask
	pendingQueuedByRoute map[string]int
	startsByOwnerRoute   map[string][]time.Time
	// beforeStartHook is a narrow test seam for the promotion/cancel race.
	// Production never sets it.
	beforeStartHook func(*Task)
}

type queuedTask struct {
	route Route
	task  *Task
	owner TaskOwner
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
		routes:               make(map[string]Route),
		registry:             registry,
		activeByRoute:        make(map[string]int),
		activeByOwnerRoute:   make(map[string]int),
		queuedByRoute:        make(map[string][]queuedTask),
		pendingQueuedByRoute: make(map[string]int),
		startsByOwnerRoute:   make(map[string][]time.Time),
	}
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
func (r *Router) Dispatch(ctx context.Context, stream net.Conn, req Request) error {
	req.Owner = ownerFromSession(req.Session)

	route, ok := r.lookup(req.Route)
	if !ok {
		err := fmt.Errorf("%w: %s", ErrRouteNotFound, req.Route)
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), statusCode(err))
		return err
	}
	if route.Privileged && (req.Session == nil || !req.Session.Privileged) {
		err := fmt.Errorf("%w: privileged route %s requires elevated bridge", ErrForbidden, req.Route)
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), statusCode(err))
		return err
	}
	if route.Decode != nil {
		decoded, err := route.Decode(req.RawRequest)
		if err != nil {
			err = fmt.Errorf("%w: %s: %v", ErrInvalidArgs, req.Route, err)
			_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), statusCode(err))
			return err
		}
		req.DecodedValue = decoded
	}

	startedAt := time.Now()
	slog.Debug("route started",
		"route", req.Route,
		"mode", route.Mode,
		"user", req.Owner.Username)

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

	outcome := "success"
	if err != nil {
		outcome = "failure"
	}
	slog.Debug("route completed",
		"route", req.Route,
		"mode", route.Mode,
		"outcome", outcome,
		"duration", time.Since(startedAt),
		"error", err)
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
	if route.Mode == ModeCall && route.Call == nil {
		panic("bridge call route handler cannot be nil: " + route.Name)
	}
	if route.Mode == ModeTask && route.Runner == nil {
		panic("bridge task route handler cannot be nil: " + route.Name)
	}
	if route.Mode == ModeDuplex && route.Duplex == nil {
		panic("bridge duplex route handler cannot be nil: " + route.Name)
	}
	for _, opt := range opts {
		opt(&route)
	}
	if route.Mode == ModeTask {
		if route.Lifetime == "" {
			route.Lifetime = TaskLifetimeSession
		}
		if route.Lifetime != TaskLifetimeSession && route.Lifetime != TaskLifetimeDurable {
			panic("bridge task route has invalid lifetime: " + string(route.Lifetime))
		}
	} else if route.Lifetime != "" {
		panic("bridge task lifetime is allowed only on task routes: " + route.Name)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.routes[route.Name]; exists {
		panic("bridge route already registered: " + route.Name)
	}
	r.routes[route.Name] = route
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
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), statusCode(err))
		return err
	}
	return relay.WriteResultOKAndClose(stream, 0, result)
}

func (r *Router) dispatchTask(ctx context.Context, stream net.Conn, route Route, req Request) error {
	task, started, err := r.startOrQueueTask(route, req)
	if err != nil {
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), statusCode(err))
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

// routeRunner reports physical completion through executionDone. A task can
// become terminal before its handler returns when a timed or canceled handler
// ignores its context, so admission accounting must wait for this signal rather
// than relying on Task.Done alone.
func (r *Router) routeRunner(route Route, executionDone chan<- struct{}) TaskRunner {
	return func(ctx context.Context, task *Task, request any) (any, error) {
		policy := normalizedPolicy(route.Policy)
		if policy.Timeout <= 0 {
			defer close(executionDone)
			return r.runRoute(ctx, task, request, route)
		}

		runCtx, cancel := context.WithTimeout(ctx, policy.Timeout)
		defer cancel()

		done := make(chan runnerResult, 1)
		go func() {
			defer close(executionDone)
			result, err := r.runRoute(runCtx, task, request, route)
			done <- runnerResult{result: result, err: err}
		}()

		select {
		case result := <-done:
			return result.result, result.err
		case <-runCtx.Done():
			if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
				return nil, NewError("operation timed out", 504)
			}
			return nil, runCtx.Err()
		}
	}
}

func (r *Router) runRoute(ctx context.Context, task *Task, request any, route Route) (any, error) {
	return route.Runner(ctx, task, request)
}

func (r *Router) startOrQueueTask(route Route, req Request) (*Task, bool, error) {
	now := time.Now().UTC()
	lifetime := normalizedTaskLifetime(route.Lifetime)
	ownerKey := req.Owner.key(lifetime)
	ownerRouteKey := req.Route + "\x00" + ownerKey
	policy := normalizedPolicy(route.Policy)

	r.mu.Lock()
	if err := r.checkRateLocked(ownerRouteKey, policy, now); err != nil {
		r.mu.Unlock()
		return nil, false, err
	}
	if policy.DuplicateActiveReject && r.activeByRoute[req.Route] > 0 {
		r.mu.Unlock()
		return nil, false, fmt.Errorf("%w: %s", ErrDuplicateActive, req.Route)
	}

	canStart := r.canStartLocked(req.Route, ownerRouteKey, policy)
	if !canStart && policy.QueueLimit <= 0 {
		r.mu.Unlock()
		return nil, false, fmt.Errorf("%w: %s", ErrQueueFull, req.Route)
	}
	if !canStart && len(r.queuedByRoute[req.Route])+r.pendingQueuedByRoute[req.Route] >= policy.QueueLimit {
		r.mu.Unlock()
		return nil, false, fmt.Errorf("%w: %s", ErrQueueFull, req.Route)
	}
	// checkRateLocked prunes this history, but only when the owner rate limit
	// is enabled — with it disabled the append would grow unbounded, so skip it.
	if policy.StartRatePerMinuteOwner > 0 {
		r.startsByOwnerRoute[ownerRouteKey] = append(r.startsByOwnerRoute[ownerRouteKey], now)
	}
	// Reserve an active slot before creating the task. CreateForOwner can take
	// long enough for another request to otherwise observe stale capacity.
	if canStart {
		r.markActiveLocked(req.Route, ownerRouteKey)
	} else {
		// Queue capacity is also reserved before CreateForOwner so concurrent
		// admission cannot overfill a bounded queue.
		r.pendingQueuedByRoute[req.Route]++
	}
	r.mu.Unlock()

	var metadata *TaskMetadata
	if route.Metadata != nil {
		value := route.Metadata(req.DecodedValue)
		metadata = &value
	}
	task, err := r.registry.CreateForOwnerWithLifetime(req.Route, req.DecodedValue, req.Owner, lifetime, metadata)
	if err != nil {
		if canStart {
			// Releasing the reserved slot and promoting the next queued task is
			// exactly the finished-task path.
			r.finishTask(req.Route, ownerRouteKey)
		} else {
			r.mu.Lock()
			if r.pendingQueuedByRoute[req.Route] > 0 {
				r.pendingQueuedByRoute[req.Route]--
			}
			r.mu.Unlock()
		}
		return nil, false, err
	}

	r.mu.Lock()
	if canStart {
		r.mu.Unlock()
		r.startTrackedTask(route, task, req.Owner)
		return task, true, nil
	}
	r.queuedByRoute[req.Route] = append(r.queuedByRoute[req.Route], queuedTask{route: route, task: task, owner: req.Owner})
	if r.pendingQueuedByRoute[req.Route] > 0 {
		r.pendingQueuedByRoute[req.Route]--
	}
	// An active task may have finished while CreateForOwner was running. Promote
	// from the real FIFO queue now so this reservation cannot strand the task.
	next := r.dequeueStartLocked(req.Route)
	r.mu.Unlock()
	if next != nil {
		r.startTrackedTask(next.route, next.task, next.owner)
		return task, next.task == task, nil
	}
	return task, false, nil
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

func (r *Router) checkRateLocked(ownerRouteKey string, policy TaskPolicy, now time.Time) error {
	if policy.StartRatePerMinuteOwner <= 0 {
		return nil
	}
	cutoff := now.Add(-time.Minute)
	starts := r.startsByOwnerRoute[ownerRouteKey]
	kept := starts[:0]
	for _, started := range starts {
		if started.After(cutoff) {
			kept = append(kept, started)
		}
	}
	r.startsByOwnerRoute[ownerRouteKey] = kept
	if len(kept) >= policy.StartRatePerMinuteOwner {
		return fmt.Errorf("%w: %s", ErrRateLimited, ownerRouteKey)
	}
	return nil
}

func (r *Router) canStartLocked(routeName, ownerRouteKey string, policy TaskPolicy) bool {
	if policy.MaxActivePerRoute > 0 && r.activeByRoute[routeName] >= policy.MaxActivePerRoute {
		return false
	}
	if policy.MaxActivePerOwnerRoute > 0 && r.activeByOwnerRoute[ownerRouteKey] >= policy.MaxActivePerOwnerRoute {
		return false
	}
	return true
}

func (r *Router) markActiveLocked(routeName, ownerRouteKey string) {
	r.activeByRoute[routeName]++
	r.activeByOwnerRoute[ownerRouteKey]++
}

func (r *Router) unmarkActiveLocked(routeName, ownerRouteKey string) {
	if r.activeByRoute[routeName] > 0 {
		r.activeByRoute[routeName]--
	}
	if r.activeByOwnerRoute[ownerRouteKey] > 0 {
		r.activeByOwnerRoute[ownerRouteKey]--
	}
}

func (r *Router) startTrackedTask(route Route, task *Task, owner TaskOwner) {
	ownerRouteKey := route.Name + "\x00" + owner.key(normalizedTaskLifetime(route.Lifetime))
	if r.beforeStartHook != nil {
		r.beforeStartHook(task)
	}
	executionDone := make(chan struct{})
	if !task.Start(r.routeRunner(route, executionDone)) {
		r.finishTask(route.Name, ownerRouteKey)
		return
	}
	go func() {
		// Set explicitly, not inherited: a queued task is promoted from
		// finishTask, so this goroutine may be spawned by the waiter of an
		// unrelated task and would otherwise carry that task's labels.
		goroutinelabel.With(context.Background(),
			"route", route.Name,
			"task_id", task.ID(),
			"session_id", owner.SessionID,
			"user", owner.Username,
		)

		<-task.Done()
		<-executionDone
		r.finishTask(route.Name, ownerRouteKey)
	}()
}

func (r *Router) finishTask(routeName, ownerRouteKey string) {
	r.mu.Lock()
	r.unmarkActiveLocked(routeName, ownerRouteKey)
	next := r.dequeueStartLocked(routeName)
	r.mu.Unlock()

	if next != nil {
		r.startTrackedTask(next.route, next.task, next.owner)
	}
}

// dequeueStartLocked promotes the oldest runnable queued task for a route.
// The caller starts it after releasing r.mu to avoid lock-order surprises.
func (r *Router) dequeueStartLocked(routeName string) *queuedTask {
	var next *queuedTask
	queue := r.queuedByRoute[routeName]
	for len(queue) > 0 {
		candidate := queue[0]
		queue = queue[1:]
		if candidate.task.IsTerminal() {
			continue
		}
		nextOwnerRouteKey := routeName + "\x00" + candidate.owner.key(normalizedTaskLifetime(candidate.route.Lifetime))
		if !r.canStartLocked(routeName, nextOwnerRouteKey, normalizedPolicy(candidate.route.Policy)) {
			queue = append([]queuedTask{candidate}, queue...)
			break
		}
		r.markActiveLocked(routeName, nextOwnerRouteKey)
		next = &candidate
		break
	}
	r.queuedByRoute[routeName] = queue
	return next
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

func (o TaskOwner) key(lifetime TaskLifetime) string {
	switch normalizedTaskLifetime(lifetime) {
	case TaskLifetimeDurable:
		if o.Username != "" {
			return fmt.Sprintf("uid:%d", o.UID)
		}
		return "durable:anonymous"
	case TaskLifetimeSession:
		if o.SessionID != "" {
			return "session:" + o.SessionID
		}
		return "session:anonymous"
	default:
		return "anonymous"
	}
}

func statusCode(err error) int {
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
	case errors.Is(err, ErrRouteNotFound):
		return 404
	case errors.Is(err, ErrRateLimited):
		return 429
	case errors.Is(err, ErrQueueFull):
		return 429
	case errors.Is(err, ErrDuplicateActive):
		return 409
	case errors.Is(err, context.DeadlineExceeded):
		return 504
	case errors.Is(err, context.Canceled):
		return 499
	default:
		return 500
	}
}
