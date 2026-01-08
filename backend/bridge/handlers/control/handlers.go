package control

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handler"
)

// RegisterHandlers registers control handlers with the new handler system
func RegisterHandlers(shutdownChan chan string) {
	handler.RegisterFunc("control", "version", func(ctx context.Context, args []string, emit handler.Events) error {
		info, err := getVersionInfo()
		if err != nil {
			return err
		}
		return emit.Result(info)
	})

	handler.RegisterFunc("control", "update", func(ctx context.Context, args []string, emit handler.Events) error {
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

	handler.RegisterFunc("control", "shutdown", func(ctx context.Context, args []string, emit handler.Events) error {
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
