package apischema_test

import (
	"context"
	"encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"errors"
	"reflect"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers"
	dockerhandler "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

func TestRoutesAreUniqueAndComplete(t *testing.T) {
	seen := make(map[string]apischema.RouteSpec, len(handlers.Routes))
	for _, route := range handlers.Routes {
		if route.Route == "" {
			t.Fatal("empty route")
		}
		if route.Mode != bridgeipc.ModeCall &&
			route.Mode != bridgeipc.ModeTask &&
			route.Mode != bridgeipc.ModeDuplex {
			t.Fatalf("%s has invalid mode %q", route.Route, route.Mode)
		}
		if route.Kind != apischema.KindHandler &&
			route.Kind != apischema.KindTaskRunner &&
			route.Kind != apischema.KindDuplex {
			t.Fatalf("%s has invalid kind %q", route.Route, route.Kind)
		}
		if route.Kind == apischema.KindDuplex && route.Mode != bridgeipc.ModeDuplex {
			t.Fatalf("%s is duplex kind but has mode %q", route.Route, route.Mode)
		}
		if route.Kind == apischema.KindTaskRunner && route.Mode != bridgeipc.ModeTask {
			t.Fatalf("%s is task-runner kind but has mode %q", route.Route, route.Mode)
		}
		if _, exists := seen[route.Route]; exists {
			t.Fatalf("duplicate route %s", route.Route)
		}
		seen[route.Route] = route
	}

	for _, route := range []string{
		"system.get_cpu_info",
		"docker.update_container",
		"tasks.watch",
		"terminal.open",
		"logs.general.follow",
	} {
		if _, ok := seen[route]; !ok {
			t.Fatalf("missing route %s", route)
		}
	}
}

func mustRoute(t *testing.T, name string) apischema.RouteSpec {
	t.Helper()
	for _, spec := range handlers.Routes {
		if spec.Route == name {
			return spec
		}
	}
	t.Fatalf("unknown route %q", name)
	return apischema.RouteSpec{}
}

func TestAllTaskRoutesUseTaskRunner(t *testing.T) {
	modes := map[bridgeipc.Mode]int{}

	for _, route := range handlers.Routes {
		modes[route.Mode]++
		if route.Mode == bridgeipc.ModeTask && route.Kind != apischema.KindTaskRunner {
			t.Errorf("%s is task kind %q, want task_runner", route.Route, route.Kind)
		}
	}
	if got, want := modes[bridgeipc.ModeCall], 203; got != want {
		t.Errorf("call route count = %d, want %d", got, want)
	}
	if got, want := modes[bridgeipc.ModeTask], 18; got != want {
		t.Errorf("task route count = %d, want %d", got, want)
	}
	if got, want := modes[bridgeipc.ModeDuplex], 10; got != want {
		t.Errorf("duplex route count = %d, want %d", got, want)
	}
}

func TestTaskRoutesDeclareAuditedLifetime(t *testing.T) {
	sessionCount := 0
	durableCount := 0
	for _, route := range handlers.Routes {
		if route.Mode != bridgeipc.ModeTask {
			continue
		}
		switch route.TaskLifetime {
		case bridgeipc.TaskLifetimeSession:
			sessionCount++
			if route.Identity != nil {
				t.Errorf("session task %s unexpectedly has a stable identity", route.Route)
			}
		case bridgeipc.TaskLifetimeDurable:
			durableCount++
			if route.Route != "control.app_update" && route.Route != "docker.update_container" {
				t.Errorf("unexpected durable task route %s", route.Route)
			}
			if route.Identity == nil {
				t.Errorf("durable task %s has no stable identity", route.Route)
			}
		default:
			t.Errorf("%s task lifetime = %q", route.Route, route.TaskLifetime)
		}
	}
	if sessionCount != 16 || durableCount != 2 {
		t.Fatalf("task lifetime counts = session %d, durable %d; want 16 and 2", sessionCount, durableCount)
	}
}

func TestTaskLifetimeOptionsRejectInvalidRoutes(t *testing.T) {
	tests := map[string]func(){
		"missing task lifetime": func() {
			_ = apischema.TaskRunner[apischema.NoRequest, apischema.SuccessResponse]("test.missing_lifetime")
		},
		"call lifetime": func() {
			_ = apischema.Call[apischema.NoRequest, apischema.SuccessResponse]("test.call_lifetime", apischema.SessionTask())
		},
		"duplex lifetime": func() {
			_ = apischema.DuplexRoute[apischema.NoRequest, apischema.NoResponse]("test.duplex_lifetime", apischema.SessionTask())
		},
		"stable identity on session task": func() {
			_ = apischema.TaskRunner[apischema.NoRequest, apischema.SuccessResponse](
				"test.session_identity",
				apischema.SessionTask(),
				apischema.WithTaskIdentity(func(apischema.NoRequest) (bridgeipc.TaskIdentity, error) {
					return bridgeipc.TaskIdentity{ID: "id", Fingerprint: "fingerprint"}, nil
				}),
			)
		},
	}

	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("invalid task lifetime declaration was accepted")
				}
			}()
			build()
		})
	}
}

