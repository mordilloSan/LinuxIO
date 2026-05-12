package systemd

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
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

func ListSockets(ctx context.Context) ([]SocketStatus, error) {
	var sockets []SocketStatus
	err := dbusclient.SystemdManager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		entries, err := listUnitsBySuffix(session, ".socket")
		if err != nil {
			return err
		}

		results := make([]SocketStatus, len(entries))
		if err := forEachListedUnitLimited(session.Context(), entries, func(i int, entry listedUnit) {
			results[i] = fetchSocketStatus(session, entry)
		}); err != nil {
			return err
		}
		sockets = results
		return nil
	})
	return sockets, err
}

func fetchSocketStatus(session dbusclient.SystemSession, entry listedUnit) SocketStatus {
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

	unit := session.ObjectAt(entry.Path)
	if state, ok := getStringProperty(session, unit, dbusclient.SystemdUnitIface, "UnitFileState"); ok {
		socket.UnitFileState = state
	}
	if ts, ok := getUint64Property(session, unit, dbusclient.SystemdUnitIface, "ActiveEnterTimestamp"); ok {
		socket.ActiveEnterTimestamp = ts
	}
	if ts, ok := getUint64Property(session, unit, dbusclient.SystemdUnitIface, "InactiveEnterTimestamp"); ok {
		socket.InactiveEnterTimestamp = ts
	}
	if val, err := dbusclient.GetVariantProperty(session.Context(), unit, dbusclient.SystemdSocketIface, "Listen"); err == nil {
		if listen := parseSocketListen(val.Value()); len(listen) > 0 {
			socket.Listen = listen
		}
	}
	if n, ok := getUint32Property(session, unit, dbusclient.SystemdSocketIface, "NConnections"); ok {
		socket.NConnections = n
	}
	if n, ok := getUint32Property(session, unit, dbusclient.SystemdSocketIface, "NAccepted"); ok {
		socket.NAccepted = n
	}
	return socket
}
