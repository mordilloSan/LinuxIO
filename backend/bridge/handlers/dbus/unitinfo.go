package dbus

import (
	"fmt"
	"path/filepath"
	"strings"

	godbus "github.com/godbus/dbus/v5"
)

var commonUnitInfoProps = []string{
	"Id", "Description", "LoadState", "ActiveState", "SubState",
	"UnitFileState", "FragmentPath", "ActiveEnterTimestamp", "InactiveEnterTimestamp",
	"Requires", "Wants", "WantedBy", "Before", "After",
	"Conflicts", "PartOf", "TriggeredBy",
}

func GetUnitInfo(unitName string) (map[string]any, error) {
	unitName = strings.TrimSpace(unitName)
	if unitName == "" {
		return nil, fmt.Errorf("missing unit name")
	}

	var info map[string]any
	err := withSystemdManager(func(conn *godbus.Conn, systemd godbus.BusObject) error {
		info = make(map[string]any)
		unitPath, err := getUnitObjectPath(systemd, unitName)
		if err != nil {
			return populateUnitFileInfo(systemd, unitName, info, err)
		}

		unit := unitObject(conn, unitPath)

		for _, prop := range commonUnitInfoProps {
			val, err := unit.GetProperty("org.freedesktop.systemd1.Unit." + prop)
			if err == nil {
				info[prop] = val.Value()
			}
		}

		switch {
		case strings.HasSuffix(unitName, ".service"):
			enrichServiceUnitInfo(unit, info)
		case strings.HasSuffix(unitName, ".timer"):
			enrichTimerUnitInfo(unit, info)
		case strings.HasSuffix(unitName, ".socket"):
			enrichSocketUnitInfo(unit, info)
		}

		return nil
	})

	return info, err
}

func populateUnitFileInfo(
	systemd godbus.BusObject,
	unitName string,
	info map[string]any,
	loadErr error,
) error {
	record, found, err := findUnitFileRecord(systemd, unitName)
	if err != nil {
		return err
	}
	if !found {
		return loadErr
	}

	info["Id"] = unitName
	info["LoadState"] = "not-loaded"
	info["ActiveState"] = "inactive"
	info["SubState"] = "dead"
	info["UnitFileState"] = record.State
	info["FragmentPath"] = record.Path

	return nil
}

func findUnitFileRecord(
	systemd godbus.BusObject,
	unitName string,
) (unitFileRecord, bool, error) {
	var unitFiles []unitFileRecord
	if err := systemd.Call("org.freedesktop.systemd1.Manager.ListUnitFiles", 0).Store(&unitFiles); err != nil {
		return unitFileRecord{}, false, err
	}

	for _, unitFile := range unitFiles {
		if filepath.Base(unitFile.Path) == unitName {
			return unitFile, true, nil
		}
	}

	return unitFileRecord{}, false, nil
}

func getUnitObjectPath(
	systemd godbus.BusObject,
	unitName string,
) (godbus.ObjectPath, error) {
	var unitPath godbus.ObjectPath
	if err := systemd.Call("org.freedesktop.systemd1.Manager.GetUnit", 0, unitName).Store(&unitPath); err == nil {
		return unitPath, nil
	}

	if err := systemd.Call("org.freedesktop.systemd1.Manager.LoadUnit", 0, unitName).Store(&unitPath); err != nil {
		return "", err
	}

	return unitPath, nil
}

func enrichServiceUnitInfo(unit godbus.BusObject, info map[string]any) {
	if val, err := unit.GetProperty("org.freedesktop.systemd1.Service.MainPID"); err == nil {
		info["MainPID"] = val.Value()
	}
	if val, err := unit.GetProperty("org.freedesktop.systemd1.Service.MemoryCurrent"); err == nil {
		info["MemoryCurrent"] = val.Value()
	} else if val, err := unit.GetProperty("org.freedesktop.systemd1.Unit.MemoryCurrent"); err == nil {
		info["MemoryCurrent"] = val.Value()
	}
	if val, err := unit.GetProperty("org.freedesktop.systemd1.Service.ExecMainStatus"); err == nil {
		info["ExecMainStatus"] = val.Value()
	}
}

func enrichTimerUnitInfo(unit godbus.BusObject, info map[string]any) {
	if val, err := unit.GetProperty("org.freedesktop.systemd1.Timer.NextElapseUSecRealtime"); err == nil {
		if v, ok := val.Value().(uint64); ok && v > 0 {
			info["NextElapseUSec"] = v
		}
	}
	if _, ok := info["NextElapseUSec"]; !ok {
		if val, err := unit.GetProperty("org.freedesktop.systemd1.Timer.NextElapseUSecMonotonic"); err == nil {
			if v, ok := val.Value().(uint64); ok {
				info["NextElapseUSec"] = v
			}
		}
	}
	if val, err := unit.GetProperty("org.freedesktop.systemd1.Timer.LastTriggerUSec"); err == nil {
		info["LastTriggerUSec"] = val.Value()
	}
	if val, err := unit.GetProperty("org.freedesktop.systemd1.Timer.Unit"); err == nil {
		info["Unit"] = val.Value()
	}
}

func enrichSocketUnitInfo(unit godbus.BusObject, info map[string]any) {
	if val, err := unit.GetProperty("org.freedesktop.systemd1.Socket.Listen"); err == nil {
		if listen := parseSocketListen(val.Value()); len(listen) > 0 {
			info["Listen"] = listen
		}
	}
	if val, err := unit.GetProperty("org.freedesktop.systemd1.Socket.NConnections"); err == nil {
		info["NConnections"] = val.Value()
	}
	if val, err := unit.GetProperty("org.freedesktop.systemd1.Socket.NAccepted"); err == nil {
		info["NAccepted"] = val.Value()
	}
}

func parseSocketListen(value any) []string {
	pairs, ok := value.([][]interface{})
	if !ok {
		return nil
	}

	listen := make([]string, 0, len(pairs))
	for _, pair := range pairs {
		if len(pair) < 2 {
			continue
		}
		addr, ok := pair[1].(string)
		if ok && addr != "" {
			listen = append(listen, addr)
		}
	}

	return listen
}
