package dbus

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	godbus "github.com/godbus/dbus/v5"
	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

const (
	systemdManagerBusName = "org.freedesktop.systemd1"
	systemdManagerPath    = godbus.ObjectPath("/org/freedesktop/systemd1")
)

type listedUnit struct {
	Name          string
	Description   string
	LoadState     string
	ActiveState   string
	SubState      string
	UnitFileState string
	Path          godbus.ObjectPath
}

type unitFileRecord struct {
	Path  string
	State string
}

func withSystemdManager(
	fn func(conn *godbus.Conn, systemd godbus.BusObject) error,
) error {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	return RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				logger.Warnf("failed to close D-Bus connection: %v", cerr)
			}
		}()

		return fn(conn, conn.Object(systemdManagerBusName, systemdManagerPath))
	})
}

func listUnitsBySuffix(
	systemd godbus.BusObject,
	suffix string,
) ([]listedUnit, error) {
	var units [][]any
	if err := systemd.Call("org.freedesktop.systemd1.Manager.ListUnits", 0).Store(&units); err != nil {
		return nil, err
	}

	loaded := make(map[string]listedUnit)
	for _, rawUnit := range units {
		name, err := utils.AsString(rawUnit[0])
		if err != nil {
			return nil, fmt.Errorf("invalid unit name: %w", err)
		}
		if !strings.HasSuffix(name, suffix) {
			continue
		}

		description, _ := utils.AsString(rawUnit[1])
		loadState, _ := utils.AsString(rawUnit[2])
		activeState, _ := utils.AsString(rawUnit[3])
		subState, _ := utils.AsString(rawUnit[4])
		path, _ := rawUnit[6].(godbus.ObjectPath)

		loaded[name] = listedUnit{
			Name:        name,
			Description: description,
			LoadState:   loadState,
			ActiveState: activeState,
			SubState:    subState,
			Path:        path,
		}
	}

	var unitFiles []unitFileRecord
	if err := systemd.Call("org.freedesktop.systemd1.Manager.ListUnitFiles", 0).Store(&unitFiles); err != nil {
		return nil, err
	}

	entries := make([]listedUnit, 0, len(loaded)+len(unitFiles))
	seen := make(map[string]struct{}, len(loaded))

	for name, unit := range loaded {
		entries = append(entries, unit)
		seen[name] = struct{}{}
	}

	for _, unitFile := range unitFiles {
		name := filepath.Base(unitFile.Path)
		if !strings.HasSuffix(name, suffix) {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}

		entries = append(entries, listedUnit{
			Name:          name,
			LoadState:     "not-loaded",
			ActiveState:   "inactive",
			SubState:      "dead",
			UnitFileState: unitFile.State,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}

func unitObject(conn *godbus.Conn, path godbus.ObjectPath) godbus.BusObject {
	return conn.Object(systemdManagerBusName, path)
}

func getStringProperty(unit godbus.BusObject, property string) (string, bool) {
	val, err := unit.GetProperty(property)
	if err != nil {
		return "", false
	}

	str, ok := val.Value().(string)
	return str, ok
}

func getUint64Property(unit godbus.BusObject, property string) (uint64, bool) {
	val, err := unit.GetProperty(property)
	if err != nil {
		return 0, false
	}

	n, ok := val.Value().(uint64)
	return n, ok
}

func getUint32Property(unit godbus.BusObject, property string) (uint32, bool) {
	val, err := unit.GetProperty(property)
	if err != nil {
		return 0, false
	}

	n, ok := val.Value().(uint32)
	return n, ok
}
