package systemd

import (
	"context"
	"strings"
	"testing"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient/testdbus"
)

type unitInfoManager struct {
	unitFiles []unitFileRecord
	loadErr   *godbus.Error
}

func (m *unitInfoManager) GetUnit(name string) (godbus.ObjectPath, *godbus.Error) {
	return "", m.loadErr
}

func (m *unitInfoManager) LoadUnit(name string) (godbus.ObjectPath, *godbus.Error) {
	return "", m.loadErr
}

func (m *unitInfoManager) ListUnitFiles() ([]unitFileRecord, *godbus.Error) {
	return append([]unitFileRecord(nil), m.unitFiles...), nil
}

func exportUnitInfoManager(t *testing.T, bus *testdbus.Bus, manager *unitInfoManager) {
	t.Helper()

	conn := bus.OwnName(t, dbusclient.SystemdBusName)
	if err := conn.Export(manager, godbus.ObjectPath(dbusclient.SystemdPath), dbusclient.SystemdManagerIface); err != nil {
		t.Fatalf("export systemd manager: %v", err)
	}
}

func TestGetUnitInfoFallsBackToUnitFileRecord(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)

	const unitName = "apport-coredump-hook@.service"
	exportUnitInfoManager(t, bus, &unitInfoManager{
		unitFiles: []unitFileRecord{
			{
				Path:  "/usr/lib/systemd/system/" + unitName,
				State: "static",
			},
		},
		loadErr: &godbus.Error{
			Name: "org.freedesktop.systemd1.NoSuchUnit",
			Body: []any{"Unit name " + unitName + " is missing the instance name."},
		},
	})

	info, err := GetUnitInfo(context.Background(), unitName)
	if err != nil {
		t.Fatalf("GetUnitInfo: %v", err)
	}

	if info.ID == nil || *info.ID != unitName {
		t.Fatalf("Id = %#v, want %q", info.ID, unitName)
	}
	if info.LoadState == nil || *info.LoadState != "not-loaded" {
		t.Fatalf("LoadState = %#v, want %q", info.LoadState, "not-loaded")
	}
	if info.ActiveState == nil || *info.ActiveState != "inactive" {
		t.Fatalf("ActiveState = %#v, want %q", info.ActiveState, "inactive")
	}
	if info.SubState == nil || *info.SubState != "dead" {
		t.Fatalf("SubState = %#v, want %q", info.SubState, "dead")
	}
	if info.UnitFileState == nil || *info.UnitFileState != "static" {
		t.Fatalf("UnitFileState = %#v, want %q", info.UnitFileState, "static")
	}
	if info.FragmentPath == nil || *info.FragmentPath != "/usr/lib/systemd/system/"+unitName {
		t.Fatalf("FragmentPath = %#v, want %q", info.FragmentPath, "/usr/lib/systemd/system/"+unitName)
	}
}

func TestUnitInfoFromMapConvertsDBusNumericValues(t *testing.T) {
	info := unitInfoFromMap(map[string]any{
		"ActiveEnterTimestamp": uint64(1_700_000_000_000_000),
		"ExecMainStatus":       int32(-1),
		"MainPID":              uint32(1234),
		"MemoryCurrent":        uint64(8 * 1024 * 1024),
		"NConnections":         uint32(2),
		"NextElapseUSec":       uint64(1_700_000_001_000_000),
		"Requires":             []string{"network.target"},
	})

	if info.ActiveEnterTimestamp == nil || *info.ActiveEnterTimestamp != 1_700_000_000_000_000 {
		t.Fatalf("ActiveEnterTimestamp = %#v", info.ActiveEnterTimestamp)
	}
	if info.ExecMainStatus == nil || *info.ExecMainStatus != -1 {
		t.Fatalf("ExecMainStatus = %#v, want -1", info.ExecMainStatus)
	}
	if info.MainPID == nil || *info.MainPID != 1234 {
		t.Fatalf("MainPID = %#v, want 1234", info.MainPID)
	}
	if info.MemoryCurrent == nil || *info.MemoryCurrent != 8*1024*1024 {
		t.Fatalf("MemoryCurrent = %#v", info.MemoryCurrent)
	}
	if info.NConnections == nil || *info.NConnections != 2 {
		t.Fatalf("NConnections = %#v, want 2", info.NConnections)
	}
	if info.NextElapseUSec == nil || *info.NextElapseUSec != 1_700_000_001_000_000 {
		t.Fatalf("NextElapseUSec = %#v", info.NextElapseUSec)
	}
	if len(info.Requires) != 1 || info.Requires[0] != "network.target" {
		t.Fatalf("Requires = %#v", info.Requires)
	}
}

func TestGetUnitInfoReturnsOriginalLoadErrorWhenUnitFileMissing(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)

	const unitName = "apport-coredump-hook@.service"
	exportUnitInfoManager(t, bus, &unitInfoManager{
		loadErr: &godbus.Error{
			Name: "org.freedesktop.systemd1.NoSuchUnit",
			Body: []any{"Unit name " + unitName + " is missing the instance name."},
		},
	})

	_, err := GetUnitInfo(context.Background(), unitName)
	if err == nil {
		t.Fatal("GetUnitInfo returned nil error")
	}
	if !strings.Contains(err.Error(), "missing the instance name") {
		t.Fatalf("error = %q, want original load error", err)
	}
}
