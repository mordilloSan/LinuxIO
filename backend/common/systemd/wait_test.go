package systemd

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient/testdbus"
)

const (
	waitUnitPath = godbus.ObjectPath("/org/freedesktop/systemd1/unit/demo_2eservice")
	waitJobPath  = godbus.ObjectPath("/org/freedesktop/systemd1/job/7")
)

// jobManager queues one job for every start or stop and resolves the unit.
type jobManager struct {
	mu    sync.Mutex
	calls []string
}

func (m *jobManager) queue(method string) (godbus.ObjectPath, *godbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, method)
	return waitJobPath, nil
}

func (m *jobManager) StartUnit(string, string) (godbus.ObjectPath, *godbus.Error) {
	return m.queue("StartUnit")
}

func (m *jobManager) StopUnit(string, string) (godbus.ObjectPath, *godbus.Error) {
	return m.queue("StopUnit")
}

func (m *jobManager) GetUnit(string) (godbus.ObjectPath, *godbus.Error) {
	return waitUnitPath, nil
}

// jobUnit is a unit that carries job for its first pendingReads reads of Job
// and then has none, or another job when replacedBy is set. ActiveState reads
// "activating" while the job is on it and final after.
type jobUnit struct {
	mu           sync.Mutex
	pendingReads int
	replacedBy   godbus.ObjectPath
	final        string
}

type unitJob struct {
	ID   uint32
	Path godbus.ObjectPath
}

func (u *jobUnit) Get(_, property string) (godbus.Variant, *godbus.Error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	switch property {
	case "Job":
		if u.pendingReads > 0 {
			u.pendingReads--
			return godbus.MakeVariant(unitJob{ID: 7, Path: waitJobPath}), nil
		}
		if u.replacedBy != "" {
			return godbus.MakeVariant(unitJob{ID: 8, Path: u.replacedBy}), nil
		}
		return godbus.MakeVariant(unitJob{Path: "/"}), nil
	case "ActiveState":
		if u.pendingReads > 0 {
			return godbus.MakeVariant("activating"), nil
		}
		return godbus.MakeVariant(u.final), nil
	}
	return godbus.Variant{}, godbus.MakeFailedError(errors.New("unknown property"))
}

func exportJobUnit(t *testing.T, unit *jobUnit) *jobManager {
	t.Helper()
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	conn := bus.OwnName(t, dbusclient.SystemdBusName)
	manager := &jobManager{}
	if err := conn.Export(manager, godbus.ObjectPath(dbusclient.SystemdPath), dbusclient.SystemdManagerIface); err != nil {
		t.Fatalf("export manager: %v", err)
	}
	if err := conn.Export(unit, waitUnitPath, dbusclient.PropertiesIface); err != nil {
		t.Fatalf("export unit: %v", err)
	}
	return manager
}

// A start returns only once the unit has dropped the job, and is judged by
// the state the unit is then in: active is success, anything else names it.
func TestStartUnitAndWait(t *testing.T) {
	for _, tc := range []struct {
		name    string
		unit    *jobUnit
		wantErr string
	}{
		{name: "active after the job", unit: &jobUnit{pendingReads: 2, final: "active"}},
		{name: "already over at the first look", unit: &jobUnit{final: "active"}},
		{name: "the command failed", unit: &jobUnit{pendingReads: 1, final: "failed"}, wantErr: "start unit demo.service: the unit is failed"},
		{name: "a stop replaced the start", unit: &jobUnit{replacedBy: "/org/freedesktop/systemd1/job/8", final: "deactivating"}, wantErr: "the unit is deactivating"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := exportJobUnit(t, tc.unit)
			err := StartUnitAndWait(t.Context(), "demo.service")
			if tc.wantErr == "" && err != nil {
				t.Fatalf("StartUnitAndWait: %v", err)
			}
			if tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("StartUnitAndWait error = %v, want %q", err, tc.wantErr)
			}
			if len(manager.calls) != 1 || manager.calls[0] != "StartUnit" {
				t.Fatalf("manager calls = %v, want one StartUnit", manager.calls)
			}
			if tc.unit.pendingReads != 0 {
				t.Fatalf("returned with the job still on the unit for %d more reads", tc.unit.pendingReads)
			}
		})
	}
}

// A stop is over when the unit is inactive; a unit left failed by its stop
// command is an error.
func TestStopUnitAndWait(t *testing.T) {
	manager := exportJobUnit(t, &jobUnit{pendingReads: 1, final: "inactive"})
	if err := StopUnitAndWait(t.Context(), "demo.service"); err != nil {
		t.Fatalf("StopUnitAndWait: %v", err)
	}
	if len(manager.calls) != 1 || manager.calls[0] != "StopUnit" {
		t.Fatalf("manager calls = %v, want one StopUnit", manager.calls)
	}

	exportJobUnit(t, &jobUnit{final: "failed"})
	err := StopUnitAndWait(t.Context(), "demo.service")
	if err == nil || !strings.Contains(err.Error(), "stop unit demo.service: the unit is failed") {
		t.Fatalf("StopUnitAndWait error = %v, want the failed state", err)
	}
}

// A wait ends with the context's error while the job is still on the unit.
func TestStartUnitAndWaitEndsWithItsContext(t *testing.T) {
	exportJobUnit(t, &jobUnit{pendingReads: 1 << 20, final: "active"})
	ctx, cancel := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancel()

	err := StartUnitAndWait(ctx, "demo.service")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("StartUnitAndWait error = %v, want context.DeadlineExceeded", err)
	}
}
