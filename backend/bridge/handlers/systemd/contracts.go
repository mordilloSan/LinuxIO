package systemd

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

func timersToAPI(values []TimerStatus) []apischema.Timer {
	result := make([]apischema.Timer, len(values))
	for i, value := range values {
		result[i] = apischema.Timer{
			Name: value.Name, Description: optionalString(value.Description), LoadState: value.LoadState,
			ActiveState: value.ActiveState, SubState: value.SubState, UnitFileState: value.UnitFileState,
			ActiveEnterTimestamp: int64(value.ActiveEnterTimestamp), InactiveEnterTimestamp: int64(value.InactiveEnterTimestamp),
			NextElapseUSec: int64(value.NextElapseUSec), LastTriggerUSec: int64(value.LastTriggerUSec), Unit: value.Unit,
		}
	}
	return result
}

func socketsToAPI(values []SocketStatus) []apischema.Socket {
	result := make([]apischema.Socket, len(values))
	for i, value := range values {
		result[i] = apischema.Socket{
			Name: value.Name, Description: optionalString(value.Description), LoadState: value.LoadState,
			ActiveState: value.ActiveState, SubState: value.SubState, UnitFileState: value.UnitFileState,
			ActiveEnterTimestamp: int64(value.ActiveEnterTimestamp), InactiveEnterTimestamp: int64(value.InactiveEnterTimestamp),
			Listen: value.Listen, NConnections: int(value.NConnections), NAccepted: int(value.NAccepted),
		}
	}
	return result
}

func servicesToAPI(values []ServiceStatus) []apischema.Service {
	result := make([]apischema.Service, len(values))
	for i, value := range values {
		result[i] = apischema.Service{
			Name: value.Name, Description: optionalString(value.Description), LoadState: value.LoadState,
			ActiveState: value.ActiveState, SubState: value.SubState, UnitFileState: value.UnitFileState,
			ActiveEnterTimestamp: int64(value.ActiveEnterTimestamp), InactiveEnterTimestamp: int64(value.InactiveEnterTimestamp),
		}
	}
	return result
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
