package system

import (
	"context"
	"errors"
	"time"

	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
)

type ServiceInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ActiveState string `json:"active_state"`
	SubState    string `json:"sub_state"`
	MainPID     int32  `json:"main_pid"`
	Failed      bool   `json:"failed"`
}

func FetchServices(parent context.Context) ([]ServiceInfo, error) {
	ctx, cancel := context.WithTimeout(parent, 2*time.Second)
	defer cancel()

	services, err := systemdapi.ListServices(ctx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("systemd query timed out")
		}
		return nil, err
	}

	result := make([]ServiceInfo, 0, len(services))
	for _, service := range services {
		result = append(result, ServiceInfo{
			Name:        service.Name,
			Description: service.Description,
			ActiveState: service.ActiveState,
			SubState:    service.SubState,
			MainPID:     service.MainPID,
			Failed:      service.ActiveState == "failed" || service.SubState == "failed",
		})
	}
	return result, nil
}
