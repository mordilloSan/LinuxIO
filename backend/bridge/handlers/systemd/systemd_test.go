package systemd

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient/testdbus"
)

func exportManager(t *testing.T, bus *testdbus.Bus) *testdbus.SystemdManager {
	t.Helper()

	manager := testdbus.NewSystemdManager()
	conn := bus.OwnName(t, dbusclient.SystemdBusName)
	if err := conn.Export(manager, godbus.ObjectPath(dbusclient.SystemdPath), dbusclient.SystemdManagerIface); err != nil {
		t.Fatalf("export systemd manager: %v", err)
	}
	return manager
}

func TestPackageLevelMethodsCallExpectedManagerMethods(t *testing.T) { //nolint:gocognit
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	manager := exportManager(t, bus)

	ctx := context.Background()
	tests := []struct {
		name       string
		invoke     func(t *testing.T)
		wantMethod string
		wantArgs   []any
	}{
		{
			name: "Start",
			invoke: func(t *testing.T) {
				if err := StartUnit(ctx, "demo.service"); err != nil {
					t.Fatalf("StartUnit: %v", err)
				}
			},
			wantMethod: "StartUnit",
			wantArgs:   []any{"demo.service", "replace"},
		},
		{
			name: "Stop",
			invoke: func(t *testing.T) {
				if err := StopUnit(ctx, "demo.service"); err != nil {
					t.Fatalf("StopUnit: %v", err)
				}
			},
			wantMethod: "StopUnit",
			wantArgs:   []any{"demo.service", "replace"},
		},
		{
			name: "Restart",
			invoke: func(t *testing.T) {
				if err := RestartUnit(ctx, "demo.service"); err != nil {
					t.Fatalf("RestartUnit: %v", err)
				}
			},
			wantMethod: "RestartUnit",
			wantArgs:   []any{"demo.service", "replace"},
		},
		{
			name: "ReloadUnit",
			invoke: func(t *testing.T) {
				if err := ReloadUnit(ctx, "demo.service"); err != nil {
					t.Fatalf("ReloadUnit: %v", err)
				}
			},
			wantMethod: "ReloadUnit",
			wantArgs:   []any{"demo.service", "replace"},
		},
		{
			name: "GetUnitFileState",
			invoke: func(t *testing.T) {
				state, err := GetUnitFileState(ctx, "demo.service")
				if err != nil {
					t.Fatalf("GetUnitFileState: %v", err)
				}
				if state != manager.UnitFileState {
					t.Fatalf("GetUnitFileState = %q, want %q", state, manager.UnitFileState)
				}
			},
			wantMethod: "GetUnitFileState",
			wantArgs:   []any{"demo.service"},
		},
		{
			name: "DaemonReload",
			invoke: func(t *testing.T) {
				if err := DaemonReload(ctx); err != nil {
					t.Fatalf("DaemonReload: %v", err)
				}
			},
			wantMethod: "Reload",
			wantArgs:   []any{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manager.ResetCalls()
			tc.invoke(t)

			calls := manager.Calls()
			if len(calls) != 1 {
				t.Fatalf("calls = %d, want 1", len(calls))
			}
			if calls[0].Method != tc.wantMethod {
				t.Fatalf("method = %q, want %q", calls[0].Method, tc.wantMethod)
			}
			if !reflect.DeepEqual(calls[0].Args, tc.wantArgs) {
				t.Fatalf("args = %#v, want %#v", calls[0].Args, tc.wantArgs)
			}
		})
	}
}

func TestPackageLevelEnableDisableReloadManager(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	manager := exportManager(t, bus)

	ctx := context.Background()
	tests := []struct {
		name       string
		invoke     func(t *testing.T)
		wantMethod string
		wantArgs   []any
	}{
		{
			name: "EnableUnit",
			invoke: func(t *testing.T) {
				if err := EnableUnit(ctx, "demo.service"); err != nil {
					t.Fatalf("EnableUnit: %v", err)
				}
			},
			wantMethod: "EnableUnitFiles",
			wantArgs:   []any{[]string{"demo.service"}, false, true},
		},
		{
			name: "DisableUnit",
			invoke: func(t *testing.T) {
				if err := DisableUnit(ctx, "demo.service"); err != nil {
					t.Fatalf("DisableUnit: %v", err)
				}
			},
			wantMethod: "DisableUnitFiles",
			wantArgs:   []any{[]string{"demo.service"}, false},
		},
		{
			name: "MaskUnit",
			invoke: func(t *testing.T) {
				if err := MaskUnit(ctx, "demo.service"); err != nil {
					t.Fatalf("MaskUnit: %v", err)
				}
			},
			wantMethod: "MaskUnitFiles",
			wantArgs:   []any{[]string{"demo.service"}, false, true},
		},
		{
			name: "UnmaskUnit",
			invoke: func(t *testing.T) {
				if err := UnmaskUnit(ctx, "demo.service"); err != nil {
					t.Fatalf("UnmaskUnit: %v", err)
				}
			},
			wantMethod: "UnmaskUnitFiles",
			wantArgs:   []any{[]string{"demo.service"}, false},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manager.ResetCalls()
			tc.invoke(t)
			assertCallsWithReload(t, manager.Calls(), tc.wantMethod, tc.wantArgs)
		})
	}
}

func assertCallsWithReload(t *testing.T, calls []testdbus.SystemdCall, wantMethod string, wantArgs []any) {
	t.Helper()
	if len(calls) != 2 {
		t.Fatalf("calls = %d, want 2", len(calls))
	}
	if calls[0].Method != wantMethod {
		t.Fatalf("method = %q, want %q", calls[0].Method, wantMethod)
	}
	if !reflect.DeepEqual(calls[0].Args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", calls[0].Args, wantArgs)
	}
	if calls[1].Method != "Reload" {
		t.Fatalf("trailing method = %q, want %q", calls[1].Method, "Reload")
	}
	if len(calls[1].Args) != 0 {
		t.Fatalf("trailing args = %#v, want []", calls[1].Args)
	}
}

func TestRequireUnitNameRejectsBlankInput(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	manager := exportManager(t, bus)

	err := StartUnit(context.Background(), "   ")
	if err == nil {
		t.Fatal("StartUnit returned nil error for blank unit name")
	}
	if !strings.Contains(err.Error(), "unit name is required") {
		t.Fatalf("error = %q, want unit name validation", err)
	}
	if calls := manager.Calls(); len(calls) != 0 {
		t.Fatalf("manager received %d calls, want 0", len(calls))
	}
}

func TestPackageLevelOperationHonorsCanceledContextBeforeConnecting(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := StartUnit(ctx, "demo.service")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("StartUnit error = %v, want context.Canceled", err)
	}
}
