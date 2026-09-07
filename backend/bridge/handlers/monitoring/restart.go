package monitoring

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
)

const monitoringServiceName = "linuxio-monitoring.service"

// restartReadyTimeout bounds the wait for a restarted daemon. It opens its
// sockets only after a first full collection - a Docker stats pass plus a SMART
// pass over every drive - which routinely outlives commandRetryTimeout.
var restartReadyTimeout = 60 * time.Second

func RestartAgent(ctx context.Context) error {
	if err := systemd.RestartUnit(ctx, monitoringServiceName); err != nil {
		return err
	}
	return WaitAgentReady(ctx)
}

func WaitAgentReady(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, restartReadyTimeout)
	defer cancel()

	var lastErr error
	for {
		_, err := runCommand(ctx, "status.get", nil)
		switch {
		case err == nil:
			return nil
		case ctx.Err() != nil:
			// The readiness window closed: report why the daemon stayed
			// unreachable, not the expired context that stopped us asking.
			if lastErr == nil {
				lastErr = err
			}
			return fmt.Errorf("wait for linuxio-monitoring readiness: %w", errors.Join(ctx.Err(), lastErr))
		case !isTransientCommandDialError(err):
			return err
		}
		lastErr = err

		timer := time.NewTimer(commandRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("wait for linuxio-monitoring readiness: %w", errors.Join(ctx.Err(), lastErr))
		case <-timer.C:
		}
	}
}
