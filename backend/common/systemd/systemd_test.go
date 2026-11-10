package systemd

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient/testdbus"
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

// invocationManager serves GetUnit for the units it knows and answers it with
// the D-Bus error named in fail for those units; the unit object answers
// property reads.
type invocationManager struct {
	paths map[string]godbus.ObjectPath
	fail  map[string]string
}

func (m invocationManager) GetUnit(name string) (godbus.ObjectPath, *godbus.Error) {
	if errName, ok := m.fail[name]; ok {
		return "", &godbus.Error{Name: errName, Body: []any{"refused"}}
	}
	path, ok := m.paths[name]
	if !ok {
		return "", &godbus.Error{Name: "org.freedesktop.systemd1.NoSuchUnit", Body: []any{"not loaded"}}
	}
	return path, nil
}

type unitProperties map[string]godbus.Variant

func (p unitProperties) Get(iface, property string) (godbus.Variant, *godbus.Error) {
	value, ok := p[iface+"."+property]
	if !ok {
		return godbus.Variant{}, godbus.MakeFailedError(errors.New("unknown property"))
	}
	return value, nil
}

func TestGetInvocationID(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	conn := bus.OwnName(t, dbusclient.SystemdBusName)
	manager := invocationManager{paths: map[string]godbus.ObjectPath{
		"started.service": "/org/freedesktop/systemd1/unit/started_2eservice",
		"idle.service":    "/org/freedesktop/systemd1/unit/idle_2eservice",
	}}
	if err := conn.Export(manager, godbus.ObjectPath(dbusclient.SystemdPath), dbusclient.SystemdManagerIface); err != nil {
		t.Fatalf("export manager: %v", err)
	}
	for name, id := range map[string][]byte{
		"started.service": {0xde, 0xad, 0xbe, 0xef, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12},
		"idle.service":    {},
	} {
		props := unitProperties{dbusclient.SystemdUnitIface + ".InvocationID": godbus.MakeVariant(id)}
		if err := conn.Export(props, manager.paths[name], dbusclient.PropertiesIface); err != nil {
			t.Fatalf("export unit %s: %v", name, err)
		}
	}

	for name, want := range map[string]string{
		"started.service": "deadbeef0102030405060708090a0b0c",
		"idle.service":    "",
		"missing.service": "",
	} {
		got, err := GetInvocationID(context.Background(), name)
		if err != nil {
			t.Fatalf("GetInvocationID(%s): %v", name, err)
		}
		if got != want {
			t.Fatalf("GetInvocationID(%s) = %q, want %q", name, got, want)
		}
	}
}

func TestUnitRunsInvocation(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	conn := bus.OwnName(t, dbusclient.SystemdBusName)
	runID := []byte{0xde, 0xad, 0xbe, 0xef, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	const run = "deadbeef0102030405060708090a0b0c"
	// Each loaded unit runs invocation run in the state given; "" leaves a
	// property out, so reading it fails.
	units := map[string]struct{ invocation, state string }{
		"activating.service":   {run, "activating"},
		"active.service":       {run, "active"},
		"reloading.service":    {run, "reloading"},
		"deactivating.service": {run, "deactivating"},
		"inactive.service":     {run, "inactive"},
		"failed.service":       {run, "failed"},
		"stateless.service":    {run, ""},
		"anonymous.service":    {"", "active"},
	}
	manager := invocationManager{paths: map[string]godbus.ObjectPath{}, fail: map[string]string{
		"busy.service":    "org.freedesktop.DBus.Error.LimitsExceeded",
		"reexec.service":  "org.freedesktop.DBus.Error.ServiceUnknown",
		"timeout.service": "org.freedesktop.DBus.Error.NoReply",
	}}
	for name := range units {
		manager.paths[name] = godbus.ObjectPath("/org/freedesktop/systemd1/unit/" + strings.TrimSuffix(name, ".service"))
	}
	if err := conn.Export(manager, godbus.ObjectPath(dbusclient.SystemdPath), dbusclient.SystemdManagerIface); err != nil {
		t.Fatalf("export manager: %v", err)
	}
	for name, unit := range units {
		props := unitProperties{}
		if unit.invocation != "" {
			props[dbusclient.SystemdUnitIface+".InvocationID"] = godbus.MakeVariant(runID)
		}
		if unit.state != "" {
			props[dbusclient.SystemdUnitIface+".ActiveState"] = godbus.MakeVariant(unit.state)
		}
		if err := conn.Export(props, manager.paths[name], dbusclient.PropertiesIface); err != nil {
			t.Fatalf("export unit %s: %v", name, err)
		}
	}

	for _, tt := range []struct {
		unit, invocation string
		want             bool
	}{
		{"activating.service", run, true},
		{"active.service", run, true},
		{"reloading.service", run, true},
		{"deactivating.service", run, true},
		{"inactive.service", run, false},
		{"failed.service", run, false},
		{"active.service", "0123456789abcdef0123456789abcdef", false},
		{"missing.service", run, false}, // NoSuchUnit: not loaded, so not running
	} {
		got, err := UnitRunsInvocation(t.Context(), tt.unit, tt.invocation)
		if err != nil {
			t.Fatalf("UnitRunsInvocation(%s, %s): %v", tt.unit, tt.invocation, err)
		}
		if got != tt.want {
			t.Errorf("UnitRunsInvocation(%s, %s) = %v, want %v", tt.unit, tt.invocation, got, tt.want)
		}
	}
	// Systemd could not be asked: the caller must leave the run as it is.
	for _, unit := range []string{"busy.service", "reexec.service", "timeout.service", "stateless.service", "anonymous.service"} {
		if got, err := UnitRunsInvocation(t.Context(), unit, run); err == nil || got {
			t.Errorf("UnitRunsInvocation(%s) = %v, %v; want false and an error", unit, got, err)
		}
	}
}

func TestSystemStopping(t *testing.T) {
	for state, want := range map[string]bool{"running": false, "stopping": true} {
		t.Run(state, func(t *testing.T) {
			bus := testdbus.Start(t)
			bus.SetSystemBus(t)
			conn := bus.OwnName(t, dbusclient.SystemdBusName)
			props := unitProperties{dbusclient.SystemdManagerIface + ".SystemState": godbus.MakeVariant(state)}
			if err := conn.Export(props, godbus.ObjectPath(dbusclient.SystemdPath), dbusclient.PropertiesIface); err != nil {
				t.Fatalf("export manager properties: %v", err)
			}
			if got := SystemStopping(t.Context()); got != want {
				t.Fatalf("SystemStopping = %v, want %v", got, want)
			}
		})
	}
}

func TestSystemStoppingIsFalseWithoutAManager(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	if SystemStopping(t.Context()) {
		t.Fatal("SystemStopping is true with no manager on the bus")
	}
}
