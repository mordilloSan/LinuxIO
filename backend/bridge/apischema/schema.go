package apischema

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net"
	"reflect"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// RouteSpec is the Go-side contract for one LinuxIO API route.
type RouteSpec struct {
	Route        string
	Mode         bridgeipc.Mode
	Kind         Kind
	Privileged   bool
	NoEndpoint   bool
	RetrySafe    bool
	TaskLifetime bridgeipc.TaskLifetime
	BrowserBound bool
	Exclusive    bool
	// TaskClient: the frontend starts this Mutation with its task client,
	// which takes the job's first snapshot and follows it with tasks.watch.
	TaskClient bool
	Resumable  bool
	Targeted   bool
	// Secrets are the request members tagged `secret:"true"`, which the job
	// record leaves out.
	Secrets []string

	Request  TypeSpec
	Result   TypeSpec
	Progress TypeSpec

	Decode   bridgeipc.RequestDecoder
	Metadata bridgeipc.TaskMetadataBuilder
	Identity bridgeipc.TaskIdentityBuilder
}

type RouteSpecOption func(*RouteSpec)

func Privileged() RouteSpecOption {
	return func(spec *RouteSpec) {
		spec.Privileged = true
	}
}

// RetrySafe declares that repeating this Call after connection loss cannot
// cause a user-visible mutation. Calls default to no retry.
func RetrySafe() RouteSpecOption {
	return func(spec *RouteSpec) { spec.RetrySafe = true }
}

// WithTaskLifetime declares how long a Task remains owned by its caller.
// Lifetime declarations are valid only for Task routes.
func WithTaskLifetime(lifetime bridgeipc.TaskLifetime) RouteSpecOption {
	return func(spec *RouteSpec) { spec.TaskLifetime = lifetime }
}

// SessionTask declares a Task whose lifetime is bound to the owning session.
func SessionTask() RouteSpecOption {
	return WithTaskLifetime(bridgeipc.TaskLifetimeSession)
}

// DurableTask declares a Task whose lifetime is independent of one session.
func DurableTask() RouteSpecOption {
	return WithTaskLifetime(bridgeipc.TaskLifetimeDurable)
}

func NoEndpoint() RouteSpecOption {
	return func(spec *RouteSpec) {
		spec.NoEndpoint = true
	}
}

// BrowserBound keeps a Mutation in the bridge because its bytes come from or
// go to the browser.
func BrowserBound() RouteSpecOption {
	return func(spec *RouteSpec) { spec.BrowserBound = true }
}

// Exclusive runs one job of a Mutation at a time, whatever its target, for
// work that cannot overlap itself: a package transaction, a LinuxIO update.
func Exclusive() RouteSpecOption {
	return func(spec *RouteSpec) { spec.Exclusive = true }
}

// Progress declares the route-specific detail carried by the common
// TaskProgress envelope. The result contract remains the terminal payload.
func Progress[Detail bridgeipc.ProgressDetail]() RouteSpecOption {
	return func(spec *RouteSpec) {
		spec.Progress = TypeOf[Detail]()
	}
}

// WithTaskProgress is the Task route name for Progress.
func WithTaskProgress[Detail bridgeipc.ProgressDetail]() RouteSpecOption {
	return Progress[Detail]()
}

type Route[Request, Result any] struct {
	spec RouteSpec
}

// TypedHandlerFunc is the ordinary call shape: take a decoded request and
// return a result whose type is bound to the route.
type TypedHandlerFunc[Request, Result any] func(ctx context.Context, req Request) (Result, error)

// VoidHandlerFunc is TypedHandlerFunc for routes whose Result is NoResponse.
type VoidHandlerFunc[Request any] func(ctx context.Context, req Request) error

type TaskRunnerFunc[Request, Result any] func(ctx context.Context, task *bridgeipc.Task, req Request) (Result, error)
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

