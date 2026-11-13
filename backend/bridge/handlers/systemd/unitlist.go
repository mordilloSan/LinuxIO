package systemd

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
)

const maxConcurrentUnitPropertyFetches = 16

// UnitStatus holds the properties the service, timer and socket lists share.
// ListUnits and ListUnitFiles report the states and fetchUnitProperties reads
// the rest. A field added here also needs a member in apischema.UnitSummary
// and a copy in unitSummaryToAPI; the tests in unitlist_test.go and
// contracts_test.go fail until it has them.
type UnitStatus struct {
	Name                   string `json:"name"`
	Description            string `json:"description"`
	LoadState              string `json:"load_state"`
	ActiveState            string `json:"active_state"`
	SubState               string `json:"sub_state"`
	UnitFileState          string `json:"unit_file_state"`
	ActiveEnterTimestamp   uint64 `json:"active_enter_timestamp"`
	InactiveEnterTimestamp uint64 `json:"inactive_enter_timestamp"`
}

// fetchUnitProperties reads the shared properties ListUnits does not report
// from the unit's D-Bus object, keeping the listed value of any it cannot read.
func (s *UnitStatus) fetchUnitProperties(session dbusclient.SystemSession, unit dbusclient.BusObject) {
	if state, ok := getStringProperty(session, unit, dbusclient.SystemdUnitIface, "UnitFileState"); ok {
		s.UnitFileState = state
	}
	if ts, ok := getUint64Property(session, unit, dbusclient.SystemdUnitIface, "ActiveEnterTimestamp"); ok {
		s.ActiveEnterTimestamp = ts
	}
	if ts, ok := getUint64Property(session, unit, dbusclient.SystemdUnitIface, "InactiveEnterTimestamp"); ok {
		s.InactiveEnterTimestamp = ts
	}
}

// listedUnit is a unit as ListUnits and ListUnitFiles report it, with the
// object path of a loaded unit.
type listedUnit struct {
	UnitStatus
	Path dbusclient.ObjectPath
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
		name, err := dbusclient.AsString(rawUnit[0])
		if err != nil {
			return nil, fmt.Errorf("invalid unit name: %w", err)
		}
		if !strings.HasSuffix(name, suffix) {
			continue
		}

		description, _ := dbusclient.AsString(rawUnit[1])
		loadState, _ := dbusclient.AsString(rawUnit[2])
		activeState, _ := dbusclient.AsString(rawUnit[3])
		subState, _ := dbusclient.AsString(rawUnit[4])
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
			UnitFileState: unitFile.State})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}

func forEachListedUnitLimited(ctx context.Context, entries []listedUnit, fn func(int, listedUnit)) error {
	if len(entries) == 0 {
		return ctx.Err()
	}

	limit := maxConcurrentUnitPropertyFetches
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}

	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i, entry := range entries {
		if err := ctx.Err(); err != nil {
			wg.Wait()
			return err
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		}

		wg.Add(1)
		go func(i int, entry listedUnit) {
			defer wg.Done()
			defer func() { <-sem }()
			fn(i, entry)
		}(i, entry)
	}

	wg.Wait()
	return ctx.Err()
}

func getStringProperty(session dbusclient.SystemSession, unit dbusclient.BusObject, iface, property string) (string, bool) {
	str, err := dbusclient.GetProperty[string](session, unit, iface, property)
	if err != nil {
		return "", false
	}
	return str, true
}

func getUint64Property(session dbusclient.SystemSession, unit dbusclient.BusObject, iface, property string) (uint64, bool) {
	n, err := dbusclient.GetProperty[uint64](session, unit, iface, property)
	if err != nil {
		return 0, false
	}
	return n, true
}

func getUint32Property(session dbusclient.SystemSession, unit dbusclient.BusObject, iface, property string) (uint32, bool) {
	n, err := dbusclient.GetProperty[uint32](session, unit, iface, property)
	if err != nil {
		return 0, false
	}
	return n, true
}
