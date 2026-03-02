package dbus

import (
	"path/filepath"
	"sort"
	"strings"
	"sync"

	godbus "github.com/godbus/dbus/v5"
	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

type TimerStatus struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	LoadState       string `json:"load_state"`
	ActiveState     string `json:"active_state"`
	SubState        string `json:"sub_state"`
	UnitFileState   string `json:"unit_file_state"`
	NextElapseUSec  uint64 `json:"next_elapse_usec"`
	LastTriggerUSec uint64 `json:"last_trigger_usec"`
	Unit            string `json:"unit"`
}

func ListTimers() ([]TimerStatus, error) {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	var timers []TimerStatus
	err := RetryOnceIfClosed(nil, func() error {
		conn, err := godbus.SystemBus()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := conn.Close(); cerr != nil {
				logger.Warnf("failed to close D-Bus connection: %v", cerr)
			}
		}()

		systemd := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")

		// --- Step 1: loaded units (runtime info) ---
		var units [][]any
		if err := systemd.Call("org.freedesktop.systemd1.Manager.ListUnits", 0).Store(&units); err != nil {
			return err
		}

		type unitEntry struct {
			timer TimerStatus
			path  godbus.ObjectPath
		}

		// Index loaded timers by name
		loaded := make(map[string]unitEntry)
		for _, u := range units {
			name, _ := utils.AsString(u[0])
			if !strings.HasSuffix(name, ".timer") {
				continue
			}
			desc, _ := utils.AsString(u[1])
			loadState, _ := utils.AsString(u[2])
			activeState, _ := utils.AsString(u[3])
			subState, _ := utils.AsString(u[4])
			unitPath, _ := u[6].(godbus.ObjectPath)
			loaded[name] = unitEntry{
				timer: TimerStatus{
					Name:        name,
					Description: desc,
					LoadState:   loadState,
					ActiveState: activeState,
					SubState:    subState,
				},
				path: unitPath,
			}
		}

		// --- Step 2: all unit files (catches unloaded/inactive timers) ---
		type unitFileRecord struct {
			Path  string
			State string
		}
		var unitFiles []unitFileRecord
		if err := systemd.Call("org.freedesktop.systemd1.Manager.ListUnitFiles", 0).Store(&unitFiles); err != nil {
			return err
		}

		// Build final entry list: loaded first, then unloaded
		var entries []unitEntry
		seen := make(map[string]bool)

		for name, e := range loaded {
			entries = append(entries, e)
			seen[name] = true
		}
		for _, uf := range unitFiles {
			name := filepath.Base(uf.Path)
			if !strings.HasSuffix(name, ".timer") || seen[name] {
				continue
			}
			entries = append(entries, unitEntry{
				timer: TimerStatus{
					Name:          name,
					LoadState:     "not-loaded",
					ActiveState:   "inactive",
					SubState:      "dead",
					UnitFileState: uf.State,
				},
			})
			seen[name] = true
		}

		// --- Step 3: fetch extra properties for loaded timers in parallel ---
		results := make([]TimerStatus, len(entries))
		var wg sync.WaitGroup
		for i, e := range entries {
			wg.Add(1)
			go func(i int, e unitEntry) {
				defer wg.Done()
				t := e.timer
				if e.path != "" {
					unit := conn.Object("org.freedesktop.systemd1", e.path)
					if val, err := unit.GetProperty("org.freedesktop.systemd1.Unit.UnitFileState"); err == nil {
						if s, ok := val.Value().(string); ok {
							t.UnitFileState = s
						}
					}
					// Prefer realtime clock; fall back to monotonic
					if val, err := unit.GetProperty("org.freedesktop.systemd1.Timer.NextElapseUSecRealtime"); err == nil {
						if v, ok := val.Value().(uint64); ok && v > 0 {
							t.NextElapseUSec = v
						}
					}
					if t.NextElapseUSec == 0 {
						if val, err := unit.GetProperty("org.freedesktop.systemd1.Timer.NextElapseUSecMonotonic"); err == nil {
							if v, ok := val.Value().(uint64); ok {
								t.NextElapseUSec = v
							}
						}
					}
					if val, err := unit.GetProperty("org.freedesktop.systemd1.Timer.LastTriggerUSec"); err == nil {
						if v, ok := val.Value().(uint64); ok {
							t.LastTriggerUSec = v
						}
					}
					if val, err := unit.GetProperty("org.freedesktop.systemd1.Timer.Unit"); err == nil {
						if s, ok := val.Value().(string); ok {
							t.Unit = s
						}
					}
				}
				results[i] = t
			}(i, e)
		}
		wg.Wait()
		sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
		timers = results
		return nil
	})
	return timers, err
}
