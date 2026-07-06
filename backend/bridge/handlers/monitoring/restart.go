package monitoring

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
)

const monitoringServiceName = "go-monitoring.service"

func RestartAgent(ctx context.Context) error {
	if err := systemd.RestartUnit(ctx, monitoringServiceName); err != nil {
		return err
	}
	return WaitAgentReady(ctx)
}

func WaitAgentReady(ctx context.Context) error {
	_, err := runCommand(ctx, "status.get", nil)
	return err
}
