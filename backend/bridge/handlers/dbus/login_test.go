package dbus

import (
	"context"
	"reflect"
	"sync"
	"testing"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/testdbus"
)

type login1ManagerStub struct {
	mu         sync.Mutex
	terminated []string
}

func (m *login1ManagerStub) TerminateSession(sessionID string) *godbus.Error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.terminated = append(m.terminated, sessionID)
	return nil
}

func (m *login1ManagerStub) terminatedSessions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.terminated...)
}

func exportLogin1Manager(t *testing.T, bus *testdbus.Bus, manager *login1ManagerStub) {
	t.Helper()

	conn := bus.OwnName(t, login1BusName)
	if err := conn.Export(manager, login1ObjectPath, login1ManagerIface); err != nil {
		t.Fatalf("export login1 manager: %v", err)
	}
}

func TestTerminateLogin1SessionCallsManager(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	manager := &login1ManagerStub{}
	exportLogin1Manager(t, bus, manager)

	if err := TerminateLogin1Session(context.Background(), "12"); err != nil {
		t.Fatalf("TerminateLogin1Session: %v", err)
	}

	if got, want := manager.terminatedSessions(), []string{"12"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("terminated sessions = %#v, want %#v", got, want)
	}
}
