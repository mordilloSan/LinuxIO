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

type SocketStatus struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	LoadState     string   `json:"load_state"`
	ActiveState   string   `json:"active_state"`
	SubState      string   `json:"sub_state"`
	UnitFileState string   `json:"unit_file_state"`
	Listen        []string `json:"listen"`
	NConnections  uint32   `json:"n_connections"`
	NAccepted     uint32   `json:"n_accepted"`
}

func ListSockets() ([]SocketStatus, error) {
	systemDBusMu.Lock()
	defer systemDBusMu.Unlock()

	var sockets []SocketStatus
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
			socket SocketStatus
			path   godbus.ObjectPath
		}

		// Index loaded sockets by name
		loaded := make(map[string]unitEntry)
		for _, u := range units {
			name, _ := utils.AsString(u[0])
			if !strings.HasSuffix(name, ".socket") {
				continue
			}
			desc, _ := utils.AsString(u[1])
			loadState, _ := utils.AsString(u[2])
			activeState, _ := utils.AsString(u[3])
			subState, _ := utils.AsString(u[4])
			unitPath, _ := u[6].(godbus.ObjectPath)
			loaded[name] = unitEntry{
				socket: SocketStatus{
					Name:        name,
					Description: desc,
					LoadState:   loadState,
					ActiveState: activeState,
					SubState:    subState,
					Listen:      []string{},
				},
				path: unitPath,
			}
		}

		// --- Step 2: all unit files (catches unloaded/inactive sockets) ---
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
			if !strings.HasSuffix(name, ".socket") || seen[name] {
				continue
			}
			entries = append(entries, unitEntry{
				socket: SocketStatus{
					Name:          name,
					LoadState:     "not-loaded",
					ActiveState:   "inactive",
					SubState:      "dead",
					UnitFileState: uf.State,
					Listen:        []string{},
				},
			})
			seen[name] = true
		}

		// --- Step 3: fetch extra properties for loaded sockets in parallel ---
		results := make([]SocketStatus, len(entries))
		var wg sync.WaitGroup
		for i, e := range entries {
			wg.Add(1)
			go func(i int, e unitEntry) {
				defer wg.Done()
				s := e.socket
				if e.path != "" {
					unit := conn.Object("org.freedesktop.systemd1", e.path)
					if val, err := unit.GetProperty("org.freedesktop.systemd1.Unit.UnitFileState"); err == nil {
						if str, ok := val.Value().(string); ok {
							s.UnitFileState = str
						}
					}
					// Listen is a(ss): array of (type, address) pairs
					if val, err := unit.GetProperty("org.freedesktop.systemd1.Socket.Listen"); err == nil {
						if pairs, ok := val.Value().([][]interface{}); ok {
							for _, pair := range pairs {
								if len(pair) >= 2 {
									if addr, ok := pair[1].(string); ok && addr != "" {
										s.Listen = append(s.Listen, addr)
									}
								}
							}
						}
					}
					if val, err := unit.GetProperty("org.freedesktop.systemd1.Socket.NConnections"); err == nil {
						if n, ok := val.Value().(uint32); ok {
							s.NConnections = n
						}
					}
					if val, err := unit.GetProperty("org.freedesktop.systemd1.Socket.NAccepted"); err == nil {
						if n, ok := val.Value().(uint32); ok {
							s.NAccepted = n
						}
					}
				}
				results[i] = s
			}(i, e)
		}
		wg.Wait()
		sort.Slice(results, func(i, j int) bool { return results[i].Name < results[j].Name })
		sockets = results
		return nil
	})
	return sockets, err
}
