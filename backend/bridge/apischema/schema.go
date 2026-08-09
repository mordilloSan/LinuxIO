package apischema

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RouteSpec is the Go-side contract for one LinuxIO API route.
type RouteSpec struct {
	Route      string
	Mode       bridgeipc.Mode
	Kind       Kind
	Privileged bool
	NoEndpoint bool

	Request  TypeSpec
	Result   TypeSpec
	Progress TypeSpec

	Decode   bridgeipc.RequestDecoder
	Metadata bridgeipc.TaskMetadataBuilder
}

type RouteSpecOption func(*RouteSpec)

func Privileged() RouteSpecOption {
	return func(spec *RouteSpec) {
		spec.Privileged = true
	}
}

func NoEndpoint() RouteSpecOption {
	return func(spec *RouteSpec) {
		spec.NoEndpoint = true
	}
}

// WithTaskProgress declares the payload emitted by a task's progress frames.
// The result contract remains the task's terminal payload.
func WithTaskProgress[Progress any]() RouteSpecOption {
	return func(spec *RouteSpec) {
		spec.Progress = TypeOf[Progress]()
	}
}

type Route[Request, Result any] struct {
	spec RouteSpec
}

// TypedHandlerFunc is the ordinary handler shape: take a decoded request,
// return a result. Unlike HandlerFunc it binds Result, so a handler returning
// the wrong type is a compile error rather than a wire-format surprise.
type TypedHandlerFunc[Request, Result any] func(ctx context.Context, req Request) (Result, error)

// VoidHandlerFunc is TypedHandlerFunc for routes whose Result is NoResponse.
type VoidHandlerFunc[Request any] func(ctx context.Context, req Request) error

// HandlerFunc is the raw emitter shape, needed only by handlers that emit
// progress or data frames. Prefer TypedHandlerFunc.
type HandlerFunc[Request any] func(ctx context.Context, req Request, emit bridgeipc.Events) error
type TaskRunnerFunc[Request any] func(ctx context.Context, task *bridgeipc.Task, req Request) (any, error)
type DuplexFunc[Request any] func(ctx context.Context, stream net.Conn, req Request) error

// WithTaskMetadata declares a typed, safe request projection for a Task runner.
// Its generic request parameter prevents a route from accidentally reading an
// unrelated request model, while bridge snapshots remain free of raw requests.
func WithTaskMetadata[Request any](build func(Request) bridgeipc.TaskMetadata) RouteSpecOption {
	return func(spec *RouteSpec) {
		spec.Metadata = func(value any) bridgeipc.TaskMetadata {
			req, ok := value.(Request)
			if !ok {
				var zero Request
				panic(fmt.Sprintf("apischema: metadata for %s got %T, want %T", spec.Route, value, zero))
			}
			return build(req)
		}
	}
}

func Call[Request, Result any](name string, opts ...RouteSpecOption) Route[Request, Result] {
	return newRoute[Request, Result](KindHandler, bridgeipc.ModeCall, name, opts...)
}

func Task[Request, Result any](name string, opts ...RouteSpecOption) Route[Request, Result] {
	return newRoute[Request, Result](KindHandler, bridgeipc.ModeTask, name, opts...)
}

func TaskRunner[Request, Result any](name string, opts ...RouteSpecOption) Route[Request, Result] {
	return newRoute[Request, Result](KindTaskRunner, bridgeipc.ModeTask, name, opts...)
}

func DuplexRoute[Request, Result any](name string, opts ...RouteSpecOption) Route[Request, Result] {
	return newRoute[Request, Result](KindDuplex, bridgeipc.ModeDuplex, name, opts...)
}

func newRoute[Request, Result any](kind Kind, mode bridgeipc.Mode, name string, opts ...RouteSpecOption) Route[Request, Result] {
	spec := routeSpec(kind, mode, name, TypeOf[Request](), TypeOf[Result](), requestDecoder[Request](), opts...)
	if spec.Metadata != nil && (spec.Kind != KindTaskRunner || spec.Mode != bridgeipc.ModeTask) {
		panic(fmt.Sprintf("apischema: route %s metadata is allowed only on task runners", spec.Route))
	}
	if spec.Progress.GoType != nil && spec.Mode != bridgeipc.ModeTask {
		panic(fmt.Sprintf("apischema: route %s progress is allowed only on task routes", spec.Route))
	}
	return Route[Request, Result]{spec: spec}
}

