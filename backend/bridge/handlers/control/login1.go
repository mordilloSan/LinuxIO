package control

import (
	"context"
	"fmt"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/dbusclient"
)

var login1ManagerIface = dbusclient.Login1Manager.Interface(dbusclient.LoginMgrIface)

const login1NonInteractive = false

// Reboot asks logind to reboot the machine.
func Reboot(ctx context.Context) error {
	return callLogin1Manager(ctx, "Reboot", login1NonInteractive)
}

// PowerOff asks logind to power off the machine.
func PowerOff(ctx context.Context) error {
	return callLogin1Manager(ctx, "PowerOff", login1NonInteractive)
}

// Logoff terminates one login1 session by ID.
func Logoff(ctx context.Context, sessionID string) error {
	return callLogin1Manager(ctx, "TerminateSession", sessionID)
}

func callLogin1Manager(ctx context.Context, method string, args ...any) error {
	return withLogin1Session(ctx, func(session dbusclient.SystemSession) error {
		if err := session.Call(
			login1ManagerIface.Method(method),
			dbusclient.CallPolicy{},
			args...,
		); err != nil {
			return fmt.Errorf("failed to call %s: %w", method, err)
		}
		return nil
	})
}

func withLogin1Session(ctx context.Context, fn func(dbusclient.SystemSession) error) error {
	return dbusclient.Login1Manager.UseSessionWithOptions(ctx, dbusclient.SystemBusOptions{
		Unserialized: true,
	}, fn)
}
