package tasks

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"io"
	"net"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// dispatchRouteError sends a payload every tasks.* decoder rejects, so a
// registered route fails with ErrInvalidArgs before its handler runs, a
// privileged one asked by an unprivileged session fails with ErrForbidden
// first, and an unregistered one fails with ErrRouteNotFound.
func dispatchRouteError(t *testing.T, router *bridgeipc.Router, route string, privileged bool) error {
	t.Helper()
	server, client := net.Pipe()
	defer client.Close()
	go func() { _, _ = io.Copy(io.Discard, client) }()
	defer server.Close()
	return router.Dispatch(context.Background(), server, bridgeipc.Request{
		Route:      route,
		RawRequest: jsontext.Value("1"),
		Session:    &session.Session{SessionID: "s", User: session.User{Username: "alice", UID: 1000}, Privileged: privileged},
	})
}

// Every tasks.* route the contract declares must be installed by
// bridgeipc.TaskService.RegisterRoutes, privileged when the contract says so.
func TestRoutesAreRegisteredByTaskService(t *testing.T) {
	router := bridgeipc.NewRouter(bridgeipc.NewTaskService())
	RegisterHandlers(runtime.Runtime{}, router)

	if err := dispatchRouteError(t, router, "tasks.unknown", true); !errors.Is(err, bridgeipc.ErrRouteNotFound) {
		t.Fatalf("unknown route error = %v, want ErrRouteNotFound", err)
	}
	if len(Routes) == 0 {
		t.Fatal("tasks contract declares no routes")
	}
	for _, name := range []string{"tasks.history", "tasks.output", "tasks.host"} {
		if !slices.ContainsFunc(Routes, func(spec apischema.RouteSpec) bool { return spec.Route == name }) {
			t.Errorf("tasks contract does not declare %s", name)
		}
	}
	for _, spec := range Routes {
		if err := dispatchRouteError(t, router, spec.Route, true); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
			t.Errorf("%s: dispatch error = %v, want ErrInvalidArgs (route not registered by TaskService?)", spec.Route, err)
		}
		want := bridgeipc.ErrInvalidArgs
		if spec.Privileged {
			want = bridgeipc.ErrForbidden
		}
		if err := dispatchRouteError(t, router, spec.Route, false); !errors.Is(err, want) {
			t.Errorf("%s: unprivileged dispatch error = %v, want %v (contract Privileged %v)", spec.Route, err, want, spec.Privileged)
		}
	}
}
