package apischema

import (
	"fmt"
	"sort"
	"strings"

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RouteSpec is the Go-side contract for one LinuxIO API route.
//
// ArgsTS and ResultTS intentionally describe the current frontend contract while
// the runtime still uses the legacy []string bridge payload. Later transport
// work can replace those fields with generated Go model traversal without
// changing which package owns the route manifest.
type RouteSpec struct {
	Route      string
	Mode       bridgeipc.Mode
	Kind       Kind
	Policy     bridgeipc.JobPolicy
	Privileged bool
	NoEndpoint bool
	ArgsTS     string
	ResultTS   string
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

func Route(route string) (RouteSpec, bool) {
	for _, spec := range Routes {
		if spec.Route == route {
			return spec, true
		}
	}
	return RouteSpec{}, false
}

func MustRoute(route string) RouteSpec {
	spec, ok := Route(route)
	if !ok {
		panic("apischema: unknown route " + route)
	}
	return spec
}

func RoutesFor(handler string) []RouteSpec {
	var out []RouteSpec
	prefix := handler + "."
	for _, spec := range Routes {
		if strings.HasPrefix(spec.Route, prefix) {
			out = append(out, spec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Route < out[j].Route })
	return out
}

type HandlerBinding struct {
	Route   string
	Handle  bridgeipc.HandlerFunc
	Policy  bridgeipc.JobPolicy
	Options []bridgeipc.RouteOption
}

type RunnerBinding struct {
	Route   string
	Runner  bridgeipc.Runner
	Policy  bridgeipc.JobPolicy
	Options []bridgeipc.RouteOption
}

type DuplexBinding struct {
	Route   string
	Handle  bridgeipc.DuplexFunc
	Options []bridgeipc.RouteOption
}

func AttachHandler(router *bridgeipc.Router, binding HandlerBinding) {
	spec := MustRoute(binding.Route)
	if spec.Kind != KindHandler {
		panic(fmt.Sprintf("apischema: route %s is %s, not handler", spec.Route, spec.Kind))
	}
	opts := routeOptions(spec, binding.Options)
	switch spec.Mode {
	case bridgeipc.ModeQuery:
		router.Query(spec.Route, binding.Handle, opts...)
	case bridgeipc.ModeJob:
		router.Job(spec.Route, binding.Handle, jobPolicy(spec, binding.Policy), opts...)
	default:
		panic(fmt.Sprintf("apischema: route %s is %s, not query/job", spec.Route, spec.Mode))
	}
}

func AttachHandlers(router *bridgeipc.Router, bindings []HandlerBinding) {
	for _, binding := range bindings {
		AttachHandler(router, binding)
	}
}

func RegisterRoutes(router *bridgeipc.Router, component string, commands []bridgeipc.Command) {
	for _, cmd := range commands {
		route := component + "." + cmd.Name
		spec := MustRoute(route)
		if cmd.Mode != "" && cmd.Mode != spec.Mode {
			panic(fmt.Sprintf("apischema: %s declared as %s but schema says %s", route, cmd.Mode, spec.Mode))
		}
		binding := HandlerBinding{
			Route:  route,
			Handle: cmd.Handler,
			Policy: cmd.Policy,
		}
		if cmd.Privileged {
			binding.Options = append(binding.Options, bridgeipc.Privileged)
		}
		AttachHandler(router, binding)
	}
}

func AttachRunner(router *bridgeipc.Router, binding RunnerBinding) {
	spec := MustRoute(binding.Route)
	if spec.Kind != KindRunner {
		panic(fmt.Sprintf("apischema: route %s is %s, not runner", spec.Route, spec.Kind))
	}
	if spec.Mode != bridgeipc.ModeJob {
		panic(fmt.Sprintf("apischema: route %s is %s, not job", spec.Route, spec.Mode))
	}
	router.JobRunner(spec.Route, binding.Runner, jobPolicy(spec, binding.Policy), routeOptions(spec, binding.Options)...)
}

func AttachDuplex(router *bridgeipc.Router, binding DuplexBinding) {
	spec := MustRoute(binding.Route)
	if spec.Kind != KindDuplex {
		panic(fmt.Sprintf("apischema: route %s is %s, not duplex", spec.Route, spec.Kind))
	}
	if spec.Mode != bridgeipc.ModeDuplex {
		panic(fmt.Sprintf("apischema: route %s is %s, not duplex", spec.Route, spec.Mode))
	}
	router.Duplex(spec.Route, binding.Handle, routeOptions(spec, binding.Options)...)
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
