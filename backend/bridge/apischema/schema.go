package apischema

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"reflect"
	"strings"

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RouteSpec is the Go-side contract for one LinuxIO API route.
type RouteSpec struct {
	Route      string
	Mode       bridgeipc.Mode
	Kind       Kind
	Policy     bridgeipc.JobPolicy
	Privileged bool
	NoEndpoint bool

	Request TypeSpec
	Result  TypeSpec
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

func WithPolicy(policy bridgeipc.JobPolicy) RouteSpecOption {
	return func(spec *RouteSpec) {
		spec.Policy = policy
	}
}

func Query[Request, Result any](route string, opts ...RouteSpecOption) RouteSpec {
	return routeSpec(KindHandler, bridgeipc.ModeQuery, route, TypeOf[Request](), TypeOf[Result](), opts...)
}

func Job[Request, Result any](route string, opts ...RouteSpecOption) RouteSpec {
	return routeSpec(KindHandler, bridgeipc.ModeJob, route, TypeOf[Request](), TypeOf[Result](), opts...)
}

func Runner[Request, Result any](route string, opts ...RouteSpecOption) RouteSpec {
	return routeSpec(KindRunner, bridgeipc.ModeJob, route, TypeOf[Request](), TypeOf[Result](), opts...)
}

func DuplexRoute[Request, Result any](route string, opts ...RouteSpecOption) RouteSpec {
	return routeSpec(KindDuplex, bridgeipc.ModeDuplex, route, TypeOf[Request](), TypeOf[Result](), opts...)
}

func routeSpec(kind Kind, mode bridgeipc.Mode, route string, request TypeSpec, result TypeSpec, opts ...RouteSpecOption) RouteSpec {
	spec := RouteSpec{
		Kind:    kind,
		Route:   route,
		Mode:    mode,
		Request: request,
		Result:  result,
	}
	for _, opt := range opts {
		opt(&spec)
	}
	return spec
}

type Kind string

const (
	KindHandler Kind = "handler"
	KindRunner  Kind = "runner"
	KindDuplex  Kind = "duplex"
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

func (r RouteSpec) Handle(handle any, options ...bridgeipc.RouteOption) HandlerBinding {
	return HandlerBinding{Route: r, Handle: handle, Options: options}
}

func (r RouteSpec) HandleWithPolicy(handle any, policy bridgeipc.JobPolicy, options ...bridgeipc.RouteOption) HandlerBinding {
	return HandlerBinding{Route: r, Handle: handle, Policy: policy, Options: options}
}

func (r RouteSpec) Run(runner any, policy bridgeipc.JobPolicy, options ...bridgeipc.RouteOption) RunnerBinding {
	return RunnerBinding{Route: r, Runner: runner, Policy: policy, Options: options}
}

func (r RouteSpec) Duplex(handle any, options ...bridgeipc.RouteOption) DuplexBinding {
	return DuplexBinding{Route: r, Handle: handle, Options: options}
}

type HandlerBinding struct {
	Route   RouteSpec
	Handle  any
	Policy  bridgeipc.JobPolicy
	Options []bridgeipc.RouteOption
}

type RunnerBinding struct {
	Route   RouteSpec
	Runner  any
	Policy  bridgeipc.JobPolicy
	Options []bridgeipc.RouteOption
}

type DuplexBinding struct {
	Route   RouteSpec
	Handle  any
	Options []bridgeipc.RouteOption
}

type Binding interface {
	addTo(*BindingSet)
}

type BindingSet struct {
	handlers []HandlerBinding
	runners  []RunnerBinding
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
	for _, binding := range s.runners {
		AttachRunner(router, binding)
	}
	for _, binding := range s.duplexes {
		AttachDuplex(router, binding)
	}
}

func (r RouteSpec) addTo(set *BindingSet) {
	set.routes = append(set.routes, requireRouteSpec(r))
}

func (b HandlerBinding) addTo(set *BindingSet) {
	set.handlers = append(set.handlers, b)
	set.routes = append(set.routes, requireRouteSpec(b.Route))
}

func (b RunnerBinding) addTo(set *BindingSet) {
	set.runners = append(set.runners, b)
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
	opts = append(opts, bridgeipc.WithRequestDecoder(requestDecoder(spec.Request)))
	handle := adaptHandler(spec, binding.Handle)
	switch spec.Mode {
	case bridgeipc.ModeQuery:
		router.Query(spec.Route, handle, opts...)
	case bridgeipc.ModeJob:
		router.Job(spec.Route, handle, jobPolicy(spec, binding.Policy), opts...)
	default:
		panic(fmt.Sprintf("apischema: route %s is %s, not query/job", spec.Route, spec.Mode))
	}
}

func AttachHandlers(router *bridgeipc.Router, bindings ...HandlerBinding) {
	for _, binding := range bindings {
		AttachHandler(router, binding)
	}
}

func RegisterRoutes(router *bridgeipc.Router, bindings ...HandlerBinding) {
	AttachHandlers(router, bindings...)
}

func AttachRunner(router *bridgeipc.Router, binding RunnerBinding) {
	spec := requireRouteSpec(binding.Route)
	if spec.Kind != KindRunner {
		panic(fmt.Sprintf("apischema: route %s is %s, not runner", spec.Route, spec.Kind))
	}
	if spec.Mode != bridgeipc.ModeJob {
		panic(fmt.Sprintf("apischema: route %s is %s, not job", spec.Route, spec.Mode))
	}
	opts := routeOptions(spec, binding.Options)
	opts = append(opts, bridgeipc.WithRequestDecoder(requestDecoder(spec.Request)))
	router.JobRunner(spec.Route, adaptRunner(spec, binding.Runner), jobPolicy(spec, binding.Policy), opts...)
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
	opts = append(opts, bridgeipc.WithRequestDecoder(requestDecoder(spec.Request)))
	router.Duplex(spec.Route, adaptDuplex(spec, binding.Handle), opts...)
}

func RequestDecoder(spec TypeSpec) bridgeipc.RequestDecoder {
	return requestDecoder(spec)
}

func requireRouteSpec(spec RouteSpec) RouteSpec {
	if spec.Route == "" {
		panic("apischema: route spec cannot be empty")
	}
	return spec
}

func requestDecoder(spec TypeSpec) bridgeipc.RequestDecoder {
	return func(raw json.RawMessage) (any, error) {
		if len(raw) == 0 || string(raw) == "null" {
			raw = json.RawMessage("{}")
		}
		return decodeRequestValue(spec, raw)
	}
}

func decodeRequestValue(spec TypeSpec, raw json.RawMessage) (any, error) {
	t := spec.GoType
	if t == nil {
		return nil, nil
	}
	target := reflect.New(deref(t))
	if err := json.Unmarshal(raw, target.Interface()); err != nil {
		return nil, err
	}
	return target.Elem().Interface(), nil
}

var (
	contextType = reflect.TypeFor[context.Context]()
	errorType   = reflect.TypeFor[error]()
	eventsType  = reflect.TypeFor[bridgeipc.Events]()
	jobType     = reflect.TypeFor[*bridgeipc.Job]()
	connType    = reflect.TypeFor[net.Conn]()
)

func adaptHandler(spec RouteSpec, handle any) bridgeipc.HandlerFunc {
	fn := reflect.ValueOf(handle)
	reqType := requestType(spec)
	validateFunc(spec.Route, "handler", fn, []reflect.Type{contextType, reqType, eventsType}, []reflect.Type{errorType})
	return func(ctx context.Context, request any, emit bridgeipc.Events) error {
		out := fn.Call([]reflect.Value{
			reflect.ValueOf(ctx),
			requestValue(request, reqType),
			reflect.ValueOf(emit),
		})
		return callError(out[0])
	}
}

func adaptRunner(spec RouteSpec, runner any) bridgeipc.Runner {
	fn := reflect.ValueOf(runner)
	reqType := requestType(spec)
	validateFunc(spec.Route, "runner", fn, []reflect.Type{contextType, jobType, reqType}, []reflect.Type{reflect.TypeFor[any](), errorType})
	return func(ctx context.Context, job *bridgeipc.Job, request any) (any, error) {
		out := fn.Call([]reflect.Value{
			reflect.ValueOf(ctx),
			reflect.ValueOf(job),
			requestValue(request, reqType),
		})
		return out[0].Interface(), callError(out[1])
	}
}

func adaptDuplex(spec RouteSpec, handle any) bridgeipc.DuplexFunc {
	fn := reflect.ValueOf(handle)
	reqType := requestType(spec)
	validateFunc(spec.Route, "duplex", fn, []reflect.Type{contextType, connType, reqType}, []reflect.Type{errorType})
	return func(ctx context.Context, stream net.Conn, request any) error {
		out := fn.Call([]reflect.Value{
			reflect.ValueOf(ctx),
			reflect.ValueOf(stream),
			requestValue(request, reqType),
		})
		return callError(out[0])
	}
}

func validateFunc(route, kind string, fn reflect.Value, in []reflect.Type, out []reflect.Type) {
	if !fn.IsValid() || fn.Kind() != reflect.Func {
		panic(fmt.Sprintf("apischema: %s %s is not a function", route, kind))
	}
	t := fn.Type()
	if t.NumIn() != len(in) || t.NumOut() != len(out) {
		panic(fmt.Sprintf("apischema: %s %s has signature %s", route, kind, t))
	}
	for i, want := range in {
		got := t.In(i)
		if !want.AssignableTo(got) {
			panic(fmt.Sprintf("apischema: %s %s arg %d is %s, want %s", route, kind, i, got, want))
		}
	}
	for i, want := range out {
		got := t.Out(i)
		if !got.AssignableTo(want) {
			panic(fmt.Sprintf("apischema: %s %s return %d is %s, want %s", route, kind, i, got, want))
		}
	}
}

func requestType(spec RouteSpec) reflect.Type {
	t := deref(spec.Request.GoType)
	if t == nil {
		return reflect.TypeFor[NoRequest]()
	}
	return t
}

func requestValue(request any, target reflect.Type) reflect.Value {
	if request == nil {
		return reflect.Zero(target)
	}
	value := reflect.ValueOf(request)
	if value.Type().AssignableTo(target) {
		return value
	}
	if value.Type().ConvertibleTo(target) {
		return value.Convert(target)
	}
	return reflect.Zero(target)
}

func callError(value reflect.Value) error {
	if value.IsNil() {
		return nil
	}
	err, ok := value.Interface().(error)
	if !ok {
		return nil
	}
	return err
}

func deref(t reflect.Type) reflect.Type {
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func routeOptions(spec RouteSpec, explicit []bridgeipc.RouteOption) []bridgeipc.RouteOption {
	opts := append([]bridgeipc.RouteOption(nil), explicit...)
	if spec.Privileged {
		opts = append(opts, bridgeipc.Privileged)
	}
	return opts
}

func jobPolicy(spec RouteSpec, explicit bridgeipc.JobPolicy) bridgeipc.JobPolicy {
	if explicit.Name != "" {
		return explicit
	}
	if spec.Policy.Name != "" {
		return spec.Policy
	}
	return bridgeipc.ActionDefault
}
