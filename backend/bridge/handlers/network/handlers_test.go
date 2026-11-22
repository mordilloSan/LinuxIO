package network

import (
	"reflect"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/network/internal/hostbridge"
)

// Restoring the libvirt hook writes under /etc and restarts the libvirt
// daemons, so a session without sudo is refused before the handler runs. It
// takes nothing from the request: the paths are fixed.
func TestRestoreBridgeHookRoute(t *testing.T) {
	for _, route := range Routes {
		if route.Route != "network.restore_bridge_hook" {
			continue
		}
		if !route.Privileged || route.Kind != apischema.KindMutation || route.RetrySafe {
			t.Fatalf("route = %+v, want a privileged mutation that is not retried", route)
		}
		if route.Request.GoType != reflect.TypeFor[apischema.NoRequest]() {
			t.Fatalf("request = %+v, want NoRequest", route.Request)
		}
		return
	}
	t.Fatal("network.restore_bridge_hook is not bound")
}

// The hook's state reaches both answers under the contract's names.
func TestMapBridgeHook(t *testing.T) {
	if mapBridgeHook(nil) != nil {
		t.Fatal("a firewall that lets bridged traffic through reports no hook")
	}
	for state, want := range map[hostbridge.HookState]apischema.NetworkBridgeHookState{
		hostbridge.HookInstalled:     apischema.NetworkBridgeHookInstalled,
		hostbridge.HookMissing:       apischema.NetworkBridgeHookMissing,
		hostbridge.HookNotExecutable: apischema.NetworkBridgeHookNotExecutable,
	} {
		got := mapBridgeHook(&hostbridge.HookStatus{State: state, Restorable: true})
		if got == nil || got.State != want || !got.Restorable {
			t.Fatalf("mapBridgeHook(%s) = %#v, want %s", state, got, want)
		}
	}
	options := mapBridgeOptions(hostbridge.BridgeOptions{Hook: &hostbridge.HookStatus{State: hostbridge.HookMissing}})
	if options.BridgeHook == nil || options.BridgeHook.State != apischema.NetworkBridgeHookMissing || options.BridgeHook.Restorable {
		t.Fatalf("bridge options hook = %#v", options.BridgeHook)
	}
}
