package systemd

import (
	"context"
	"reflect"
	"testing"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/testdbus"
)

func exportManager(t *testing.T, bus *testdbus.Bus) *testdbus.SystemdManager {
	t.Helper()

	manager := testdbus.NewSystemdManager()
	conn := bus.OwnName(t, "org.freedesktop.systemd1")
	if err := conn.Export(manager, godbus.ObjectPath("/org/freedesktop/systemd1"), "org.freedesktop.systemd1.Manager"); err != nil {
		t.Fatalf("export systemd manager: %v", err)
	}
	return manager
}

func TestClientMethodsCallExpectedManagerMethods(t *testing.T) { //nolint:gocognit
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	manager := exportManager(t, bus)

	client, err := New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer client.Close()

	ctx := context.Background()
	tests := []struct {
		name       string
		invoke     func(t *testing.T)
		wantMethod string
		wantArgs   []any
	}{
		{
			name: "Enable",
			invoke: func(t *testing.T) {
				if err := client.Enable(ctx, "demo.service"); err != nil {
					t.Fatalf("Enable: %v", err)
				}
			},
			wantMethod: "EnableUnitFiles",
			wantArgs:   []any{[]string{"demo.service"}, false, true},
		},
		{
			name: "Disable",
			invoke: func(t *testing.T) {
				if err := client.Disable(ctx, "demo.service"); err != nil {
					t.Fatalf("Disable: %v", err)
				}
			},
			wantMethod: "DisableUnitFiles",
			wantArgs:   []any{[]string{"demo.service"}, false},
		},
		{
			name: "Start",
			invoke: func(t *testing.T) {
				if err := client.Start(ctx, "demo.service"); err != nil {
					t.Fatalf("Start: %v", err)
				}
			},
			wantMethod: "StartUnit",
			wantArgs:   []any{"demo.service", "replace"},
		},
		{
			name: "Stop",
			invoke: func(t *testing.T) {
				if err := client.Stop(ctx, "demo.service"); err != nil {
					t.Fatalf("Stop: %v", err)
				}
			},
			wantMethod: "StopUnit",
			wantArgs:   []any{"demo.service", "replace"},
		},
		{
			name: "Restart",
			invoke: func(t *testing.T) {
				if err := client.Restart(ctx, "demo.service"); err != nil {
					t.Fatalf("Restart: %v", err)
				}
			},
			wantMethod: "RestartUnit",
			wantArgs:   []any{"demo.service", "replace"},
		},
		{
			name: "Reload",
			invoke: func(t *testing.T) {
				if err := client.Reload(ctx); err != nil {
					t.Fatalf("Reload: %v", err)
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

func TestOnCalendarFor(t *testing.T) {
	tests := []struct {
		freq    string
		want    string
		wantErr bool
	}{
		{freq: "hourly", want: "hourly"},
		{freq: "daily", want: "daily"},
		{freq: "weekly", want: "weekly"},
		{freq: "monthly", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.freq, func(t *testing.T) {
			got, err := OnCalendarFor(tc.freq)
			if tc.wantErr {
				if err == nil {
					t.Fatal("OnCalendarFor returned nil error")
				}
				return
			}
			if err != nil {
				t.Fatalf("OnCalendarFor: %v", err)
			}
			if got != tc.want {
				t.Fatalf("OnCalendarFor(%q) = %q, want %q", tc.freq, got, tc.want)
			}
		})
	}
}
