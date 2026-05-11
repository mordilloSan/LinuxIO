package dbus

import (
	"context"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

// Reboot asks logind to reboot the machine.
func Reboot(ctx context.Context) error {
	return callLogin1Action(ctx, "Reboot")
}

// PowerOff asks logind to power off the machine.
func PowerOff(ctx context.Context) error {
	return callLogin1Action(ctx, "PowerOff")
}

func callLogin1Action(ctx context.Context, action string) error {
	if err := dbusclient.Login1Manager.Interface(dbusclient.LoginMgrIface).Call(
		ctx,
		action,
		dbusclient.CallPolicy{},
		false,
	); err != nil {
		return fmt.Errorf("failed to call %s: %w", action, err)
	}
	return nil
}

// TerminateLogin1Session calls org.freedesktop.login1.Manager.TerminateSession.
func TerminateLogin1Session(ctx context.Context, sessionID string) error {
	if err := dbusclient.Login1Manager.Interface(dbusclient.LoginMgrIface).Call(
		ctx,
		"TerminateSession",
		dbusclient.CallPolicy{},
		sessionID,
	); err != nil {
		return fmt.Errorf("failed to call TerminateSession: %w", err)
	}
	return nil
}
