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

	"github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type Mode string

const (
	ModeQuery  Mode = "query"
	ModeJob    Mode = "job"
	ModeDuplex Mode = "duplex"
)

const InitialJobSettleTimeout = 25 * time.Millisecond

var (
	ErrInvalidArgs      = errors.New("invalid arguments")
	ErrForbidden        = errors.New("forbidden")
	ErrRouteNotFound    = errors.New("route not found")
	ErrRateLimited      = errors.New("rate limit exceeded")
	ErrQueueFull        = errors.New("job queue full")
	ErrDuplicateActive  = errors.New("job already active")
	ErrReservedJobRoute = errors.New("reserved jobs route")
)

// NoRequest marks a route that takes no request payload.
type NoRequest struct{}

// NoResponse marks a route that returns no result payload.
type NoResponse struct{}

type HandlerFunc func(ctx context.Context, request any, emit Events) error
type DuplexFunc func(ctx context.Context, stream net.Conn, request any) error

type RequestDecoder func(raw json.RawMessage) (any, error)

type Events interface {
	Data(chunk []byte) error
	Progress(progress any) error
	Result(result any) error
	Error(err error, code int) error
	Close(reason string) error
}

type Request struct {
	Route        string
	RawRequest   json.RawMessage
	DecodedValue any
	Session      *session.Session
	Owner        Owner
}

