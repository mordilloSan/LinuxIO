package memory

import (
	"reflect"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func routeSpec(t *testing.T, name string) apischema.RouteSpec {
	t.Helper()
	for _, route := range Routes {
		if route.Route == name {
			return route
		}
	}
	t.Fatalf("%s is not registered", name)
	return apischema.RouteSpec{}
}

// The swap status reads world-readable files under /proc, /sys and /etc, so a
// session without sudo can read it; it is safe to retry.
func TestGetSwapStatusIsAnOpenRetrySafeCall(t *testing.T) {
	route := routeSpec(t, "memory.get_swap_status")
	if route.Mode != bridgeipc.ModeCall || !route.RetrySafe || route.Privileged {
		t.Fatalf("route = mode %q retry-safe %v privileged %v, want an unprivileged retry-safe call", route.Mode, route.RetrySafe, route.Privileged)
	}
}

// The writes change swap, sysctls, systemd units and files under /etc, so
// they run only for a sudo session; the two long ones report progress.
func TestMemoryWritesArePrivilegedMutations(t *testing.T) {
	progress := reflect.TypeFor[apischema.SwapProgress]()
	tests := []struct {
		name         string
		wantProgress bool
		wantResult   reflect.Type
	}{
		{name: "memory.set_swap_mode", wantProgress: true, wantResult: reflect.TypeFor[apischema.SwapModeResult]()},
		{name: "memory.set_swappiness", wantResult: reflect.TypeFor[apischema.SwapStatus]()},
		{name: "memory.drain_swap", wantProgress: true, wantResult: reflect.TypeFor[apischema.SwapStatus]()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route := routeSpec(t, test.name)
			if route.Kind != apischema.KindMutation || !route.Privileged {
				t.Fatalf("route = kind %q privileged %v, want a privileged mutation", route.Kind, route.Privileged)
			}
			if got := route.Progress.GoType == progress; got != test.wantProgress {
				t.Fatalf("progress = %v, want SwapProgress %v", route.Progress.GoType, test.wantProgress)
			}
			if route.Result.GoType != test.wantResult {
				t.Fatalf("result = %v, want %v", route.Result.GoType, test.wantResult)
			}
		})
	}
}