func routeSpec(kind Kind, mode bridgeipc.Mode, route string, request TypeSpec, result TypeSpec, decode bridgeipc.RequestDecoder, opts ...RouteSpecOption) RouteSpec {
	spec := RouteSpec{
		Kind:    kind,
		Route:   route,
		Mode:    mode,
		Request: request,
		Result:  result,
		Decode:  decode,
	}
	for _, opt := range opts {
		opt(&spec)
	}
	return spec
}

type Kind string

const (
	KindHandler    Kind = "handler"
	KindTaskRunner Kind = "task_runner"
	KindDuplex     Kind = "duplex"
)

func (r RouteSpec) Handler() string {
	handler, _, _ := strings.Cut(r.Route, ".")
	return handler
}

func (r RouteSpec) Command() string {
	_, command, _ := strings.Cut(r.Route, ".")
	return command
}

func (r RouteSpec) Endpoint() bool {
	return r.Mode != bridgeipc.ModeDuplex && !r.NoEndpoint
}

func (r RouteSpec) RequestSpec() TypeSpec {
	return r.Request
}

func (r RouteSpec) ResultSpec() TypeSpec {
	return r.Result
}

func (r RouteSpec) ProgressSpec() (TypeSpec, bool) {
	return r.Progress, r.Progress.GoType != nil
}

// Handle binds the ordinary request-in/result-out handler for this route.
func (r Route[Request, Result]) Handle(handle TypedHandlerFunc[Request, Result], options ...bridgeipc.RouteOption) HandlerBinding {
	binding := HandlerBinding{
		Route:   r.spec,
		Decode:  r.spec.Decode,
		Options: options,
	}
	if r.spec.Mode == bridgeipc.ModeCall {
		binding.Call = wrapTypedCall(r.spec, handle)
	} else {
		binding.Handle = wrapTypedHandler(r.spec, handle)
	}
	return binding
}

// HandleVoid binds a handler for a route declared with a NoResponse result.
// It panics at binding time if Result is anything else, so the declaration and
// the handler cannot drift apart.
func (r Route[Request, Result]) HandleVoid(handle VoidHandlerFunc[Request], options ...bridgeipc.RouteOption) HandlerBinding {
	if !r.spec.Result.Void() {
		panic(fmt.Sprintf("apischema: route %s returns %s, so it cannot use HandleVoid", r.spec.Route, r.spec.Result.GoType))
	}
	binding := HandlerBinding{
		Route:   r.spec,
		Decode:  r.spec.Decode,
		Options: options,
	}
	if r.spec.Mode == bridgeipc.ModeCall {
		binding.Call = wrapVoidCall(r.spec.Route, handle)
	} else {
		binding.Handle = wrapVoidHandler(r.spec.Route, handle)
	}
	return binding
}

// HandleEvents binds a handler that needs the raw emitter — progress or data
// frames. Every other route should use Handle or HandleVoid.
func (r Route[Request, Result]) HandleEvents(handle HandlerFunc[Request], options ...bridgeipc.RouteOption) HandlerBinding {
	if r.spec.Mode == bridgeipc.ModeCall {
		panic(fmt.Sprintf("apischema: call route %s cannot use HandleEvents; calls return one result directly", r.spec.Route))
	}
	return HandlerBinding{
		Route:   r.spec,
		Decode:  r.spec.Decode,
		Handle:  wrapHandler(r.spec.Route, handle),
		Options: options,
	}
}

func (r Route[Request, Result]) Run(runner TaskRunnerFunc[Request], policy bridgeipc.TaskPolicy, options ...bridgeipc.RouteOption) TaskBinding {
	return TaskBinding{
		Route:   r.spec,
		Decode:  r.spec.Decode,
		Runner:  wrapTaskRunner(r.spec.Route, runner),
		Policy:  policy,
		Options: options,
	}
}

func (r Route[Request, Result]) Duplex(handle DuplexFunc[Request], options ...bridgeipc.RouteOption) DuplexBinding {
	return DuplexBinding{
		Route:   r.spec,
		Decode:  r.spec.Decode,
		Handle:  wrapDuplex(r.spec.Route, handle),
		Options: options,
	}
}

// HandlerBinding carries no TaskPolicy: HandleWithPolicy was its only setter and
// had zero call sites, so every handler-form task route has always run under
// TaskDefault. Only .Run (TaskBinding) chooses a policy. If a handler-form
// route ever needs one, the question to ask first is whether it should be a task
// at all.
type HandlerBinding struct {
	Route   RouteSpec
	Handle  bridgeipc.HandlerFunc
	Call    bridgeipc.CallFunc
	Decode  bridgeipc.RequestDecoder
	Options []bridgeipc.RouteOption
}

type TaskBinding struct {
	Route   RouteSpec
	Runner  bridgeipc.TaskRunner
	Decode  bridgeipc.RequestDecoder
	Policy  bridgeipc.TaskPolicy
	Options []bridgeipc.RouteOption
}

