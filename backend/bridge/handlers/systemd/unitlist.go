package systemd

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
	"github.com/mordilloSan/LinuxIO/backend/bridge/utils"
)

type listedUnit struct {
	Name          string
	Description   string
	LoadState     string
	ActiveState   string
	SubState      string
	UnitFileState string
	Path          dbusclient.ObjectPath
}

type unitFileRecord struct {
	Path  string
	State string
}

func listUnitsBySuffix(
	session dbusclient.SystemSession,
	suffix string,
) ([]listedUnit, error) {
	var units [][]any
	if err := session.CallStore(dbusclient.SystemdManagerIface+".ListUnits", dbusclient.CallPolicy{}, nil, &units); err != nil {
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
		path, _ := rawUnit[6].(dbusclient.ObjectPath)

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
	if err := session.CallStore(dbusclient.SystemdManagerIface+".ListUnitFiles", dbusclient.CallPolicy{}, nil, &unitFiles); err != nil {
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

func getStringProperty(session dbusclient.SystemSession, unit dbusclient.BusObject, iface, property string) (string, bool) {
	str, err := dbusclient.GetProperty[string](session.Context(), unit, iface, property)
	if err != nil {
		return "", false
	}
	return str, true
}

func getUint64Property(session dbusclient.SystemSession, unit dbusclient.BusObject, iface, property string) (uint64, bool) {
	n, err := dbusclient.GetProperty[uint64](session.Context(), unit, iface, property)
	if err != nil {
		return 0, false
	}
	return n, true
}

func getUint32Property(session dbusclient.SystemSession, unit dbusclient.BusObject, iface, property string) (uint32, bool) {
	n, err := dbusclient.GetProperty[uint32](session.Context(), unit, iface, property)
	if err != nil {
		return 0, false
	}
	return n, true
}
