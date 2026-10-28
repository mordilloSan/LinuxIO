package apischema_test

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"reflect"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers"
	dockerhandler "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/docker"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func TestRoutesAreUniqueAndComplete(t *testing.T) {
	modeOfKind := map[apischema.Kind]bridgeipc.Mode{
		apischema.KindHandler:  bridgeipc.ModeCall,
		apischema.KindDuplex:   bridgeipc.ModeDuplex,
		apischema.KindMutation: bridgeipc.ModeMutation,
	}
	seen := make(map[string]apischema.RouteSpec, len(handlers.Routes))
	for _, route := range handlers.Routes {
		if route.Route == "" {
			t.Fatal("empty route")
		}
		mode, ok := modeOfKind[route.Kind]
		if !ok {
			t.Fatalf("%s has invalid kind %q", route.Route, route.Kind)
		}
		if route.Mode != mode {
			t.Fatalf("%s is %s kind but has mode %q, want %q", route.Route, route.Kind, route.Mode, mode)
		}
		if _, exists := seen[route.Route]; exists {
			t.Fatalf("duplicate route %s", route.Route)
		}
		seen[route.Route] = route
	}

	for _, route := range []string{
		"config.get",
		"config.get_ui",
		"config.set",
		"config.set_ui",
		"system.get_cpu_info",
		"monitoring.get_live",
		"monitoring.get_processes",
		"monitoring.get_programs",
		"monitoring.refresh_smart",
		"monitoring.check_database",
		"monitoring.maintain_database",
		"network.get_bridge_options",
		"network.create_bridge",
		"network.start_bridge_handoff",
		"network.get_bridge_handoff",
		"network.confirm_bridge_handoff",
		"network.revert_bridge_handoff",
		"docker.update_container",
		"tasks.watch",
		"terminal.open",
		"assistant.open",
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

func TestRetrySafeRoutesAreExplicitCalls(t *testing.T) {
	for _, route := range handlers.Routes {
		if route.RetrySafe && (route.Mode != bridgeipc.ModeCall || !route.Endpoint()) {
			t.Errorf("%s is retry-safe but is not a public Call", route.Route)
		}
	}
	for _, route := range []string{"schedules.list", "schedules.get", "config.get", "config.get_ui", "docker.inspect_container", "system.get_cpu_info", "monitoring.get_live", "monitoring.get_processes", "monitoring.get_programs", "storage.get_topology", "tasks.get", "virt.preflight", "virt.templates", "network.get_bridge_options", "network.get_bridge_handoff", "network.confirm_bridge_handoff", "network.revert_bridge_handoff"} {
		if !mustRoute(t, route).RetrySafe {
			t.Errorf("%s should be explicitly retry-safe", route)
		}
	}
	for _, route := range []string{
		"schedules.run_now",
		"schedules.stop",
		"docker.check_updates",
		"docker.get_icon",
		"docker.get_icon_uri",
		"docker.start_container",
		"network.get_network_info",
		"network.create_bridge",
		"network.start_bridge_handoff",
		"system.get_health_summary",
		"tasks.cancel",
		"terminal.list_shells",
	} {
		if mustRoute(t, route).RetrySafe {
			t.Errorf("%s should default to no retry", route)
		}
	}
}

func TestMonitoringMaintenanceRoutesArePrivileged(t *testing.T) {
	for _, route := range []string{"monitoring.refresh_smart", "monitoring.check_database", "monitoring.maintain_database"} {
		spec := mustRoute(t, route)
		if !spec.Privileged {
			t.Errorf("%s should be privileged", route)
		}
		if spec.RetrySafe {
			t.Errorf("%s should not be retry-safe", route)
		}
	}
}

func TestRootOnlyStorageAndDockerRoutesArePrivileged(t *testing.T) {
	for _, route := range []string{
		"storage.create_lv", "storage.delete_lv", "storage.resize_lv",
		"storage.mount_nfs", "storage.unmount_nfs", "storage.remount_nfs",
		"storage.mount_cifs", "storage.unmount_cifs", "storage.remount_cifs",
		"storage.unmount_filesystem", "storage.set_btrfs_maintenance", "storage.run_smart_test",
		"docker.set_container_auto_update",
	} {
		if !mustRoute(t, route).Privileged {
			t.Errorf("%s should be privileged", route)
		}
	}
}

func TestRetrySafeRejectsNonCallRoutes(t *testing.T) {
	for name, build := range map[string]func(){
		"duplex": func() {
			_ = apischema.DuplexRoute[apischema.NoRequest, apischema.NoResponse]("test.retry_duplex", apischema.RetrySafe())
		},
		"mutation": func() {
			_ = apischema.MutationVoid("test.retry_mutation", func(context.Context, apischema.NoRequest) error { return nil }, apischema.RetrySafe())
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
			decoded, err := spec.Decode(jsontext.Value(tc.raw))
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
		raw                jsontext.Value
		wantContainerID    string
		wantSemanticError  bool
		wantSyntacticError bool
	}{
		{name: "valid object", raw: jsontext.Value(`{"containerId":"web"}`), wantContainerID: "web"},
		{name: "unknown field", raw: jsontext.Value(`{"containerId":"web","unexpected":true}`), wantSemanticError: true},
		{name: "case-mismatched field", raw: jsontext.Value(`{"ContainerId":"web"}`), wantSemanticError: true},
		{name: "duplicate field", raw: jsontext.Value(`{"containerId":"web","containerId":"db"}`), wantSyntacticError: true},
		{name: "invalid UTF-8", raw: jsontext.Value("{\"containerId\":\"\xff\"}"), wantSyntacticError: true},
		{name: "trailing JSON value", raw: jsontext.Value(`{"containerId":"web"} {}`), wantSyntacticError: true},
		{name: "scalar type mismatch", raw: jsontext.Value(`{"containerId":123}`), wantSemanticError: true},
		{name: "empty input", raw: nil},
		{name: "null input", raw: jsontext.Value(`null`)},
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
	raw jsontext.Value,
	wantContainerID string,
	wantSemanticError bool,
	wantSyntacticError bool,
) {
	t.Helper()
	decoded, err := spec.Decode(raw)
	if wantSemanticError {
		if _, ok := errors.AsType[*json.SemanticError](err); !ok {
			t.Fatalf("Decode() error = %v, want *json.SemanticError", err)
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
	for _, route := range []string{"tasks.watch", "tasks.data", "terminal.open", "assistant.open", "container.open", "filebrowser.download_stream"} {
		spec := mustRoute(t, route)
		if spec.Endpoint() {
			t.Fatalf("%s should not generate an endpoint", route)
		}
	}

	for _, route := range []string{"docker.logs.follow", "docker.logs.follow_all", "logs.general.follow", "logs.service.follow"} {
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

func TestProgressRejectsCallRoutes(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Call() accepted a task progress contract")
		}
	}()
	_ = apischema.Call[apischema.NoRequest, apischema.NoResponse]("test.progress_call", apischema.Progress[apischema.VMCreateProgress]())
}

func TestBrowserBoundRejectsNonMutationRoutes(t *testing.T) {
	for name, build := range map[string]func(){
		"call": func() {
			_ = apischema.Call[apischema.NoRequest, apischema.NoResponse]("test.browser_call", apischema.BrowserBound())
		},
		"duplex": func() {
			_ = apischema.DuplexRoute[apischema.NoRequest, apischema.NoResponse]("test.browser_duplex", apischema.BrowserBound())
		},
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("non-Mutation route accepted BrowserBound")
				}
			}()
			build()
		})
	}
}

type poolRequest struct {
	UUID string `json:"uuid"`
}

func (r poolRequest) Target() []string { return []string{r.UUID} }

func TestMutationInfersTypesAndDetectsTarget(t *testing.T) {
	targeted := apischema.Mutation("test.targeted", func(context.Context, poolRequest) (apischema.SuccessResponse, error) {
		return apischema.SuccessResponse{}, nil
	}, apischema.Progress[apischema.VMCreateProgress](), apischema.BrowserBound())
	untargeted := apischema.Mutation("test.untargeted", func(context.Context, apischema.NoRequest) (apischema.SuccessResponse, error) {
		return apischema.SuccessResponse{}, nil
	})

	spec := targeted.Route
	if spec.Kind != apischema.KindMutation || spec.Mode != bridgeipc.ModeMutation {
		t.Fatalf("kind = %q, mode = %q; want mutation", spec.Kind, spec.Mode)
	}
	if spec.Request.GoType != reflect.TypeFor[poolRequest]() || spec.Result.GoType != reflect.TypeFor[apischema.SuccessResponse]() {
		t.Fatalf("request = %v, result = %v; want the handler's types", spec.Request.GoType, spec.Result.GoType)
	}
	if spec.Progress.GoType != reflect.TypeFor[apischema.VMCreateProgress]() || !spec.BrowserBound {
		t.Fatalf("progress = %v, browser bound = %t; want the declared options", spec.Progress.GoType, spec.BrowserBound)
	}
	if !spec.Targeted {
		t.Fatal("a request type with Target() did not mark the route targeted")
	}
	if untargeted.Route.Targeted {
		t.Fatal("a request type without Target() marked the route targeted")
	}

	set := apischema.Bindings(targeted, untargeted)
	set.Register(bridgeipc.NewRouter(bridgeipc.NewTaskService()))
	if got := len(set.Routes()); got != 2 {
		t.Fatalf("binding set routes = %d, want 2", got)
	}
}

func TestResumablePanicsWithoutPrivileged(t *testing.T) {
	run := func(context.Context, poolRequest) error { return nil }
	hooks := apischema.ResumeHooks[poolRequest, apischema.NoResponse]{
		Suspend: func(context.Context, poolRequest, jsontext.Value) (jsontext.Value, error) { return nil, nil },
		Resume: func(context.Context, poolRequest, jsontext.Value) (apischema.NoResponse, error) {
			return apischema.NoResponse{}, nil
		},
	}

	resumable := apischema.MutationVoid("test.resumable", run, apischema.Privileged()).Resumable(hooks)
	if !resumable.Route.Resumable {
		t.Fatal("Resumable() did not mark the route resumable")
	}
	apischema.Bindings(resumable).Register(bridgeipc.NewRouter(bridgeipc.NewTaskService()))

	defer func() {
		if recover() == nil {
			t.Fatal("Resumable() accepted a route without Privileged()")
		}
	}()
	_ = apischema.MutationVoid("test.unprivileged_resumable", run).Resumable(hooks)
}

type passwordRequest struct {
	User     string `json:"user"`
	Password string `json:"password" secret:"true"`
}

// A Mutation records its request without the members tagged secret, so it
// cannot resume from that record.
func TestRedactedMutationRecordsItsRequestWithoutTheSecret(t *testing.T) {
	binding := apischema.MutationVoid("test.redacted", func(context.Context, passwordRequest) error { return nil }, apischema.Privileged())
	router := bridgeipc.NewRouter(bridgeipc.NewTaskService())
	apischema.Bindings(binding).Register(router)
	route, _ := router.Route("test.redacted")
	if got := string(route.RecordedRequest(jsontext.Value(`{"user":"bob","password":"hunter2"}`))); got != `{"user":"bob"}` {
		t.Fatalf("recorded request = %s, want it without the password", got)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("Resumable() accepted a route that redacts its request")
		}
	}()
	_ = binding.Resumable(apischema.ResumeHooks[passwordRequest, apischema.NoResponse]{
		Suspend: func(context.Context, passwordRequest, jsontext.Value) (jsontext.Value, error) { return nil, nil },
		Resume: func(context.Context, passwordRequest, jsontext.Value) (apischema.NoResponse, error) {
			return apischema.NoResponse{}, nil
		},
	})
}

func TestMutationVoidRequiresNoResponse(t *testing.T) {
	binding := apischema.MutationVoid("test.void_mutation", func(context.Context, poolRequest) error { return nil })
	if !binding.Route.Result.Void() {
		t.Fatalf("MutationVoid result = %v, want NoResponse", binding.Route.Result.GoType)
	}
}

func TestTaskMetadataBuildersAreAllowlisted(t *testing.T) {
	want := map[string]bool{
		"filebrowser.compress": true, "filebrowser.extract": true, "filebrowser.copy_batch": true,
		"filebrowser.move_batch": true, "filebrowser.delete_batch": true, "filebrowser.trash_batch": true,
		"filebrowser.trash_restore": true, "filebrowser.trash_purge": true, "filebrowser.index": true,
		"filebrowser.upload": true, "filebrowser.upload_batch": true,
		"filebrowser.archive": true, "filebrowser.chmod_batch": true, "docker.compose": true,
		"docker.apply_daemon_storage": true,
		"packages.update":             true, "storage.run_smart_test": true, "system.install_capability": true,
		"storage.btrfs_snapshot_rollback": true, "storage.btrfs_device_remove": true, "storage.btrfs_device_replace": true,
		"storage.btrfs_balance": true, "storage.btrfs_scrub_start": true, "storage.btrfs_replication_run": true,
		"storage.btrfs_pool_create": true, "storage.partition_create": true, "storage.partition_format": true,
	}
	for _, route := range handlers.Routes {
		if route.Metadata == nil {
			continue
		}
		if !want[route.Route] {
			t.Fatalf("%s unexpectedly declares public task metadata", route.Route)
		}
		delete(want, route.Route)
	}
	if len(want) != 0 {
		t.Fatalf("missing metadata builders: %v", want)
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
