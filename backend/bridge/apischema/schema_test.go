package apischema_test

import (
	"context"
	"encoding/json"
	"errors"
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

func TestOnlyProgressHandlersRemainJobs(t *testing.T) {
	remainingProgressJobs := map[string]bool{
		"filebrowser.resource_patch": true,
		"virt.create":                true,
	}
	modes := map[bridgeipc.Mode]int{}

	for _, route := range handlers.Routes {
		modes[route.Mode]++
		if route.Kind != apischema.KindHandler || route.Mode != bridgeipc.ModeJob {
			continue
		}
		if !remainingProgressJobs[route.Route] {
			t.Errorf("%s is a progressless handler route but remains a job", route.Route)
			continue
		}
		delete(remainingProgressJobs, route.Route)
	}

	if len(remainingProgressJobs) != 0 {
		t.Errorf("expected progress handler jobs are missing: %v", remainingProgressJobs)
	}
	if got, want := modes[bridgeipc.ModeQuery], 203; got != want {
		t.Errorf("query route count = %d, want %d", got, want)
	}
	if got, want := modes[bridgeipc.ModeJob], 21; got != want {
		t.Errorf("job route count = %d, want %d", got, want)
	}
	if got, want := modes[bridgeipc.ModeDuplex], 6; got != want {
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

func TestJobMetadataBuildersAreAllowlistedRunnerRoutes(t *testing.T) {
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
			t.Fatalf("%s unexpectedly declares public job metadata", route.Route)
		}
		if route.Kind != apischema.KindRunner || route.Mode != bridgeipc.ModeJob {
			t.Fatalf("%s metadata is not a job runner", route.Route)
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

func TestHandleEmitsTypedResult(t *testing.T) {
	binding := apischema.Query[apischema.UsernameRequest, apischema.SuccessNameResponse]("test.typed").
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

func TestHandleErrorEmitsNoResult(t *testing.T) {
	sentinel := errors.New("boom")
	binding := apischema.Query[apischema.UsernameRequest, apischema.SuccessNameResponse]("test.typed_err").
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
// would stop job snapshots from omitting `result`.
func TestNoResponseRoutesEmitNilNotZeroStruct(t *testing.T) {
	typed := apischema.Job[apischema.UsernameRequest, apischema.NoResponse]("test.void_typed").
		Handle(func(_ context.Context, _ apischema.UsernameRequest) (apischema.NoResponse, error) {
			return apischema.NoResponse{}, nil
		})
	void := apischema.Job[apischema.UsernameRequest, apischema.NoResponse]("test.void_short").
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
	apischema.Job[apischema.UsernameRequest, apischema.SuccessResponse]("test.void_mismatch").
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