func TestTaskLifetimeOptionsDeclareExpectedLifetime(t *testing.T) {
	tests := []struct {
		name string
		want bridgeipc.TaskLifetime
		opt  apischema.RouteSpecOption
	}{
		{name: "session", want: bridgeipc.TaskLifetimeSession, opt: apischema.SessionTask()},
		{name: "durable", want: bridgeipc.TaskLifetimeDurable, opt: apischema.DurableTask()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			binding := apischema.TaskRunner[apischema.NoRequest, apischema.SuccessResponse]("test."+tc.name+"_lifetime", tc.opt).Run(
				func(context.Context, *bridgeipc.Task, apischema.NoRequest) (apischema.SuccessResponse, error) {
					return apischema.SuccessResponse{}, nil
				},
				bridgeipc.TaskDefault,
			)
			if binding.Route.TaskLifetime != tc.want {
				t.Fatalf("task lifetime = %q, want %q", binding.Route.TaskLifetime, tc.want)
			}
		})
	}
}

func TestRetrySafeRoutesAreExplicitCalls(t *testing.T) {
	count := 0
	for _, route := range handlers.Routes {
		if !route.RetrySafe {
			continue
		}
		count++
		if route.Mode != bridgeipc.ModeCall || !route.Endpoint() {
			t.Errorf("%s is retry-safe but is not a public Call", route.Route)
		}
	}
	if count != 87 {
		t.Fatalf("retry-safe Call count = %d, want 87", count)
	}
	for _, route := range []string{"config.get", "system.get_cpu_info", "tasks.get", "virt.preflight"} {
		if !mustRoute(t, route).RetrySafe {
			t.Errorf("%s should be explicitly retry-safe", route)
		}
	}
	for _, route := range []string{
		"docker.check_updates",
		"docker.get_icon",
		"docker.get_icon_uri",
		"docker.start_container",
		"network.get_interface_stats",
		"network.get_network_info",
		"system.get_disk_throughput",
		"system.get_health_summary",
		"system.get_updates_fast",
		"tasks.cancel",
		"terminal.list_shells",
	} {
		if mustRoute(t, route).RetrySafe {
			t.Errorf("%s should default to no retry", route)
		}
	}
}

func TestRetrySafeRejectsNonCallRoutes(t *testing.T) {
	for name, build := range map[string]func(){
		"task": func() {
			_ = apischema.TaskRunner[apischema.NoRequest, apischema.SuccessResponse]("test.retry_task", apischema.RetrySafe())
		},
		"duplex": func() {
			_ = apischema.DuplexRoute[apischema.NoRequest, apischema.NoResponse]("test.retry_duplex", apischema.RetrySafe())
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("non-Call route accepted RetrySafe")
				}
			}()
			build()
		})
	}
}