// WithTaskIdentity declares the canonical ID and safe request fingerprint for
// a durable Task. The typed wrapper prevents identity logic from accidentally
// inspecting another route's request model.
func WithTaskIdentity[Request any](build func(Request) (bridgeipc.TaskIdentity, error)) RouteSpecOption {
	return func(spec *RouteSpec) {
		if build == nil {
			panic("apischema: task identity builder cannot be nil")
		}
		spec.Identity = func(value any) (bridgeipc.TaskIdentity, error) {
			req, ok := value.(Request)
			if !ok {
				var zero Request
				panic(fmt.Sprintf("apischema: identity for %s got %T, want %T", spec.Route, value, zero))
			}
			return build(req)
		}
	}
}

func Call[Request, Result any](name string, opts ...RouteSpecOption) Route[Request, Result] {
	return newRoute[Request, Result](KindHandler, bridgeipc.ModeCall, name, opts...)
}

func TaskRunner[Request, Result any](name string, opts ...RouteSpecOption) Route[Request, Result] {
	return newRoute[Request, Result](KindTaskRunner, bridgeipc.ModeTask, name, opts...)
}

func DuplexRoute[Request, Result any](name string, opts ...RouteSpecOption) Route[Request, Result] {
	return newRoute[Request, Result](KindDuplex, bridgeipc.ModeDuplex, name, opts...)
}

// Targeted is implemented by a Mutation request type that names what the job
// works on, such as a pool UUID or a device, so the job host refuses a second
// run on the same target.
type Targeted interface {
	Target() []string
}

// secretMembers names the JSON members of t's fields tagged `secret:"true"`,
// with those of its embedded structs.
func secretMembers(t reflect.Type) []string {
	if t.Kind() != reflect.Struct {
		return nil
	}
	var names []string
	for field := range t.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		switch {
		case field.Anonymous && name == "":
			names = append(names, secretMembers(field.Type)...)
		case field.Tag.Get("secret") == "true":
			if name == "" {
				name = field.Name
			}
			names = append(names, name)
		}
	}
	return names
}

// Job record members a resume hook reads.
const (
	recordRequestMember    = "request"
	recordCheckpointMember = "checkpoint"
)

// ResumeHooks lets a privileged Mutation survive a shutdown: Suspend stops the
// job and returns a checkpoint, Resume continues the job from it.
type ResumeHooks[Req, Res any] struct {
	Suspend func(ctx context.Context, req Req, checkpoint jsontext.Value) (jsontext.Value, error)
	Resume  func(ctx context.Context, req Req, checkpoint jsontext.Value) (Res, error)
}

// MutationBinding carries a Mutation route and its handler.
type MutationBinding[Req, Res any] struct {
	Route RouteSpec
	fn    func(context.Context, Req) (Res, error)
	hooks ResumeHooks[Req, Res]
}

// Mutation declares a route that changes the host. Its request and result
// types come from fn, Req implementing Targeted makes the route targeted, and
// its fields tagged `secret:"true"` stay out of the job record.
func Mutation[Req, Res any](name string, fn func(context.Context, Req) (Res, error), opts ...RouteSpecOption) MutationBinding[Req, Res] {
	spec := newRoute[Req, Res](KindMutation, bridgeipc.ModeMutation, name, opts...).spec
	_, spec.Targeted = any(*new(Req)).(Targeted)
	spec.Secrets = secretMembers(reflect.TypeFor[Req]())
	return MutationBinding[Req, Res]{Route: spec, fn: fn}
}

// MutationTask declares a Mutation whose handler takes the job's task, for
// runners that report through it or name files after its ID.
func MutationTask[Req, Res any](name string, fn TaskRunnerFunc[Req, Res], opts ...RouteSpecOption) MutationBinding[Req, Res] {
	b := Mutation(name, func(ctx context.Context, req Req) (Res, error) {
		task, _ := bridgeipc.TaskFromContext(ctx) // a Mutation always runs as a task
		return fn(ctx, task, req)
	}, opts...)
	b.Route.TaskClient = true
	return b
}

// MutationVoid declares a Mutation whose handler returns no result.
func MutationVoid[Req any](name string, fn func(context.Context, Req) error, opts ...RouteSpecOption) MutationBinding[Req, NoResponse] {
	return Mutation(name, func(ctx context.Context, req Req) (NoResponse, error) {
		return NoResponse{}, fn(ctx, req)
	}, opts...)
}

