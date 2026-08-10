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
	pendingIdentities    map[string]chan struct{}
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
		pendingIdentities:    make(map[string]chan struct{}),
	}
}

// TaskService returns the task service used by this router.
func (r *Router) TaskService() *TaskService {
	return r.registry
}

// RecoverDurableTask reattaches a persisted operation to its registered route
// without treating recovery as a new admission. The recovered Task counts as
// active so normal singleton and capacity policy still protects new starts.
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
	ownerRouteKey := routeName + "\x00" + owner.key(TaskLifetimeDurable)
	r.mu.Lock()
	r.markActiveLocked(routeName, ownerRouteKey)
	r.mu.Unlock()
	r.startTrackedTask(route, task, owner)
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
	if route.Identity == nil {
		return r.startOrQueueTaskWithIdentity(route, req, TaskIdentity{})
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

		task, started, startErr := r.startOrQueueTaskWithIdentity(route, req, identity)
		r.mu.Lock()
		delete(r.pendingIdentities, claimKey)
		close(pending)
		r.mu.Unlock()
		return task, started, startErr
	}
}

func (r *Router) startOrQueueTaskWithIdentity(route Route, req Request, identity TaskIdentity) (*Task, bool, error) {
	now := time.Now().UTC()
	lifetime := normalizedTaskLifetime(route.Lifetime)
	ownerRouteKey := req.Route + "\x00" + req.Owner.key(lifetime)
	policy := normalizedPolicy(route.Policy)
	canStart, err := r.reserveTaskAdmission(req.Route, ownerRouteKey, policy, now)
	if err != nil {
		return nil, false, err
	}
	metadata := buildTaskMetadata(route, req.DecodedValue)
	task, created, err := r.registry.ClaimForOwnerWithIdentity(req.Route, req.DecodedValue, req.Owner, lifetime, identity, metadata)
	if err != nil {
		r.releaseTaskReservation(req.Route, ownerRouteKey, canStart)
		return nil, false, err
	}
	if !created {
		r.releaseTaskReservation(req.Route, ownerRouteKey, canStart)
		return task, false, nil
	}
	return r.startOrEnqueueClaimedTask(route, req.Owner, task, ownerRouteKey, canStart)
}

func (r *Router) reserveTaskAdmission(routeName, ownerRouteKey string, policy TaskPolicy, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.checkRateLocked(ownerRouteKey, policy, now); err != nil {
		return false, err
	}
	if policy.DuplicateActiveReject && r.activeByRoute[routeName] > 0 {
		return false, fmt.Errorf("%w: %s", ErrDuplicateActive, routeName)
	}
	canStart := r.canStartLocked(routeName, ownerRouteKey, policy)
	if !canStart && (policy.QueueLimit <= 0 || len(r.queuedByRoute[routeName])+r.pendingQueuedByRoute[routeName] >= policy.QueueLimit) {
		return false, fmt.Errorf("%w: %s", ErrQueueFull, routeName)
	}
	if policy.StartRatePerMinuteOwner > 0 {
		r.startsByOwnerRoute[ownerRouteKey] = append(r.startsByOwnerRoute[ownerRouteKey], now)
	}
	if canStart {
		r.markActiveLocked(routeName, ownerRouteKey)
	} else {
		r.pendingQueuedByRoute[routeName]++
	}
	return canStart, nil
}

func buildTaskMetadata(route Route, request any) *TaskMetadata {
	if route.Metadata == nil {
		return nil
	}
	value := route.Metadata(request)
	return &value
}

func (r *Router) releaseTaskReservation(routeName, ownerRouteKey string, canStart bool) {
	if canStart {
		r.finishTask(routeName, ownerRouteKey)
		return
	}
	r.mu.Lock()
	if r.pendingQueuedByRoute[routeName] > 0 {
		r.pendingQueuedByRoute[routeName]--
	}
	r.mu.Unlock()
}

func (r *Router) startOrEnqueueClaimedTask(route Route, owner TaskOwner, task *Task, ownerRouteKey string, canStart bool) (*Task, bool, error) {

	r.mu.Lock()
	if canStart {
		r.mu.Unlock()
		r.startTrackedTask(route, task, owner)
		return task, true, nil
	}
	r.queuedByRoute[route.Name] = append(r.queuedByRoute[route.Name], queuedTask{route: route, task: task, owner: owner})
	if r.pendingQueuedByRoute[route.Name] > 0 {
		r.pendingQueuedByRoute[route.Name]--
	}
	// An active task may have finished while CreateForOwner was running. Promote
	// from the real FIFO queue now so this reservation cannot strand the task.
	next := r.dequeueStartLocked(route.Name)
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