func TestTaskRoutesDeclareTerminalResultsAndProgress(t *testing.T) {
	taskSnapshotType := reflect.TypeFor[apischema.TaskSnapshot]()
	for _, route := range handlers.Routes {
		if route.Mode != bridgeipc.ModeTask {
			continue
		}
		if route.Result.GoType == taskSnapshotType || route.Result.Void() {
			t.Errorf("%s has placeholder terminal result %v", route.Route, route.Result.GoType)
		}
		if route.Progress.GoType == nil {
			t.Errorf("%s reports progress but has no progress contract", route.Route)
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
			raw:   `{"containerId":"web","runId":"00000000-0000-4000-8000-000000000001"}`,
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
			spec := mustRoute(t, tc.route)
			if spec.Decode == nil {
				t.Fatalf("%s has no request decoder", tc.route)
			}
			decoded, err := spec.Decode(json.RawMessage(tc.raw))
			if err != nil {
				t.Fatalf("requestDecoder() error = %v", err)
			}
			if !jsonEquivalent(t, decoded, tc.raw) {
				t.Fatalf("decoded request %#v does not match %s", decoded, tc.raw)
			}
		})
	}
}

func TestRequestDecoderEnforcesStrictSingleValuePolicy(t *testing.T) {
	spec := mustRoute(t, "docker.start_container")
	tests := []struct {
		name               string
		raw                json.RawMessage
		wantContainerID    string
		wantSemanticError  bool
		wantSyntacticError bool
	}{
		{name: "valid object", raw: json.RawMessage(`{"containerId":"web"}`), wantContainerID: "web"},
		{name: "unknown field", raw: json.RawMessage(`{"containerId":"web","unexpected":true}`), wantSemanticError: true},
		{name: "case-mismatched field", raw: json.RawMessage(`{"ContainerId":"web"}`), wantSemanticError: true},
		{name: "duplicate field", raw: json.RawMessage(`{"containerId":"web","containerId":"db"}`), wantSyntacticError: true},
		{name: "invalid UTF-8", raw: json.RawMessage("{\"containerId\":\"\xff\"}"), wantSyntacticError: true},
		{name: "trailing JSON value", raw: json.RawMessage(`{"containerId":"web"} {}`), wantSyntacticError: true},
		{name: "scalar type mismatch", raw: json.RawMessage(`{"containerId":123}`), wantSemanticError: true},
		{name: "empty input", raw: nil},
		{name: "null input", raw: json.RawMessage(`null`)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assertContainerRequestDecode(t, spec, tc.raw, tc.wantContainerID, tc.wantSemanticError, tc.wantSyntacticError)
		})
	}
}

func assertContainerRequestDecode(
	t *testing.T,
	spec apischema.RouteSpec,
	raw json.RawMessage,
	wantContainerID string,
	wantSemanticError bool,
	wantSyntacticError bool,
) {
	t.Helper()
	decoded, err := spec.Decode(raw)
	if wantSemanticError {
		if _, ok := errors.AsType[*jsonv2.SemanticError](err); !ok {
			t.Fatalf("Decode() error = %v, want *jsonv2.SemanticError", err)
		}
		return
	}
	if wantSyntacticError {
		if _, ok := errors.AsType[*jsontext.SyntacticError](err); !ok {
			t.Fatalf("Decode() error = %v, want *jsontext.SyntacticError", err)
		}
		return
	}
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	request, ok := decoded.(apischema.ContainerIDRequest)
	if !ok {
		t.Fatalf("Decode() result = %T, want apischema.ContainerIDRequest", decoded)
	}
	if request.ContainerID != wantContainerID {
		t.Fatalf("containerId = %q, want %q", request.ContainerID, wantContainerID)
	}
}

