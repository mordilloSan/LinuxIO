package dbus

import (
	"sync"

	godbus "github.com/godbus/dbus/v5"
)

type SocketStatus struct {
	Name                   string   `json:"name"`
	Description            string   `json:"description"`
	LoadState              string   `json:"load_state"`
	ActiveState            string   `json:"active_state"`
	SubState               string   `json:"sub_state"`
	UnitFileState          string   `json:"unit_file_state"`
	ActiveEnterTimestamp   uint64   `json:"active_enter_timestamp"`
	InactiveEnterTimestamp uint64   `json:"inactive_enter_timestamp"`
	Listen                 []string `json:"listen"`
	NConnections           uint32   `json:"n_connections"`
	NAccepted              uint32   `json:"n_accepted"`
}

func ListSockets() ([]SocketStatus, error) {
	var sockets []SocketStatus
	err := withSystemdManager(func(conn *godbus.Conn, systemd godbus.BusObject) error {
		entries, err := listUnitsBySuffix(systemd, ".socket")
		if err != nil {
			return err
		}

		results := make([]SocketStatus, len(entries))
		var wg sync.WaitGroup
		for i, entry := range entries {
			wg.Add(1)
			go func(i int, entry listedUnit) {
				defer wg.Done()
				results[i] = fetchSocketStatus(conn, entry)
			}(i, entry)
		}

		wg.Wait()
		sockets = results
		return nil
	})
	return sockets, err
}

func fetchSocketStatus(conn *godbus.Conn, entry listedUnit) SocketStatus {
	socket := SocketStatus{
		Name:          entry.Name,
		Description:   entry.Description,
		LoadState:     entry.LoadState,
		ActiveState:   entry.ActiveState,
		SubState:      entry.SubState,
		UnitFileState: entry.UnitFileState,
		Listen:        []string{},
	}
	if entry.Path == "" {
		return socket
	}

	unit := unitObject(conn, entry.Path)
	if state, ok := getStringProperty(unit, "org.freedesktop.systemd1.Unit.UnitFileState"); ok {
		socket.UnitFileState = state
	}
	if ts, ok := getUint64Property(unit, "org.freedesktop.systemd1.Unit.ActiveEnterTimestamp"); ok {
		socket.ActiveEnterTimestamp = ts
	}
	if ts, ok := getUint64Property(unit, "org.freedesktop.systemd1.Unit.InactiveEnterTimestamp"); ok {
		socket.InactiveEnterTimestamp = ts
	}
	if val, err := unit.GetProperty("org.freedesktop.systemd1.Socket.Listen"); err == nil {
		if listen := parseSocketListen(val.Value()); len(listen) > 0 {
			socket.Listen = listen
		}
	}
	if n, ok := getUint32Property(unit, "org.freedesktop.systemd1.Socket.NConnections"); ok {
		socket.NConnections = n
	}
	if n, ok := getUint32Property(unit, "org.freedesktop.systemd1.Socket.NAccepted"); ok {
		socket.NAccepted = n
	}
	return socket
}
