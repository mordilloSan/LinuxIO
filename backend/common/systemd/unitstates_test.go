package systemd

import (
	"reflect"
	"testing"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient/testdbus"
)

// loadManager serves GetUnit for the loaded units and LoadUnit for those a
// file defines or not, refusing names in invalid as the manager refuses a
// name that is no valid unit name.
type loadManager struct {
	loaded, files map[string]godbus.ObjectPath
	invalid       map[string]bool
}

func (m loadManager) GetUnit(name string) (godbus.ObjectPath, *godbus.Error) {
	if path, ok := m.loaded[name]; ok {
		return path, nil
	}
	return "", &godbus.Error{Name: "org.freedesktop.systemd1.NoSuchUnit", Body: []any{"not loaded"}}
}

func (m loadManager) LoadUnit(name string) (godbus.ObjectPath, *godbus.Error) {
	if m.invalid[name] {
		return "", &godbus.Error{Name: "org.freedesktop.DBus.Error.InvalidArgs", Body: []any{"Unit name " + name + " is not valid."}}
	}
	if path, ok := m.files[name]; ok {
		return path, nil
	}
	return "", &godbus.Error{Name: "org.freedesktop.DBus.Error.NoReply", Body: []any{"no answer"}}
}

// GetUnitStates reads a loaded unit and loads one the manager collected; a
// name the manager refuses is left out and the others are still read, and
// any other failure to load is an error.
func TestGetUnitStates(t *testing.T) {
	bus := testdbus.Start(t)
	bus.SetSystemBus(t)
	conn := bus.OwnName(t, dbusclient.SystemdBusName)
	long := "very-long.mount"
	manager := loadManager{
		loaded:  map[string]godbus.ObjectPath{"data.mount": "/org/freedesktop/systemd1/unit/data_2emount"},
		files:   map[string]godbus.ObjectPath{"other.mount": "/org/freedesktop/systemd1/unit/other_2emount"},
		invalid: map[string]bool{long: true},
	}
	if err := conn.Export(manager, godbus.ObjectPath(dbusclient.SystemdPath), dbusclient.SystemdManagerIface); err != nil {
		t.Fatalf("export manager: %v", err)
	}
	units := map[godbus.ObjectPath]UnitState{
		"/org/freedesktop/systemd1/unit/data_2emount":  {LoadState: "loaded", ActiveState: "inactive", SourcePath: "/etc/fstab"},
		"/org/freedesktop/systemd1/unit/other_2emount": {LoadState: "not-found", ActiveState: "inactive"},
	}
	for path, u := range units {
		props := unitProperties{
			dbusclient.SystemdUnitIface + ".LoadState":   godbus.MakeVariant(u.LoadState),
			dbusclient.SystemdUnitIface + ".ActiveState": godbus.MakeVariant(u.ActiveState),
			dbusclient.SystemdUnitIface + ".SourcePath":  godbus.MakeVariant(u.SourcePath),
		}
		if err := conn.Export(props, path, dbusclient.PropertiesIface); err != nil {
			t.Fatalf("export unit %s: %v", path, err)
		}
	}

	got, err := GetUnitStates(t.Context(), []string{long, "data.mount", "other.mount"})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]UnitState{
		"data.mount":  units["/org/freedesktop/systemd1/unit/data_2emount"],
		"other.mount": units["/org/freedesktop/systemd1/unit/other_2emount"],
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetUnitStates = %+v, want %+v", got, want)
	}
	if _, err := GetUnitStates(t.Context(), []string{"data.mount", "silent.mount"}); err == nil {
		t.Fatal("a unit the manager did not answer for was not an error")
	}
}