func TestEndpointExcludesChannels(t *testing.T) {
	for _, route := range []string{"tasks.watch", "tasks.data", "terminal.open", "container.open", "filebrowser.download_stream"} {
		spec := mustRoute(t, route)
		if spec.Endpoint() {
			t.Fatalf("%s should not generate an endpoint", route)
		}
	}

	for _, route := range []string{"docker.logs.follow", "logs.general.follow", "logs.service.follow"} {
		spec := mustRoute(t, route)
		if spec.Mode != bridgeipc.ModeDuplex || spec.Kind != apischema.KindDuplex {
			t.Fatalf("%s should be a direct channel, got mode=%q kind=%q", route, spec.Mode, spec.Kind)
		}
		if spec.Endpoint() {
			t.Fatalf("%s should not generate an endpoint", route)
		}
	}

	if !mustRoute(t, "system.get_cpu_info").Endpoint() {
		t.Fatal("call route should generate an endpoint")
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

func TestDockerComposeDeclaresTerminalAndProgressContracts(t *testing.T) {
	compose := mustRoute(t, "docker.compose")
	if got, want := compose.Result.GoType, reflect.TypeFor[dockerhandler.ComposeTaskResult](); got != want {
		t.Fatalf("docker.compose result type = %v, want %v", got, want)
	}
	if got, want := compose.Progress.GoType, reflect.TypeFor[dockerhandler.ComposeTaskMessage](); got != want {
		t.Fatalf("docker.compose progress type = %v, want %v", got, want)
	}
}

func TestWithTaskProgressRejectsCallRoutes(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Call() accepted a task progress contract")
		}
	}()
	_ = apischema.Call[apischema.NoRequest, apischema.NoResponse](
		"test.progress_call",
		apischema.WithTaskProgress[apischema.VMCreateProgress](),
	)
}

func TestTaskMetadataBuildersAreAllowlistedTaskRoutes(t *testing.T) {
	want := map[string]bool{
		"filebrowser.compress": true, "filebrowser.extract": true, "filebrowser.copy_batch": true,
		"filebrowser.move_batch": true, "filebrowser.delete_batch": true, "filebrowser.index": true,
		"filebrowser.upload": true, "filebrowser.upload_batch": true,
		"filebrowser.archive": true, "filebrowser.chmod_batch": true, "docker.compose": true,
		"docker.update_container": true,
		"packages.update":         true, "storage.run_smart_test": true, "system.install_capability": true,
	}
	for _, route := range handlers.Routes {
		if route.Metadata == nil {
			continue
		}
		if !want[route.Route] {
			t.Fatalf("%s unexpectedly declares public task metadata", route.Route)
		}
		if route.Kind != apischema.KindTaskRunner || route.Mode != bridgeipc.ModeTask {
			t.Fatalf("%s metadata is not a task route", route.Route)
		}
		delete(want, route.Route)
	}
	if len(want) != 0 {
		t.Fatalf("missing metadata builders: %v", want)
	}
}

func TestTaskRunnerErasesOnlyAtBridgeBoundary(t *testing.T) {
	route := apischema.TaskRunner[apischema.UsernameRequest, apischema.SuccessNameResponse]("test.runner", apischema.SessionTask())
	binding := route.Run(func(_ context.Context, _ *bridgeipc.Task, req apischema.UsernameRequest) (apischema.SuccessNameResponse, error) {
		return apischema.SuccessNameResponse{Success: true, Name: req.Username}, nil
	}, bridgeipc.TaskDefault)
	got, err := binding.Runner(context.Background(), &bridgeipc.Task{}, apischema.UsernameRequest{Username: "ada"})
	if err != nil {
		t.Fatalf("Runner() error = %v", err)
	}
	if got != (apischema.SuccessNameResponse{Success: true, Name: "ada"}) {
		t.Fatalf("Runner() result = %#v", got)
	}
}

func TestCallBindingReturnsTypedResultDirectly(t *testing.T) {
	binding := apischema.Call[apischema.UsernameRequest, apischema.SuccessNameResponse]("test.call_typed").
		Handle(func(_ context.Context, req apischema.UsernameRequest) (apischema.SuccessNameResponse, error) {
			return apischema.SuccessNameResponse{Success: true, Name: req.Username}, nil
		})

	if binding.Call == nil {
		t.Fatal("Call binding has nil direct handler")
	}
	got, err := binding.Call(context.Background(), bridgeipc.Request{
		DecodedValue: apischema.UsernameRequest{Username: "ada"},
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	want := apischema.SuccessNameResponse{Success: true, Name: "ada"}
	if got != want {
		t.Fatalf("Call() result = %#v, want %#v", got, want)
	}
}

func TestHandleVoidRejectsNonVoidResult(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("HandleVoid on a route that declares a real result should panic")
		}
	}()
	apischema.Call[apischema.UsernameRequest, apischema.SuccessResponse]("test.void_mismatch").
		HandleVoid(func(_ context.Context, _ apischema.UsernameRequest) error { return nil })
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