type DuplexBinding struct {
	Route   RouteSpec
	Handle  bridgeipc.DuplexFunc
	Decode  bridgeipc.RequestDecoder
	Options []bridgeipc.RouteOption
}

type Binding interface {
	addTo(*BindingSet)
}

type BindingSet struct {
	handlers []HandlerBinding
	tasks    []TaskBinding
	duplexes []DuplexBinding
	routes   []RouteSpec
}

func Bindings(bindings ...Binding) BindingSet {
	var set BindingSet
	for _, binding := range bindings {
		binding.addTo(&set)
	}
	return set
}

func CombineRoutes(groups ...[]RouteSpec) []RouteSpec {
	total := 0
	for _, group := range groups {
		total += len(group)
	}
	routes := make([]RouteSpec, 0, total)
	for _, group := range groups {
		routes = append(routes, group...)
	}
	return routes
}

func (s BindingSet) Routes() []RouteSpec {
	return append([]RouteSpec(nil), s.routes...)
}

func (s BindingSet) Register(router *bridgeipc.Router) {
	for _, binding := range s.handlers {
		AttachHandler(router, binding)
	}
	for _, binding := range s.tasks {
		AttachTask(router, binding)
	}
	for _, binding := range s.duplexes {
		AttachDuplex(router, binding)
	}
}

func (r Route[Request, Result]) addTo(set *BindingSet) {
	set.routes = append(set.routes, requireRouteSpec(r.spec))
}

func (b HandlerBinding) addTo(set *BindingSet) {
	set.handlers = append(set.handlers, b)
	set.routes = append(set.routes, requireRouteSpec(b.Route))
}

func (b TaskBinding) addTo(set *BindingSet) {
	set.tasks = append(set.tasks, b)
	set.routes = append(set.routes, requireRouteSpec(b.Route))
}

func (b DuplexBinding) addTo(set *BindingSet) {
	set.duplexes = append(set.duplexes, b)
	set.routes = append(set.routes, requireRouteSpec(b.Route))
}

func AttachHandler(router *bridgeipc.Router, binding HandlerBinding) {
	spec := requireRouteSpec(binding.Route)
	if spec.Kind != KindHandler {
		panic(fmt.Sprintf("apischema: route %s is %s, not handler", spec.Route, spec.Kind))
	}
	opts := routeOptions(spec, binding.Options)
	opts = append(opts, bridgeipc.WithRequestDecoder(requireDecoder(spec, binding.Decode)))
	switch spec.Mode {
	case bridgeipc.ModeCall:
		router.Call(spec.Route, binding.Call, opts...)
	case bridgeipc.ModeTask:
		router.Task(spec.Route, binding.Handle, bridgeipc.TaskDefault, opts...)
	default:
		panic(fmt.Sprintf("apischema: route %s is %s, not call/task", spec.Route, spec.Mode))
	}
}

func AttachTask(router *bridgeipc.Router, binding TaskBinding) {
	spec := requireRouteSpec(binding.Route)
	if spec.Kind != KindTaskRunner {
		panic(fmt.Sprintf("apischema: route %s is %s, not task runner", spec.Route, spec.Kind))
	}
	if spec.Mode != bridgeipc.ModeTask {
		panic(fmt.Sprintf("apischema: route %s is %s, not task", spec.Route, spec.Mode))
	}
	opts := routeOptions(spec, binding.Options)
	opts = append(opts, bridgeipc.WithRequestDecoder(requireDecoder(spec, binding.Decode)))
	router.TaskRunner(spec.Route, binding.Runner, taskPolicy(binding.Policy), opts...)
}

func AttachDuplex(router *bridgeipc.Router, binding DuplexBinding) {
	spec := requireRouteSpec(binding.Route)
	if spec.Kind != KindDuplex {
		panic(fmt.Sprintf("apischema: route %s is %s, not duplex", spec.Route, spec.Kind))
	}
	if spec.Mode != bridgeipc.ModeDuplex {
		panic(fmt.Sprintf("apischema: route %s is %s, not duplex", spec.Route, spec.Mode))
	}
	opts := routeOptions(spec, binding.Options)
	opts = append(opts, bridgeipc.WithRequestDecoder(requireDecoder(spec, binding.Decode)))
	router.Duplex(spec.Route, binding.Handle, opts...)
}

func requireRouteSpec(spec RouteSpec) RouteSpec {
	if spec.Route == "" {
		panic("apischema: route spec cannot be empty")
	}
	return spec
}

