package systemd

import (
	"reflect"
	"strings"
	"testing"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/testdbus"
)

func exportManager(t *testing.T, bus *testdbus.Bus) *testdbus.SystemdManager {
	t.Helper()

	manager := testdbus.NewSystemdManager()
	conn := bus.OwnName(t, systemdBusName)
	if err := conn.Export(manager, godbus.ObjectPath(systemdObjectPath), systemdMgrIface); err != nil {
		t.Fatalf("export systemd manager: %v", err)
	}
	return manager
}

func TestManagerOperationsCallExpectedMethods(t *testing.T) { //nolint:gocognit
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	manager := exportManager(t, bus)

	tests := []struct {
		name       string
		invoke     func(t *testing.T)
		wantMethod string
		wantArgs   []any
	}{
		{
			name: "StartUnit",
			invoke: func(t *testing.T) {
				if err := StartUnit("demo.service"); err != nil {
					t.Fatalf("StartUnit: %v", err)
				}
			},
			wantMethod: "StartUnit",
			wantArgs:   []any{"demo.service", "replace"},
		},
		{
			name: "StopUnit",
			invoke: func(t *testing.T) {
				if err := StopUnit("demo.service"); err != nil {
					t.Fatalf("StopUnit: %v", err)
				}
			},
			wantMethod: "StopUnit",
			wantArgs:   []any{"demo.service", "replace"},
		},
		{
			name: "RestartUnit",
			invoke: func(t *testing.T) {
				if err := RestartUnit("demo.service"); err != nil {
					t.Fatalf("RestartUnit: %v", err)
				}
			},
			wantMethod: "RestartUnit",
			wantArgs:   []any{"demo.service", "replace"},
		},
		{
			name: "ReloadUnit",
			invoke: func(t *testing.T) {
				if err := ReloadUnit("demo.service"); err != nil {
					t.Fatalf("ReloadUnit: %v", err)
				}
			},
			wantMethod: "ReloadUnit",
			wantArgs:   []any{"demo.service", "replace"},
		},
		{
			name: "EnableUnit",
			invoke: func(t *testing.T) {
				if err := EnableUnit("demo.service"); err != nil {
					t.Fatalf("EnableUnit: %v", err)
				}
			},
			wantMethod: "EnableUnitFiles",
			wantArgs:   []any{[]string{"demo.service"}, false, true},
		},
		{
			name: "DisableUnit",
			invoke: func(t *testing.T) {
				if err := DisableUnit("demo.service"); err != nil {
					t.Fatalf("DisableUnit: %v", err)
				}
			},
			wantMethod: "DisableUnitFiles",
			wantArgs:   []any{[]string{"demo.service"}, false},
		},
		{
			name: "MaskUnit",
			invoke: func(t *testing.T) {
				if err := MaskUnit("demo.service"); err != nil {
					t.Fatalf("MaskUnit: %v", err)
				}
			},
			wantMethod: "MaskUnitFiles",
			wantArgs:   []any{[]string{"demo.service"}, false, true},
		},
		{
			name: "UnmaskUnit",
			invoke: func(t *testing.T) {
				if err := UnmaskUnit("demo.service"); err != nil {
					t.Fatalf("UnmaskUnit: %v", err)
				}
			},
			wantMethod: "UnmaskUnitFiles",
			wantArgs:   []any{[]string{"demo.service"}, false},
		},
		{
			name: "GetUnitFileState",
			invoke: func(t *testing.T) {
				state, err := GetUnitFileState("demo.service")
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
				if err := DaemonReload(); err != nil {
					t.Fatalf("DaemonReload: %v", err)
				}
			},
			wantMethod: "Reload",
			wantArgs:   []any{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runManagerOperationTest(t, manager, tc.invoke, tc.wantMethod, tc.wantArgs)
		})
	}
}

func runManagerOperationTest(t *testing.T, manager *testdbus.SystemdManager, invoke func(t *testing.T), wantMethod string, wantArgs []any) {
	t.Helper()

	manager.ResetCalls()
	invoke(t)

	calls := manager.Calls()
	assertSingleManagerCall(t, calls, wantMethod, wantArgs)
}

func assertSingleManagerCall(t *testing.T, calls []testdbus.SystemdCall, wantMethod string, wantArgs []any) {
	t.Helper()

	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	if calls[0].Method != wantMethod {
		t.Fatalf("method = %q, want %q", calls[0].Method, wantMethod)
	}
	if !reflect.DeepEqual(calls[0].Args, wantArgs) {
		t.Fatalf("args = %#v, want %#v", calls[0].Args, wantArgs)
	}
}

func TestRequireUnitNameRejectsBlankInput(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	manager := exportManager(t, bus)

	err := StartUnit("   ")
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
