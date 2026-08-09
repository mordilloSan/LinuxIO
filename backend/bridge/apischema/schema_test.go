package apischema_test

import (
	"context"
	"encoding/json"
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

func TestOnlyProgressHandlersRemainTasks(t *testing.T) {
	remainingProgressTasks := map[string]bool{
		"filebrowser.resource_patch": true,
		"virt.create":                true,
	}
	modes := map[bridgeipc.Mode]int{}

	for _, route := range handlers.Routes {
		modes[route.Mode]++
		if route.Kind != apischema.KindHandler || route.Mode != bridgeipc.ModeTask {
			continue
		}
		if !remainingProgressTasks[route.Route] {
			t.Errorf("%s is a progressless task route", route.Route)
			continue
		}
		delete(remainingProgressTasks, route.Route)
	}

	if len(remainingProgressTasks) != 0 {
		t.Errorf("expected progress task routes are missing: %v", remainingProgressTasks)
	}
	if got, want := modes[bridgeipc.ModeCall], 203; got != want {
		t.Errorf("call route count = %d, want %d", got, want)
	}
	if got, want := modes[bridgeipc.ModeTask], 18; got != want {
		t.Errorf("task route count = %d, want %d", got, want)
	}
	if got, want := modes[bridgeipc.ModeDuplex], 9; got != want {
		t.Errorf("duplex route count = %d, want %d", got, want)
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

func TestEndpointExcludesChannels(t *testing.T) {
	for _, route := range []string{"tasks.watch", "tasks.data", "terminal.open", "container.open"} {
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
		apischema.WithTaskProgress[apischema.MessageResponse](),
	)
}

func TestTaskMetadataBuildersAreAllowlistedTaskRoutes(t *testing.T) {
	want := map[string]bool{
		"filebrowser.compress": true, "filebrowser.extract": true, "filebrowser.copy_batch": true,
		"filebrowser.move_batch": true, "filebrowser.delete_batch": true, "filebrowser.index": true,
		"filebrowser.upload": true, "filebrowser.upload_batch": true, "filebrowser.download": true,
		"filebrowser.archive": true, "filebrowser.chmod_batch": true, "docker.compose": true,
		"packages.update": true, "storage.run_smart_test": true, "system.install_capability": true,
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

// recordingEmitter captures what a binding emitted so the tests can assert on
// the exact value that reaches the wire — in particular nil vs a zero struct.
type recordingEmitter struct {
	results  []any
	progress []any
}

func (e *recordingEmitter) Data([]byte) error            { return nil }
func (e *recordingEmitter) Progress(p any) error         { e.progress = append(e.progress, p); return nil }
func (e *recordingEmitter) Result(r any) error           { e.results = append(e.results, r); return nil }
func (e *recordingEmitter) Error(err error, _ int) error { return err }
func (e *recordingEmitter) Close(string) error           { return nil }

func TestTaskHandleEmitsTypedResult(t *testing.T) {
	binding := apischema.Task[apischema.UsernameRequest, apischema.SuccessNameResponse]("test.typed").
		Handle(func(_ context.Context, req apischema.UsernameRequest) (apischema.SuccessNameResponse, error) {
			return apischema.SuccessNameResponse{Success: true, Name: req.Username}, nil
		})

	emit := &recordingEmitter{}
	if err := binding.Handle(context.Background(), apischema.UsernameRequest{Username: "ada"}, emit); err != nil {
		t.Fatalf("Handle() error = %v", err)
	}
	want := apischema.SuccessNameResponse{Success: true, Name: "ada"}
	if len(emit.results) != 1 || emit.results[0] != want {
		t.Fatalf("emitted %#v, want one %#v", emit.results, want)
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
	got, err := binding.Call(context.Background(), apischema.UsernameRequest{Username: "ada"})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	want := apischema.SuccessNameResponse{Success: true, Name: "ada"}
	if got != want {
		t.Fatalf("Call() result = %#v, want %#v", got, want)
	}
}

func TestCallRejectsHandleEvents(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("HandleEvents on a Call route should panic")
		}
	}()
	apischema.Call[apischema.NoRequest, apischema.SuccessResponse]("test.call_events").
		HandleEvents(func(context.Context, apischema.NoRequest, bridgeipc.Events) error { return nil })
}

func TestTaskHandleErrorEmitsNoResult(t *testing.T) {
	sentinel := errors.New("boom")
	binding := apischema.Task[apischema.UsernameRequest, apischema.SuccessNameResponse]("test.typed_err").
		Handle(func(_ context.Context, _ apischema.UsernameRequest) (apischema.SuccessNameResponse, error) {
			return apischema.SuccessNameResponse{Success: true}, sentinel
		})

	emit := &recordingEmitter{}
	err := binding.Handle(context.Background(), apischema.UsernameRequest{}, emit)
	if !errors.Is(err, sentinel) {
		t.Fatalf("Handle() error = %v, want %v", err, sentinel)
	}
	if len(emit.results) != 0 {
		t.Fatalf("a failing handler emitted %#v; nothing should reach the wire", emit.results)
	}
}

// NoResponse generates TypeScript `void`. Both binding forms must put nil on the
// wire rather than the zero struct: `{}` would contradict the generated type and
// would stop task snapshots from omitting `result`.
func TestNoResponseRoutesEmitNilNotZeroStruct(t *testing.T) {
	typed := apischema.Task[apischema.UsernameRequest, apischema.NoResponse]("test.void_typed").
		Handle(func(_ context.Context, _ apischema.UsernameRequest) (apischema.NoResponse, error) {
			return apischema.NoResponse{}, nil
		})
	void := apischema.Task[apischema.UsernameRequest, apischema.NoResponse]("test.void_short").
		HandleVoid(func(_ context.Context, _ apischema.UsernameRequest) error {
			return nil
		})

	for name, binding := range map[string]apischema.HandlerBinding{"Handle": typed, "HandleVoid": void} {
		t.Run(name, func(t *testing.T) {
			emit := &recordingEmitter{}
			if err := binding.Handle(context.Background(), apischema.UsernameRequest{}, emit); err != nil {
				t.Fatalf("Handle() error = %v", err)
			}
			if len(emit.results) != 1 {
				t.Fatalf("emitted %d results, want 1", len(emit.results))
			}
			if emit.results[0] != nil {
				t.Fatalf("emitted %#v, want nil so the wire stays null", emit.results[0])
			}
		})
	}
}

func TestHandleVoidRejectsNonVoidResult(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("HandleVoid on a route that declares a real result should panic")
		}
	}()
	apischema.Task[apischema.UsernameRequest, apischema.SuccessResponse]("test.void_mismatch").
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
