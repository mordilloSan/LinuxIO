package systemd

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	godbus "github.com/godbus/dbus/v5"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

var commonUnitInfoProps = []string{
	"Id", "Description", "LoadState", "ActiveState", "SubState",
	"UnitFileState", "FragmentPath", "ActiveEnterTimestamp", "InactiveEnterTimestamp",
	"Requires", "Wants", "WantedBy", "Before", "After",
	"Conflicts", "PartOf", "TriggeredBy",
}

func GetUnitInfo(ctx context.Context, unitName string) (map[string]any, error) {
	unitName = strings.TrimSpace(unitName)
	if unitName == "" {
		return nil, fmt.Errorf("missing unit name")
	}

	var info map[string]any
	err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		info = make(map[string]any)
		unitPath, err := getUnitObjectPath(session, unitName)
		if err != nil {
			return populateUnitFileInfo(session, unitName, info, err)
		}

		unit := session.ObjectAt(unitPath)

		for _, prop := range commonUnitInfoProps {
			val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdUnitIface, prop)
			if err == nil {
				info[prop] = val.Value()
			}
		}

		switch {
		case strings.HasSuffix(unitName, ".service"):
			enrichServiceUnitInfo(session, unit, info)
		case strings.HasSuffix(unitName, ".timer"):
			enrichTimerUnitInfo(session, unit, info)
		case strings.HasSuffix(unitName, ".socket"):
			enrichSocketUnitInfo(session, unit, info)
		}

		return nil
	})

	return info, err
}

func populateUnitFileInfo(
	session dbusclient.SystemSession,
	unitName string,
	info map[string]any,
	loadErr error,
) error {
	record, found, err := findUnitFileRecord(session, unitName)
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
	session dbusclient.SystemSession,
	unitName string,
) (unitFileRecord, bool, error) {
	var unitFiles []unitFileRecord
	if err := session.CallStore(dbusclient.SystemdManagerIface+".ListUnitFiles", dbusclient.CallPolicy{}, nil, &unitFiles); err != nil {
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
	session dbusclient.SystemSession,
	unitName string,
) (godbus.ObjectPath, error) {
	var unitPath godbus.ObjectPath
	if err := session.CallStore(dbusclient.SystemdManagerIface+".GetUnit", dbusclient.CallPolicy{}, []any{unitName}, &unitPath); err == nil {
		return unitPath, nil
	}

	if err := session.CallStore(dbusclient.SystemdManagerIface+".LoadUnit", dbusclient.CallPolicy{}, []any{unitName}, &unitPath); err != nil {
		return "", err
	}

	return unitPath, nil
}

func enrichServiceUnitInfo(session dbusclient.SystemSession, unit godbus.BusObject, info map[string]any) {
	if val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdServiceIface, "MainPID"); err == nil {
		info["MainPID"] = val.Value()
	}
	if val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdServiceIface, "MemoryCurrent"); err == nil {
		info["MemoryCurrent"] = val.Value()
	} else if val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdUnitIface, "MemoryCurrent"); err == nil {
		info["MemoryCurrent"] = val.Value()
	}
	if val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdServiceIface, "ExecMainStatus"); err == nil {
		info["ExecMainStatus"] = val.Value()
	}
}

func enrichTimerUnitInfo(session dbusclient.SystemSession, unit godbus.BusObject, info map[string]any) {
	if val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdTimerIface, "NextElapseUSecRealtime"); err == nil {
		if v, ok := val.Value().(uint64); ok && v > 0 {
			info["NextElapseUSec"] = v
		}
	}
	if _, ok := info["NextElapseUSec"]; !ok {
		if val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdTimerIface, "NextElapseUSecMonotonic"); err == nil {
			if v, ok := val.Value().(uint64); ok {
				info["NextElapseUSec"] = v
			}
		}
	}
	if val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdTimerIface, "LastTriggerUSec"); err == nil {
		info["LastTriggerUSec"] = val.Value()
	}
	if val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdTimerIface, "Unit"); err == nil {
		info["Unit"] = val.Value()
	}
}

func enrichSocketUnitInfo(session dbusclient.SystemSession, unit godbus.BusObject, info map[string]any) {
	if val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdSocketIface, "Listen"); err == nil {
		if listen := parseSocketListen(val.Value()); len(listen) > 0 {
			info["Listen"] = listen
		}
	}
	if val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdSocketIface, "NConnections"); err == nil {
		info["NConnections"] = val.Value()
	}
	if val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdSocketIface, "NAccepted"); err == nil {
		info["NAccepted"] = val.Value()
	}
}

func parseSocketListen(value any) []string {
	pairs, ok := value.([][]any)
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
