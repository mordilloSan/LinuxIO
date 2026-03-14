package dbus

import (
	"strings"
	"testing"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/testdbus"
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

	conn := bus.OwnName(t, systemdManagerBusName)
	if err := conn.Export(manager, systemdManagerPath, systemdManagerBusName+".Manager"); err != nil {
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

	info, err := GetUnitInfo(unitName)
	if err != nil {
		t.Fatalf("GetUnitInfo: %v", err)
	}

	if got := info["Id"]; got != unitName {
		t.Fatalf("Id = %#v, want %q", got, unitName)
	}
	if got := info["LoadState"]; got != "not-loaded" {
		t.Fatalf("LoadState = %#v, want %q", got, "not-loaded")
	}
	if got := info["ActiveState"]; got != "inactive" {
		t.Fatalf("ActiveState = %#v, want %q", got, "inactive")
	}
	if got := info["SubState"]; got != "dead" {
		t.Fatalf("SubState = %#v, want %q", got, "dead")
	}
	if got := info["UnitFileState"]; got != "static" {
		t.Fatalf("UnitFileState = %#v, want %q", got, "static")
	}
	if got := info["FragmentPath"]; got != "/usr/lib/systemd/system/"+unitName {
		t.Fatalf("FragmentPath = %#v, want %q", got, "/usr/lib/systemd/system/"+unitName)
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

	_, err := GetUnitInfo(unitName)
	if err == nil {
		t.Fatal("GetUnitInfo returned nil error")
	}
	if !strings.Contains(err.Error(), "missing the instance name") {
		t.Fatalf("error = %q, want original load error", err)
	}
}