func requestDecoder[Request any]() bridgeipc.RequestDecoder {
	return func(raw json.RawMessage) (any, error) {
		if len(raw) == 0 || string(raw) == "null" {
			raw = json.RawMessage("{}")
		}
		var req Request
		if err := json.Unmarshal(raw, &req); err != nil {
			return nil, err
		}
		return req, nil
	}
}

func wrapTypedHandler[Request, Result any](spec RouteSpec, handle TypedHandlerFunc[Request, Result]) bridgeipc.HandlerFunc {
	// NoResponse generates TypeScript `void`, so it must stay off the wire:
	// emitting the zero struct would send `{}` to a `void` consumer and stop
	// task snapshots from omitting `result`. Read once per binding off the spec's
	// already-materialized result type — the same predicate the generator uses
	// to decide `void`.
	voidResult := spec.Result.Void()
	route := spec.Route
	return func(ctx context.Context, request any, emit bridgeipc.Events) error {
		req, err := typedRequest[Request](route, request)
		if err != nil {
			return err
		}
		result, err := handle(ctx, req)
		if err != nil {
			return err
		}
		if voidResult {
			return emit.Result(nil)
		}
		return emit.Result(result)
	}
}

func wrapTypedCall[Request, Result any](spec RouteSpec, handle TypedHandlerFunc[Request, Result]) bridgeipc.CallFunc {
	voidResult := spec.Result.Void()
	route := spec.Route
	return func(ctx context.Context, request any) (any, error) {
		req, err := typedRequest[Request](route, request)
		if err != nil {
			return nil, err
		}
		result, err := handle(ctx, req)
		if err != nil {
			return nil, err
		}
		if voidResult {
			return nil, nil
		}
		return result, nil
	}
}

func wrapVoidHandler[Request any](route string, handle VoidHandlerFunc[Request]) bridgeipc.HandlerFunc {
	return func(ctx context.Context, request any, emit bridgeipc.Events) error {
		req, err := typedRequest[Request](route, request)
		if err != nil {
			return err
		}
		if err := handle(ctx, req); err != nil {
			return err
		}
		return emit.Result(nil)
	}
}

func wrapVoidCall[Request any](route string, handle VoidHandlerFunc[Request]) bridgeipc.CallFunc {
	return func(ctx context.Context, request any) (any, error) {
		req, err := typedRequest[Request](route, request)
		if err != nil {
			return nil, err
		}
		return nil, handle(ctx, req)
	}
}

func wrapHandler[Request any](route string, handle HandlerFunc[Request]) bridgeipc.HandlerFunc {
	return func(ctx context.Context, request any, emit bridgeipc.Events) error {
		req, err := typedRequest[Request](route, request)
		if err != nil {
			return err
		}
		return handle(ctx, req, emit)
	}
}

func wrapTaskRunner[Request any](route string, runner TaskRunnerFunc[Request]) bridgeipc.TaskRunner {
	return func(ctx context.Context, task *bridgeipc.Task, request any) (any, error) {
		req, err := typedRequest[Request](route, request)
		if err != nil {
			return nil, err
		}
		return runner(ctx, task, req)
	}
}

func wrapDuplex[Request any](route string, handle DuplexFunc[Request]) bridgeipc.DuplexFunc {
	return func(ctx context.Context, stream net.Conn, request any) error {
		req, err := typedRequest[Request](route, request)
		if err != nil {
			return err
		}
		return handle(ctx, stream, req)
	}
}

func typedRequest[Request any](route string, request any) (Request, error) {
	req, ok := request.(Request)
	if ok {
		return req, nil
	}
	var zero Request
	return zero, fmt.Errorf("%w: %s decoded request is %T, want %T", bridgeipc.ErrInvalidArgs, route, request, zero)
}

func requireDecoder(spec RouteSpec, decode bridgeipc.RequestDecoder) bridgeipc.RequestDecoder {
	if decode == nil {
		panic(fmt.Sprintf("apischema: route %s has no request decoder", spec.Route))
	}
	return decode
}

func routeOptions(spec RouteSpec, explicit []bridgeipc.RouteOption) []bridgeipc.RouteOption {
	opts := append([]bridgeipc.RouteOption(nil), explicit...)
	if spec.Privileged {
		opts = append(opts, bridgeipc.Privileged)
	}
	if spec.Metadata != nil {
		opts = append(opts, bridgeipc.WithTaskMetadata(spec.Metadata))
	}
	return opts
}

func taskPolicy(explicit bridgeipc.TaskPolicy) bridgeipc.TaskPolicy {
	if explicit.Name != "" {
		return explicit
	}
	return bridgeipc.TaskDefault
}
