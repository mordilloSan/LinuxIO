package systemd

import (
	"context"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient/testdbus"
)

func TestForEachListedUnitLimitedCapsConcurrency(t *testing.T) {
	entries := make([]listedUnit, maxConcurrentUnitPropertyFetches*3)
	var active atomic.Int32
	var maxSeen atomic.Int32

	err := forEachListedUnitLimited(context.Background(), entries, func(_ int, _ listedUnit) {
		now := active.Add(1)
		for {
			seen := maxSeen.Load()
			if now <= seen || maxSeen.CompareAndSwap(seen, now) {
				break
			}
		}
		time.Sleep(2 * time.Millisecond)
		active.Add(-1)
	})
	if err != nil {
		t.Fatalf("forEachListedUnitLimited returned error: %v", err)
	}
	if got := maxSeen.Load(); got > int32(maxConcurrentUnitPropertyFetches) {
		t.Fatalf("max concurrency = %d, want <= %d", got, maxConcurrentUnitPropertyFetches)
	}
}

func TestForEachListedUnitLimitedHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := forEachListedUnitLimited(ctx, []listedUnit{{Name: "demo.service"}}, func(int, listedUnit) {
		t.Fatal("callback should not run for canceled context")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

type unitProperties map[string]godbus.Variant

func (p unitProperties) Get(iface, property string) (godbus.Variant, *godbus.Error) {
	value, ok := p[iface+"."+property]
	if !ok {
		return godbus.Variant{}, godbus.MakeFailedError(errors.New("unknown property"))
	}
	return value, nil
}

// TestFetchUnitPropertiesSetsEverySharedProperty fails when a UnitStatus field
// is neither reported by ListUnits nor read by fetchUnitProperties.
func TestFetchUnitPropertiesSetsEverySharedProperty(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	conn := bus.OwnName(t, dbusclient.SystemdBusName)
	const path = godbus.ObjectPath("/org/freedesktop/systemd1/unit/demo_2eservice")
	props := unitProperties{
		dbusclient.SystemdUnitIface + ".UnitFileState":          godbus.MakeVariant("enabled"),
		dbusclient.SystemdUnitIface + ".ActiveEnterTimestamp":   godbus.MakeVariant(uint64(1)),
		dbusclient.SystemdUnitIface + ".InactiveEnterTimestamp": godbus.MakeVariant(uint64(2)),
	}
	if err := conn.Export(props, path, dbusclient.PropertiesIface); err != nil {
		t.Fatalf("export unit: %v", err)
	}

	// The states ListUnits reports for a loaded unit.
	status := UnitStatus{
		Name:        "demo.service",
		Description: "Demo",
		LoadState:   "loaded",
		ActiveState: "active",
		SubState:    "running",
	}
	err := dbusclient.SystemdManager.UseSession(context.Background(), func(session dbusclient.SystemSession) error {
		status.fetchUnitProperties(session, session.ObjectAt(path))
		return nil
	})
	if err != nil {
		t.Fatalf("UseSession: %v", err)
	}

	for field, value := range reflect.ValueOf(status).Fields() {
		if value.IsZero() {
			t.Errorf("UnitStatus.%s is not set: read it in fetchUnitProperties and give the fake unit a value", field.Name)
		}
	}
	want := UnitStatus{
		Name:                   "demo.service",
		Description:            "Demo",
		LoadState:              "loaded",
		ActiveState:            "active",
		SubState:               "running",
		UnitFileState:          "enabled",
		ActiveEnterTimestamp:   1,
		InactiveEnterTimestamp: 2,
	}
	if status != want {
		t.Fatalf("status = %+v, want %+v", status, want)
	}
}
