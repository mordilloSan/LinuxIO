package control

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers control handlers with the new handler system
func RegisterHandlers(shutdownChan chan string) {
	ipc.RegisterFunc("control", "version", func(ctx context.Context, args []string, emit ipc.Events) error {
		info, err := getVersionInfo()
		if err != nil {
			return err
		}
		return emit.Result(info)
	})

	ipc.RegisterFunc("control", "update", func(ctx context.Context, args []string, emit ipc.Events) error {
		targetVersion := ""
		if len(args) > 0 {
			targetVersion = args[0]
		}
		result, err := performUpdate(targetVersion)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	ipc.RegisterFunc("control", "shutdown", func(ctx context.Context, args []string, emit ipc.Events) error {
		reason := "unknown"
		if len(args) > 0 {
			reason = args[0]
		}
		select {
		case shutdownChan <- reason:
		default:
		}
		return emit.Result("Bridge shutting down")
	})
}