type Route struct {
	Name       string
	Mode       Mode
	Handler    HandlerFunc
	Runner     Runner
	Duplex     DuplexFunc
	Privileged bool
	Policy     JobPolicy
	Decode     RequestDecoder
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

type JobPolicy struct {
	Name                    string
	MaxActivePerRoute       int
	MaxActivePerOwnerRoute  int
	QueueLimit              int
	StartRatePerMinuteOwner int
	// Timeout is the maximum runtime after a job starts. Queue time is not counted.
	// When it expires, bridgeipc cancels the runner context and fails the job with 504.
	Timeout               time.Duration
	DuplicateActiveReject bool
}

var (
	ActionDefault = JobPolicy{
		Name:                    "action_default",
		MaxActivePerRoute:       4,
		MaxActivePerOwnerRoute:  1,
		QueueLimit:              16,
		StartRatePerMinuteOwner: 10,
		Timeout:                 120 * time.Minute,
	}
	SingletonSystem = JobPolicy{
		Name:                    "singleton_system",
		MaxActivePerRoute:       1,
		MaxActivePerOwnerRoute:  1,
		QueueLimit:              1,
		StartRatePerMinuteOwner: 10,
		DuplicateActiveReject:   true,
	}
	StreamDefault = JobPolicy{
		Name:                    "stream_default",
		MaxActivePerRoute:       64,
		MaxActivePerOwnerRoute:  8,
		QueueLimit:              0,
		StartRatePerMinuteOwner: 30,
	}
)

type Router struct {
	mu                 sync.RWMutex
	routes             map[string]Route
	registry           *Registry
	activeByRoute      map[string]int
	activeByOwnerRoute map[string]int
	queuedByRoute      map[string][]queuedJob
	startsByOwnerRoute map[string][]time.Time
}

type queuedJob struct {
	route Route
	job   *Job
	owner Owner
}

type runnerResult struct {
	result any
	err    error
}

func NewRouter(registry *Registry) *Router {
	if registry == nil {
		registry = DefaultRegistry
	}
	return &Router{
		routes:             make(map[string]Route),
		registry:           registry,
		activeByRoute:      make(map[string]int),
		activeByOwnerRoute: make(map[string]int),
		queuedByRoute:      make(map[string][]queuedJob),
		startsByOwnerRoute: make(map[string][]time.Time),
	}
}

// Registry returns the job registry used by this router.
func (r *Router) Registry() *Registry {
	return r.registry
}

// Query registers a request-response route. The handler runs synchronously and
// its result is written back to the caller before the connection is closed.
func (r *Router) Query(name string, handler HandlerFunc, opts ...RouteOption) {
	r.register(Route{Name: name, Mode: ModeQuery, Handler: handler}, opts...)
}

// Job registers a background job route using a HandlerFunc. The handler emits
// progress and results through the Events interface. If policy.Name is empty,
// ActionDefault is used.
func (r *Router) Job(name string, handler HandlerFunc, policy JobPolicy, opts ...RouteOption) {
	if policy.Name == "" {
		policy = ActionDefault
	}
	r.register(Route{Name: name, Mode: ModeJob, Handler: handler, Policy: policy}, opts...)
}

// JobRunner registers a background job route using a Runner. Unlike Job, the
// runner receives the *Job directly, enabling lower-level control (e.g. calling
// ReportProgress). If policy.Name is empty, ActionDefault is used.
func (r *Router) JobRunner(name string, runner Runner, policy JobPolicy, opts ...RouteOption) {
	if policy.Name == "" {
		policy = ActionDefault
	}
	r.register(Route{Name: name, Mode: ModeJob, Runner: runner, Policy: policy}, opts...)
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

	if strings.HasPrefix(req.Route, "jobs.") {
		return r.dispatchJobPrimitive(ctx, stream, req)
	}

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
	case ModeQuery:
		err = r.dispatchQuery(ctx, stream, route, req.DecodedValue)
	case ModeJob:
		err = r.dispatchJob(ctx, stream, route, req)
	case ModeDuplex:
		err = route.Duplex(ctx, stream, req.DecodedValue)
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
	if route.Name == "" {
		panic("bridge route cannot be empty")
	}
	if strings.HasPrefix(route.Name, "jobs.") {
		panic("bridge route uses reserved jobs.* namespace: " + route.Name)
	}
	if route.Mode == ModeQuery && route.Handler == nil {
		panic("bridge route handler cannot be nil: " + route.Name)
	}
	if route.Mode == ModeJob && route.Handler == nil && route.Runner == nil {
		panic("bridge job route handler cannot be nil: " + route.Name)
	}
	if route.Mode == ModeDuplex && route.Duplex == nil {
		panic("bridge duplex route handler cannot be nil: " + route.Name)
	}
	for _, opt := range opts {
		opt(&route)
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

func (r *Router) dispatchQuery(ctx context.Context, stream net.Conn, route Route, request any) error {
	emit := newStreamEmitter(stream)
	err := route.Handler(ctx, request, emit)
	if err != nil {
		_ = emit.Error(err, statusCode(err))
	}
	_ = emit.Close("")
	return err
}

func (r *Router) dispatchJob(ctx context.Context, stream net.Conn, route Route, req Request) error {
	job, started, err := r.startOrQueueJob(route, req)
	if err != nil {
		_ = relay.WriteResultErrorAndClose(stream, 0, err.Error(), statusCode(err))
		return err
	}
	if started {
		select {
		case <-job.Done():
		case <-time.After(InitialJobSettleTimeout):
		case <-ctx.Done():
		}
	}
	return relay.WriteResultOKAndClose(stream, 0, job.Snapshot())
}

func (r *Router) routeRunner(route Route) Runner {
	return func(ctx context.Context, job *Job, request any) (any, error) {
		policy := normalizedPolicy(route.Policy)
		if policy.Timeout <= 0 {
			return r.runRoute(ctx, job, request, route)
		}

		runCtx, cancel := context.WithTimeout(ctx, policy.Timeout)
		defer cancel()

		done := make(chan runnerResult, 1)
		go func() {
			result, err := r.runRoute(runCtx, job, request, route)
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

func (r *Router) runRoute(ctx context.Context, job *Job, request any, route Route) (any, error) {
	if route.Runner != nil {
		return route.Runner(ctx, job, request)
	}
	emit := newJobEmitter(job)
	if err := route.Handler(ctx, request, emit); err != nil {
		return nil, err
	}
	return emit.result, nil
}

func (r *Router) startOrQueueJob(route Route, req Request) (*Job, bool, error) {
	now := time.Now().UTC()
	ownerKey := req.Owner.key()
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
	if !canStart && len(r.queuedByRoute[req.Route]) >= policy.QueueLimit {
		r.mu.Unlock()
		return nil, false, fmt.Errorf("%w: %s", ErrQueueFull, req.Route)
	}
	r.startsByOwnerRoute[ownerRouteKey] = append(r.startsByOwnerRoute[ownerRouteKey], now)
	r.mu.Unlock()

	job, err := r.registry.CreateForOwner(req.Route, req.DecodedValue, req.Owner)
	if err != nil {
		return nil, false, err
	}

	r.mu.Lock()
	if canStart {
		r.markActiveLocked(req.Route, ownerRouteKey)
		r.mu.Unlock()
		r.startTrackedJob(route, job, req.Owner)
		return job, true, nil
	}
	r.queuedByRoute[req.Route] = append(r.queuedByRoute[req.Route], queuedJob{route: route, job: job, owner: req.Owner})
	r.mu.Unlock()
	return job, false, nil
}

func normalizedPolicy(policy JobPolicy) JobPolicy {
	if policy.Name == "" {
		return ActionDefault
	}
	return policy
}

func (r *Router) checkRateLocked(ownerRouteKey string, policy JobPolicy, now time.Time) error {
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

func (r *Router) canStartLocked(routeName, ownerRouteKey string, policy JobPolicy) bool {
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

func (r *Router) startTrackedJob(route Route, job *Job, owner Owner) {
	ownerRouteKey := route.Name + "\x00" + owner.key()
	job.Start(r.routeRunner(route))
	go func() {
		<-job.Done()
		r.finishJob(route.Name, ownerRouteKey)
	}()
}

func (r *Router) finishJob(routeName, ownerRouteKey string) {
	var next *queuedJob
	r.mu.Lock()
	if r.activeByRoute[routeName] > 0 {
		r.activeByRoute[routeName]--
	}
	if r.activeByOwnerRoute[ownerRouteKey] > 0 {
		r.activeByOwnerRoute[ownerRouteKey]--
	}
	queue := r.queuedByRoute[routeName]
	for len(queue) > 0 {
		candidate := queue[0]
		queue = queue[1:]
		if candidate.job.IsTerminal() {
			continue
		}
		nextOwnerRouteKey := routeName + "\x00" + candidate.owner.key()
		if !r.canStartLocked(routeName, nextOwnerRouteKey, normalizedPolicy(candidate.route.Policy)) {
			queue = append([]queuedJob{candidate}, queue...)
			break
		}
		r.markActiveLocked(routeName, nextOwnerRouteKey)
		next = &candidate
		break
	}
	r.queuedByRoute[routeName] = queue
	r.mu.Unlock()

	if next != nil {
		r.startTrackedJob(next.route, next.job, next.owner)
	}
}

type streamEmitter struct {
	stream    net.Conn
	errorSent bool
}

func newStreamEmitter(stream net.Conn) *streamEmitter {
	return &streamEmitter{stream: stream}
}

func (e *streamEmitter) Data(chunk []byte) error {
	return relay.WriteRelayFrame(e.stream, &relay.StreamFrame{Opcode: relay.OpStreamData, Payload: chunk})
}

func (e *streamEmitter) Progress(progress any) error {
	return relay.WriteProgress(e.stream, 0, progress)
}

func (e *streamEmitter) Result(result any) error {
	return relay.WriteResultOK(e.stream, 0, result)
}

func (e *streamEmitter) Error(err error, code int) error {
	e.errorSent = true
	return relay.WriteResultError(e.stream, 0, err.Error(), code)
}

func (e *streamEmitter) Close(string) error {
	return relay.WriteStreamClose(e.stream, 0)
}

type jobEmitter struct {
	job    *Job
	result any
}

func newJobEmitter(job *Job) *jobEmitter {
	return &jobEmitter{job: job}
}

func (e *jobEmitter) Data(chunk []byte) error {
	e.job.ReportData(string(chunk))
	return nil
}

func (e *jobEmitter) Progress(progress any) error {
	e.job.ReportProgress(progress)
	return nil
}

func (e *jobEmitter) Result(result any) error {
	e.result = result
	return nil
}

func (e *jobEmitter) Error(err error, code int) error {
	return NewError(err.Error(), code)
}

func (e *jobEmitter) Close(string) error {
	return nil
}

func ownerFromSession(sess *session.Session) Owner {
	if sess == nil {
		return Owner{}
	}
	return Owner{
		SessionID: sess.SessionID,
		Username:  sess.User.Username,
		UID:       sess.User.UID,
	}
}

func (o Owner) key() string {
	if o.Username != "" {
		return o.Username
	}
	if o.UID != 0 {
		return fmt.Sprintf("uid:%d", o.UID)
	}
	if o.SessionID != "" {
		return "session:" + o.SessionID
	}
	return "anonymous"
}

func statusCode(err error) int {
	if err == nil {
		return 0
	}
	var jobErr *Error
	if errors.As(err, &jobErr) && jobErr.Code != 0 {
		return jobErr.Code
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

func EmitResult(emit Events, result any, err error) error {
	if err != nil {
		return err
	}
	return emit.Result(result)
}
