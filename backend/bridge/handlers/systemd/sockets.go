package systemd

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
)

type SocketStatus struct {
	UnitStatus
	Listen       []string `json:"listen"`
	NConnections uint32   `json:"n_connections"`
	NAccepted    uint32   `json:"n_accepted"`
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
	socket := SocketStatus{UnitStatus: entry.UnitStatus, Listen: []string{}}
	if entry.Path == "" {
		return socket
	}

	unit := session.ObjectAt(entry.Path)
	socket.fetchUnitProperties(session, unit)
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
