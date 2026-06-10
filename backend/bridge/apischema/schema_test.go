package apischema_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func TestRoutesAreUniqueAndComplete(t *testing.T) {
	seen := make(map[string]apischema.RouteSpec, len(handlers.Routes))
	for _, route := range handlers.Routes {
		if route.Route == "" {
			t.Fatal("empty route")
		}
		if route.Mode != bridgeipc.ModeQuery &&
			route.Mode != bridgeipc.ModeJob &&
			route.Mode != bridgeipc.ModeDuplex {
			t.Fatalf("%s has invalid mode %q", route.Route, route.Mode)
		}
		if route.Kind != apischema.KindHandler &&
			route.Kind != apischema.KindRunner &&
			route.Kind != apischema.KindDuplex {
			t.Fatalf("%s has invalid kind %q", route.Route, route.Kind)
		}
		if route.Kind == apischema.KindDuplex && route.Mode != bridgeipc.ModeDuplex {
			t.Fatalf("%s is duplex kind but has mode %q", route.Route, route.Mode)
		}
		if route.Kind == apischema.KindRunner && route.Mode != bridgeipc.ModeJob {
			t.Fatalf("%s is runner kind but has mode %q", route.Route, route.Mode)
		}
		if _, exists := seen[route.Route]; exists {
			t.Fatalf("duplicate route %s", route.Route)
		}
		seen[route.Route] = route
	}

	for _, route := range []string{
		"system.get_cpu_info",
		"docker.update_container",
		"jobs.attach",
		"terminal.open",
		"logs.general.follow",
	} {
		if _, ok := seen[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}
}

func TestRequestDecoderDecodesRouteContracts(t *testing.T) {
	tests := []struct {
		name  string
		route string
		raw   string
	}{
		{
			name:  "no request",
			route: "system.get_cpu_info",
			raw:   `{}`,
		},
		{
			name:  "object request",
			route: "docker.update_container",
			raw:   `{"containerId":"web"}`,
		},
		{
			name:  "optional request",
			route: "docker.compose",
			raw:   `{"action":"up","projectName":"stack"}`,
		},
		{
			name:  "slice request",
			route: "datetime.set_ntp_servers",
			raw:   `{"servers":["0.pool.ntp.org","1.pool.ntp.org"]}`,
		},
		{
			name:  "runner request",
			route: "storage.run_smart_test",
			raw:   `{"device":"sda","testType":"short"}`,
		},
		{
			name:  "duplex request",
			route: "terminal.open",
			raw:   `{"cols":120,"rows":40}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := handlers.MustRoute(tc.route)
			decoded, err := apischema.RequestDecoder(spec)(json.RawMessage(tc.raw))
			if err != nil {
				t.Fatalf("requestDecoder() error = %v", err)
			}
			if !jsonEquivalent(t, decoded, tc.raw) {
				t.Fatalf("decoded request %#v does not match %s", decoded, tc.raw)
			}
		})
	}
}

func TestEndpointExcludesDuplexAndStreamOnlyJobs(t *testing.T) {
	for _, route := range []string{"jobs.attach", "jobs.data", "terminal.open", "container.open"} {
		spec := handlers.MustRoute(route)
		if spec.Endpoint() {
			t.Fatalf("%s should not generate a React Query endpoint", route)
		}
	}

	for _, route := range []string{"docker.logs.follow", "logs.general.follow", "logs.service.follow"} {
		spec := handlers.MustRoute(route)
		if spec.Endpoint() {
			t.Fatalf("%s should remain stream-opener only in this phase", route)
		}
	}

	if !handlers.MustRoute("system.get_cpu_info").Endpoint() {
		t.Fatal("query route should generate an endpoint")
	}
}

func TestRoutesDeclareContractFields(t *testing.T) {
	for _, route := range handlers.Routes {
		if route.Request.GoType == nil {
			t.Fatalf("%s should declare a request contract", route.Route)
		}
		if route.Result.GoType == nil {
			t.Fatalf("%s should declare a result contract", route.Route)
		}
	}
}

func jsonEquivalent(t *testing.T, got any, want string) bool {
	t.Helper()
	gotBytes, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal decoded request: %v", err)
	}
	var gotValue any
	if err := json.Unmarshal(gotBytes, &gotValue); err != nil {
		t.Fatalf("unmarshal decoded request: %v", err)
	}
	var wantValue any
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("unmarshal expected request: %v", err)
	}
	return reflect.DeepEqual(gotValue, wantValue)
}
