package apischema

import (
	"testing"

	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func TestRoutesAreUniqueAndComplete(t *testing.T) {
	seen := make(map[string]RouteSpec, len(Routes))
	for _, route := range Routes {
		if route.Route == "" {
			t.Fatal("empty route")
		}
		if route.Mode != bridgeipc.ModeQuery && route.Mode != bridgeipc.ModeJob && route.Mode != bridgeipc.ModeDuplex {
			t.Fatalf("%s has invalid mode %q", route.Route, route.Mode)
		}
		if route.Kind != KindHandler && route.Kind != KindRunner && route.Kind != KindDuplex {
			t.Fatalf("%s has invalid kind %q", route.Route, route.Kind)
		}
		if route.Kind == KindDuplex && route.Mode != bridgeipc.ModeDuplex {
			t.Fatalf("%s is duplex kind but has mode %q", route.Route, route.Mode)
		}
		if route.Kind == KindRunner && route.Mode != bridgeipc.ModeJob {
			t.Fatalf("%s is runner kind but has mode %q", route.Route, route.Mode)
		}
		if _, exists := seen[route.Route]; exists {
			t.Fatalf("duplicate route %s", route.Route)
		}
		seen[route.Route] = route
	}

	for _, route := range []string{
		"system.get_cpu_info",
		"docker.set_auto_update",
		"jobs.attach",
		"terminal.open",
		"logs.general.follow",
	} {
		if _, ok := seen[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}
}

func TestEndpointExcludesDuplexAndStreamOnlyJobs(t *testing.T) {
	for _, route := range []string{"jobs.attach", "jobs.data", "terminal.open", "container.open"} {
		spec := MustRoute(route)
		if spec.Endpoint() {
			t.Fatalf("%s should not generate a React Query endpoint", route)
		}
	}

	for _, route := range []string{"docker.logs.follow", "logs.general.follow", "logs.service.follow"} {
		spec := MustRoute(route)
		if spec.Endpoint() {
			t.Fatalf("%s should remain stream-opener only in this phase", route)
		}
	}

	if !MustRoute("system.get_cpu_info").Endpoint() {
		t.Fatal("query route should generate an endpoint")
	}
}
