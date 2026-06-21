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

	Request TypeSpec
	Result  TypeSpec

	Decode bridgeipc.RequestDecoder
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

type Route[Request, Result any] struct {
	spec RouteSpec
}

type HandlerFunc[Request any] func(ctx context.Context, req Request, emit bridgeipc.Events) error
type RunnerFunc[Request any] func(ctx context.Context, job *bridgeipc.Job, req Request) (any, error)
type DuplexFunc[Request any] func(ctx context.Context, stream net.Conn, req Request) error

func Query[Request, Result any](name string, opts ...RouteSpecOption) Route[Request, Result] {
	return newRoute[Request, Result](KindHandler, bridgeipc.ModeQuery, name, opts...)
}

func Job[Request, Result any](name string, opts ...RouteSpecOption) Route[Request, Result] {
	return newRoute[Request, Result](KindHandler, bridgeipc.ModeJob, name, opts...)
}

func Runner[Request, Result any](name string, opts ...RouteSpecOption) Route[Request, Result] {
	return newRoute[Request, Result](KindRunner, bridgeipc.ModeJob, name, opts...)
}

func DuplexRoute[Request, Result any](name string, opts ...RouteSpecOption) Route[Request, Result] {
	return newRoute[Request, Result](KindDuplex, bridgeipc.ModeDuplex, name, opts...)
}

func newRoute[Request, Result any](kind Kind, mode bridgeipc.Mode, name string, opts ...RouteSpecOption) Route[Request, Result] {
	return Route[Request, Result]{
		spec: routeSpec(kind, mode, name, TypeOf[Request](), TypeOf[Result](), requestDecoder[Request](), opts...),
	}
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

func (r Route[Request, Result]) Handle(handle HandlerFunc[Request], options ...bridgeipc.RouteOption) HandlerBinding {
	return HandlerBinding{
		Route:   r.spec,
		Decode:  r.spec.Decode,
		Handle:  wrapHandler(r.spec.Route, handle),
		Options: options,
	}
}

func (r Route[Request, Result]) HandleWithPolicy(handle HandlerFunc[Request], policy bridgeipc.JobPolicy, options ...bridgeipc.RouteOption) HandlerBinding {
	return HandlerBinding{
		Route:   r.spec,
		Decode:  r.spec.Decode,
		Handle:  wrapHandler(r.spec.Route, handle),
		Policy:  policy,
		Options: options,
	}
}

func (r Route[Request, Result]) Run(runner RunnerFunc[Request], policy bridgeipc.JobPolicy, options ...bridgeipc.RouteOption) RunnerBinding {
	return RunnerBinding{
		Route:   r.spec,
		Decode:  r.spec.Decode,
		Runner:  wrapRunner(r.spec.Route, runner),
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

type HandlerBinding struct {
	Route   RouteSpec
	Handle  bridgeipc.HandlerFunc
	Decode  bridgeipc.RequestDecoder
	Policy  bridgeipc.JobPolicy
	Options []bridgeipc.RouteOption
}

type RunnerBinding struct {
	Route   RouteSpec
	Runner  bridgeipc.Runner
	Decode  bridgeipc.RequestDecoder
	Policy  bridgeipc.JobPolicy
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

func (r Route[Request, Result]) addTo(set *BindingSet) {
	set.routes = append(set.routes, requireRouteSpec(r.spec))
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
	opts = append(opts, bridgeipc.WithRequestDecoder(requireDecoder(spec, binding.Decode)))
	switch spec.Mode {
	case bridgeipc.ModeQuery:
		router.Query(spec.Route, binding.Handle, opts...)
	case bridgeipc.ModeJob:
		router.Job(spec.Route, binding.Handle, jobPolicy(binding.Policy), opts...)
	default:
		panic(fmt.Sprintf("apischema: route %s is %s, not query/job", spec.Route, spec.Mode))
	}
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
	opts = append(opts, bridgeipc.WithRequestDecoder(requireDecoder(spec, binding.Decode)))
	router.JobRunner(spec.Route, binding.Runner, jobPolicy(binding.Policy), opts...)
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

func RequestDecoder(spec RouteSpec) bridgeipc.RequestDecoder {
	return requireDecoder(spec, spec.Decode)
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

func wrapHandler[Request any](route string, handle HandlerFunc[Request]) bridgeipc.HandlerFunc {
	return func(ctx context.Context, request any, emit bridgeipc.Events) error {
		req, err := typedRequest[Request](route, request)
		if err != nil {
			return err
		}
		return handle(ctx, req, emit)
	}
}

func wrapRunner[Request any](route string, runner RunnerFunc[Request]) bridgeipc.Runner {
	return func(ctx context.Context, job *bridgeipc.Job, request any) (any, error) {
		req, err := typedRequest[Request](route, request)
		if err != nil {
			return nil, err
		}
		return runner(ctx, job, req)
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
	return opts
}

func jobPolicy(explicit bridgeipc.JobPolicy) bridgeipc.JobPolicy {
	if explicit.Name != "" {
		return explicit
	}
	return bridgeipc.ActionDefault
}
