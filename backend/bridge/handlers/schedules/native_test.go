package schedules

import (
	"encoding/hex"
	"sync/atomic"
	"testing"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient/testdbus"
)

type invocationService struct {
	snapshot  map[string]godbus.Variant
	wrongName bool
	expires   bool
	stops     atomic.Int32
	nameStops atomic.Int32
}

func (s *invocationService) GetAll(iface string) (map[string]godbus.Variant, *godbus.Error) {
	if iface != "" {
		return nil, godbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []any{"expected all unit interfaces"})
	}
	return s.snapshot, nil
}
func (s *invocationService) GetUnit(_ string) (godbus.ObjectPath, *godbus.Error) {
	return "/org/freedesktop/systemd1/unit/exact_invocation", nil
}
func (s *invocationService) GetUnitByInvocationID(_ []byte) (godbus.ObjectPath, *godbus.Error) {
	return "/org/freedesktop/systemd1/unit/exact_invocation", nil
}
func (s *invocationService) Get(_, property string) (godbus.Variant, *godbus.Error) {
	name := serviceName(testID)
	if s.wrongName {
		name = "another.service"
	}
	return godbus.MakeVariant(name), nil
}
func (s *invocationService) Stop(_ string) (godbus.ObjectPath, *godbus.Error) {
	if s.expires {
		return "", &godbus.Error{Name: "org.freedesktop.systemd1.NoSuchUnit", Body: []any{"invocation expired"}}
	}
	s.stops.Add(1)
	return "/org/freedesktop/systemd1/job/1", nil
}
func (s *invocationService) StopUnit(_, _ string) (godbus.ObjectPath, *godbus.Error) {
	s.nameStops.Add(1)
	return "/org/freedesktop/systemd1/job/2", nil
}

func TestStopUsesInvocationObjectWithoutNameFallback(t *testing.T) {
	for _, scenario := range []struct {
		name               string
		wrongName, expires bool
	}{{"current", false, false}, {"different unit", true, false}, {"run replaced before stop", false, true}} {
		t.Run(scenario.name, func(t *testing.T) {
			service := exportInvocation(t, scenario.wrongName, scenario.expires)
			err := stopInvocation(t.Context(), testID, testInvocation)
			if scenario.wrongName || scenario.expires {
				if err == nil || service.stops.Load() != 0 {
					t.Fatalf("unsafe stop: %v", err)
				}
			} else if err != nil || service.stops.Load() != 1 {
				t.Fatalf("exact stop failed: %v", err)
			}
			if service.nameStops.Load() != 0 {
				t.Fatal("fell back to unit-name cancellation")
			}
		})
	}
}

func exportInvocation(t *testing.T, wrongName, expires bool) *invocationService {
	t.Helper()
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	conn := bus.OwnName(t, dbusclient.SystemdBusName)
	service := &invocationService{wrongName: wrongName, expires: expires}
	id, _ := hex.DecodeString(testInvocation)
	service.snapshot = map[string]godbus.Variant{}
	for name, value := range map[string]any{
		"ActiveState": "inactive", "Job": []any{uint32(1), godbus.ObjectPath("/org/freedesktop/systemd1/job/1")},
		"InvocationID": id, "Result": "exit-code", "ExecMainCode": int32(1), "ExecMainStatus": int32(7), "ExecMainExitTimestamp": uint64(100),
	} {
		service.snapshot[name] = godbus.MakeVariant(value)
	}
	if err := conn.Export(service, godbus.ObjectPath(dbusclient.SystemdPath), dbusclient.SystemdManagerIface); err != nil {
		t.Fatal(err)
	}
	path := godbus.ObjectPath("/org/freedesktop/systemd1/unit/exact_invocation")
	if err := conn.Export(service, path, dbusclient.SystemdUnitIface); err != nil {
		t.Fatal(err)
	}
	if err := conn.Export(service, path, "org.freedesktop.DBus.Properties"); err != nil {
		t.Fatal(err)
	}
	return service
}

func TestScheduleRoutesRequirePrivilege(t *testing.T) {
	for _, route := range Routes {
		if !route.Privileged {
			t.Errorf("%s permits unprivileged scheduling", route.Route)
		}
	}
}

func TestNativeStateReadsOneConsistentSnapshot(t *testing.T) {
	exportInvocation(t, false, false)
	state, err := inspectUnit(t.Context(), serviceName(testID))
	if err != nil {
		t.Fatal(err)
	}
	if !active(state) || state.ActiveState != "inactive" || state.InvocationID != testInvocation || state.ExitStatus != 7 {
		t.Fatalf("lost queued run or native exit evidence: %+v", state)
	}
}
