package control

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"testing"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient/testdbus"
)

type login1ManagerStub struct {
	mu         sync.Mutex
	terminated []string
	actions    []string
}

func (m *login1ManagerStub) TerminateSession(sessionID string) *godbus.Error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.terminated = append(m.terminated, sessionID)
	return nil
}

func (m *login1ManagerStub) Reboot(interactive bool) *godbus.Error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actions = append(m.actions, actionRecord("Reboot", interactive))
	return nil
}

func (m *login1ManagerStub) PowerOff(interactive bool) *godbus.Error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.actions = append(m.actions, actionRecord("PowerOff", interactive))
	return nil
}

func (m *login1ManagerStub) terminatedSessions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.terminated...)
}

func (m *login1ManagerStub) calledActions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.actions...)
}

func actionRecord(action string, interactive bool) string {
	return action + ":interactive=" + fmt.Sprint(interactive)
}

func exportLogin1Manager(t *testing.T, bus *testdbus.Bus, manager *login1ManagerStub) {
	t.Helper()

	conn := bus.OwnName(t, dbusclient.LoginBusName)
	if err := conn.Export(manager, godbus.ObjectPath(dbusclient.LoginPath), dbusclient.LoginMgrIface); err != nil {
		t.Fatalf("export login1 manager: %v", err)
	}
}

func TestLogoffCallsManager(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	manager := &login1ManagerStub{}
	exportLogin1Manager(t, bus, manager)

	if err := Logoff(context.Background(), "12"); err != nil {
		t.Fatalf("Logoff: %v", err)
	}

	if got, want := manager.terminatedSessions(), []string{"12"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("terminated sessions = %#v, want %#v", got, want)
	}
}

func TestRebootAndPowerOffCallManagerWithNonInteractiveFlag(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	manager := &login1ManagerStub{}
	exportLogin1Manager(t, bus, manager)

	if err := Reboot(context.Background()); err != nil {
		t.Fatalf("Reboot: %v", err)
	}
	if err := PowerOff(context.Background()); err != nil {
		t.Fatalf("PowerOff: %v", err)
	}

	want := []string{
		actionRecord("Reboot", false),
		actionRecord("PowerOff", false),
	}

	if got := manager.calledActions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %#v, want %#v", got, want)
	}
}
