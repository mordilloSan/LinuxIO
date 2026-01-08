package modules

import (
	"context"
	"fmt"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handler"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// RegisterHandlers registers module handlers with the new handler system
func RegisterHandlers(
	sess *session.Session,
	streamHandlers map[string]func(*session.Session, net.Conn, []string) error,
) {
	// GetModules - public handler (no privilege required)
	handler.RegisterFunc("modules", "GetModules", func(ctx context.Context, args []string, emit handler.Events) error {
		modules, err := GetLoadedModulesForFrontend()
		if err != nil {
			return err
		}
		return emit.Result(modules)
	})

	// GetModuleDetails - privileged handler
	handler.RegisterFunc("modules", "GetModuleDetails", func(ctx context.Context, args []string, emit handler.Events) error {
		if !sess.Privileged {
			return fmt.Errorf("privilege required")
		}
		if len(args) < 1 {
			return handler.ErrInvalidArgs
		}
		details, err := GetModuleDetailsInfo(args[0])
		if err != nil {
			return err
		}
		return emit.Result(details)
	})

	// ValidateModule - privileged handler
	handler.RegisterFunc("modules", "ValidateModule", func(ctx context.Context, args []string, emit handler.Events) error {
		if !sess.Privileged {
			return fmt.Errorf("privilege required")
		}
		if len(args) < 1 {
			return handler.ErrInvalidArgs
		}
		result, err := ValidateModuleAtPath(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	// InstallModule - privileged handler
	handler.RegisterFunc("modules", "InstallModule", func(ctx context.Context, args []string, emit handler.Events) error {
		if !sess.Privileged {
			return fmt.Errorf("privilege required")
		}
		if len(args) < 1 {
			return handler.ErrInvalidArgs
		}
		targetName := ""
		if len(args) > 1 {
			targetName = args[1]
		}
		createSymlink := len(args) > 2 && args[2] == "true"
		result, err := InstallModuleOperation(args[0], targetName, createSymlink, streamHandlers)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	// UninstallModule - privileged handler
	handler.RegisterFunc("modules", "UninstallModule", func(ctx context.Context, args []string, emit handler.Events) error {
		if !sess.Privileged {
			return fmt.Errorf("privilege required")
		}
		if len(args) < 1 {
			return handler.ErrInvalidArgs
		}
		result, err := UninstallModuleOperation(args[0], streamHandlers)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})
}