// Resumable returns the binding with resume hooks. Only a Privileged route may
// resume, because resumption runs in the root job host after a reboot.
func (b MutationBinding[Req, Res]) Resumable(h ResumeHooks[Req, Res]) MutationBinding[Req, Res] {
	if !b.Route.Privileged {
		panic(fmt.Sprintf("apischema: resumable route %s must be privileged", b.Route.Route))
	}
	if h.Suspend == nil || h.Resume == nil {
		panic(fmt.Sprintf("apischema: resumable route %s needs both hooks", b.Route.Route))
	}
	if len(b.Route.Secrets) > 0 {
		panic(fmt.Sprintf("apischema: resumable route %s cannot resume without its secret fields", b.Route.Route))
	}
	b.Route.Resumable = true
	b.hooks = h
	return b
}

func newRoute[Request, Result any](kind Kind, mode bridgeipc.Mode, name string, opts ...RouteSpecOption) Route[Request, Result] {
	spec := routeSpec(kind, mode, name, TypeOf[Request](), TypeOf[Result](), bridgeipc.JSONRequestDecoder[Request](), opts...)
	if spec.Metadata != nil && spec.Mode != bridgeipc.ModeTask && spec.Mode != bridgeipc.ModeMutation {
		panic(fmt.Sprintf("apischema: route %s metadata is allowed only on task and mutation routes", spec.Route))
	}
	if spec.Exclusive && spec.Mode != bridgeipc.ModeMutation {
		panic(fmt.Sprintf("apischema: route %s exclusivity is allowed only on mutation routes", spec.Route))
	}
	if spec.Identity != nil && (spec.Kind != KindTaskRunner || spec.Mode != bridgeipc.ModeTask) {
		panic(fmt.Sprintf("apischema: route %s identity is allowed only on task runners", spec.Route))
	}
	if spec.Progress.GoType != nil && spec.Mode != bridgeipc.ModeTask && spec.Mode != bridgeipc.ModeMutation {
		panic(fmt.Sprintf("apischema: route %s progress is allowed only on task and mutation routes", spec.Route))
	}
	if spec.BrowserBound && spec.Mode != bridgeipc.ModeMutation {
		panic(fmt.Sprintf("apischema: route %s browser binding is allowed only on mutation routes", spec.Route))
	}
	if spec.RetrySafe && spec.Mode != bridgeipc.ModeCall {
		panic(fmt.Sprintf("apischema: route %s retry safety is allowed only on call routes", spec.Route))
	}
	if spec.Mode == bridgeipc.ModeTask && spec.TaskLifetime == "" {
		panic(fmt.Sprintf("apischema: task route %s must declare a lifetime", spec.Route))
	}
	if spec.Identity != nil && spec.TaskLifetime != bridgeipc.TaskLifetimeDurable {
		panic(fmt.Sprintf("apischema: route %s stable identity requires durable lifetime", spec.Route))
	}
	if spec.Mode != bridgeipc.ModeTask && spec.TaskLifetime != "" {
		panic(fmt.Sprintf("apischema: route %s task lifetime is allowed only on task routes", spec.Route))
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
	KindMutation   Kind = "mutation"
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

// Handle binds the ordinary request-in/result-out handler for call routes.
func (r Route[Request, Result]) Handle(handle TypedHandlerFunc[Request, Result], options ...bridgeipc.RouteOption) HandlerBinding {
	if r.spec.Mode != bridgeipc.ModeCall {
		panic(fmt.Sprintf("apischema: route %s is not a call route", r.spec.Route))
	}
	binding := HandlerBinding{
		Route:   r.spec,
		Decode:  r.spec.Decode,
		Options: options,
		Call:    wrapTypedCall(r.spec, handle),
	}
	return binding
}

// HandleVoid binds a handler for a call route declared with a NoResponse result.
// It panics at binding time if Result is anything else, so the declaration and
// the handler cannot drift apart.
func (r Route[Request, Result]) HandleVoid(handle VoidHandlerFunc[Request], options ...bridgeipc.RouteOption) HandlerBinding {
	if !r.spec.Result.Void() {
		panic(fmt.Sprintf("apischema: route %s returns %s, so it cannot use HandleVoid", r.spec.Route, r.spec.Result.GoType))
	}
	if r.spec.Mode != bridgeipc.ModeCall {
		panic(fmt.Sprintf("apischema: route %s is not a call route", r.spec.Route))
	}
	binding := HandlerBinding{
		Route:   r.spec,
		Decode:  r.spec.Decode,
		Options: options,
		Call:    wrapVoidCall(r.spec.Route, handle),
	}
	return binding
}

func (r Route[Request, Result]) Run(runner TaskRunnerFunc[Request, Result], policy bridgeipc.TaskPolicy, options ...bridgeipc.RouteOption) TaskBinding {
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

// HandlerBinding carries a direct call handler.
type HandlerBinding struct {
	Route   RouteSpec
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
	handlers  []HandlerBinding
	tasks     []TaskBinding
	duplexes  []DuplexBinding
	mutations []func(*bridgeipc.Router)
	routes    []RouteSpec
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
	for _, attach := range s.mutations {
		attach(router)
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

func (b MutationBinding[Req, Res]) addTo(set *BindingSet) {
	set.mutations = append(set.mutations, func(router *bridgeipc.Router) { AttachMutation(router, b) })
	set.routes = append(set.routes, requireRouteSpec(b.Route))
}

func AttachHandler(router *bridgeipc.Router, binding HandlerBinding) {
	spec := requireRouteSpec(binding.Route)
	if spec.Kind != KindHandler {
		panic(fmt.Sprintf("apischema: route %s is %s, not handler", spec.Route, spec.Kind))
	}
	opts := routeOptions(spec, binding.Options)
	opts = append(opts, bridgeipc.WithRequestDecoder(requireDecoder(spec, binding.Decode)))
	if spec.Mode != bridgeipc.ModeCall {
		panic(fmt.Sprintf("apischema: route %s is %s, not call", spec.Route, spec.Mode))
	}
	router.Call(spec.Route, binding.Call, opts...)
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

// AttachMutation registers a Mutation binding. Its resume hooks, if any, read
// the request and checkpoint members of the job record.
func AttachMutation[Req, Res any](router *bridgeipc.Router, binding MutationBinding[Req, Res]) {
	spec := requireRouteSpec(binding.Route)
	if spec.Kind != KindMutation {
		panic(fmt.Sprintf("apischema: route %s is %s, not mutation", spec.Route, spec.Kind))
	}
	opts := routeOptions(spec, nil)
	opts = append(opts, bridgeipc.WithRequestDecoder(requireDecoder(spec, spec.Decode)))
	if spec.Resumable {
		opts = append(opts, bridgeipc.WithResume(resumeHooks(spec, binding.hooks)))
	}
	router.Mutation(spec.Route, wrapMutation(spec, binding.fn), opts...)
}

func requireRouteSpec(spec RouteSpec) RouteSpec {
	if spec.Route == "" {
		panic("apischema: route spec cannot be empty")
	}
	return spec
}

func wrapTypedCall[Request, Result any](spec RouteSpec, handle TypedHandlerFunc[Request, Result]) bridgeipc.CallFunc {
	return func(ctx context.Context, request bridgeipc.Request) (any, error) {
		req, err := typedRequest[Request](spec.Route, request.DecodedValue)
		if err != nil {
			return nil, err
		}
		result, err := handle(ctx, req)
		return erasedResult(spec, result, err)
	}
}

func wrapVoidCall[Request any](route string, handle VoidHandlerFunc[Request]) bridgeipc.CallFunc {
	return func(ctx context.Context, request bridgeipc.Request) (any, error) {
		req, err := typedRequest[Request](route, request.DecodedValue)
		if err != nil {
			return nil, err
		}
		return nil, handle(ctx, req)
	}
}

func wrapTaskRunner[Request, Result any](route string, runner TaskRunnerFunc[Request, Result]) bridgeipc.TaskRunner {
	return func(ctx context.Context, task *bridgeipc.Task, request any) (any, error) {
		req, err := typedRequest[Request](route, request)
		if err != nil {
			return nil, err
		}
		result, err := runner(ctx, task, req)
		return any(result), err
	}
}

func wrapDuplex[Request any](route string, handle DuplexFunc[Request]) bridgeipc.DuplexFunc {
	return func(ctx context.Context, stream net.Conn, request bridgeipc.Request) error {
		req, err := typedRequest[Request](route, request.DecodedValue)
		if err != nil {
			return err
		}
		return handle(ctx, stream, req)
	}
}

func wrapMutation[Req, Res any](spec RouteSpec, fn func(context.Context, Req) (Res, error)) bridgeipc.TaskRunner {
	return func(ctx context.Context, _ *bridgeipc.Task, request any) (any, error) {
		req, err := typedRequest[Req](spec.Route, request)
		if err != nil {
			return nil, err
		}
		result, err := fn(ctx, req)
		return erasedResult(spec, result, err)
	}
}

func resumeHooks[Req, Res any](spec RouteSpec, hooks ResumeHooks[Req, Res]) (bridgeipc.SuspendFunc, bridgeipc.ResumeFunc) {
	suspend := func(ctx context.Context, record jsontext.Value) (jsontext.Value, error) {
		req, checkpoint, err := decodeRecord[Req](spec, record)
		if err != nil {
			return nil, err
		}
		return hooks.Suspend(ctx, req, checkpoint)
	}
	resume := func(ctx context.Context, record jsontext.Value) (any, error) {
		req, checkpoint, err := decodeRecord[Req](spec, record)
		if err != nil {
			return nil, err
		}
		result, err := hooks.Resume(ctx, req, checkpoint)
		return erasedResult(spec, result, err)
	}
	return suspend, resume
}

// decodeRecord reads a job record's request through the route decoder and
// returns it with the record's checkpoint.
func decodeRecord[Req any](spec RouteSpec, record jsontext.Value) (Req, jsontext.Value, error) {
	var zero Req
	var members map[string]jsontext.Value
	if err := json.Unmarshal(record, &members); err != nil {
		return zero, nil, fmt.Errorf("decode %s job record: %w", spec.Route, err)
	}
	decoded, err := spec.Decode(members[recordRequestMember])
	if err != nil {
		return zero, nil, fmt.Errorf("decode %s job record request: %w", spec.Route, err)
	}
	req, err := typedRequest[Req](spec.Route, decoded)
	return req, members[recordCheckpointMember], err
}

// erasedResult erases a typed Call or Mutation result at the bridge boundary.
// A NoResponse result is sent as no result.
func erasedResult[Res any](spec RouteSpec, result Res, err error) (any, error) {
	if err != nil || spec.Result.Void() {
		return nil, err
	}
	return result, nil
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
	if spec.Identity != nil {
		opts = append(opts, bridgeipc.WithTaskIdentity(spec.Identity))
	}
	if spec.TaskLifetime != "" {
		opts = append(opts, bridgeipc.WithTaskLifetime(spec.TaskLifetime))
	}
	if spec.Targeted {
		opts = append(opts, bridgeipc.WithTarget(func(request any) []string {
			// A request of another type has no target; the runner rejects it.
			if targeted, ok := request.(Targeted); ok {
				return targeted.Target()
			}
			return nil
		}))
	}
	if len(spec.Secrets) > 0 {
		opts = append(opts, bridgeipc.WithSecrets(spec.Secrets...))
	}
	if spec.BrowserBound {
		opts = append(opts, bridgeipc.WithBrowserBound())
	}
	if spec.Exclusive {
		opts = append(opts, bridgeipc.Exclusive)
	}
	return opts
}

func taskPolicy(explicit bridgeipc.TaskPolicy) bridgeipc.TaskPolicy {
	if explicit.Name != "" {
		return explicit
	}
	return bridgeipc.TaskDefault
}
