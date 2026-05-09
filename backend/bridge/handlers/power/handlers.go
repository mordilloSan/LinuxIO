package power

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/bridge/privilege"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type powerRegistration struct {
	command string
	handler ipc.HandlerFunc
}

func RegisterHandlers(sess *session.Session) {
	for _, registration := range []powerRegistration{
		{command: "get_status", handler: handleGetStatus},
		{command: "start", handler: handleStart},
		{command: "set_profile", handler: handleSetProfile},
		{command: "disable", handler: handleDisable},
	} {
		ipc.RegisterFunc(
			"power",
			registration.command,
			privilege.RequirePrivilegedIPC(sess, registration.handler),
		)
	}
}

func handleGetStatus(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := GetStatus()
	if err != nil {
		return err
	}
	return emit.Result(result)
}

func handleStart(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("TuneD start requested", "component", "power")
	result, err := StartTuned()
	if err != nil {
		return err
	}
	return emit.Result(result)
}

func handleSetProfile(ctx context.Context, args []string, emit ipc.Events) error {
	if len(args) != 1 {
		return ipc.ErrInvalidArgs
	}
	slog.Info("TuneD profile change requested", "component", "power", "profile", args[0])
	result, err := SetProfile(args[0])
	if err != nil {
		return err
	}
	return emit.Result(result)
}

func handleDisable(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("TuneD disable requested", "component", "power")
	result, err := DisableTuned()
	if err != nil {
		return err
	}
	return emit.Result(result)
}
