package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

// dispatchRouteError sends a payload every tasks.* decoder rejects, so a
// registered route fails with ErrInvalidArgs before its handler runs and an
// unregistered one fails with ErrRouteNotFound.
func dispatchRouteError(t *testing.T, router *bridgeipc.Router, route string) error {
	t.Helper()
	server, client := net.Pipe()
	defer client.Close()
	go func() { _, _ = io.Copy(io.Discard, client) }()
	defer server.Close()
	return router.Dispatch(context.Background(), server, bridgeipc.Request{
		Route:      route,
		RawRequest: json.RawMessage("1"),
	})
}

// Every tasks.* route the contract declares must be installed by
// bridgeipc.TaskService.RegisterRoutes.
func TestRoutesAreRegisteredByTaskService(t *testing.T) {
	router := bridgeipc.NewRouter(bridgeipc.NewTaskService())
	RegisterHandlers(runtime.Runtime{}, router)

	if err := dispatchRouteError(t, router, "tasks.unknown"); !errors.Is(err, bridgeipc.ErrRouteNotFound) {
		t.Fatalf("unknown route error = %v, want ErrRouteNotFound", err)
	}
	if len(Routes) == 0 {
		t.Fatal("tasks contract declares no routes")
	}
	for _, spec := range Routes {
		if err := dispatchRouteError(t, router, spec.Route); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
			t.Errorf("%s: dispatch error = %v, want ErrInvalidArgs (route not registered by TaskService?)", spec.Route, err)
		}
	}
}
