package modules

import (
	"context"
	"fmt"
	"net"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

// RegisterHandlers registers module handlers with the new handler system
func RegisterHandlers(
	sess *session.Session,
	streamHandlers map[string]func(*session.Session, net.Conn, []string) error,
) {
	// GetModules - public handler (no privilege required)
	ipc.RegisterFunc("modules", "get_modules", func(ctx context.Context, args []string, emit ipc.Events) error {
		modules, err := GetLoadedModulesForFrontend()
		if err != nil {
			return err
		}
		return emit.Result(modules)
	})

	// GetModuleDetails - privileged handler
	ipc.RegisterFunc("modules", "get_module_details", func(ctx context.Context, args []string, emit ipc.Events) error {
		if !sess.Privileged {
			return fmt.Errorf("privilege required")
		}
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		details, err := GetModuleDetailsInfo(args[0])
		if err != nil {
			return err
		}
		return emit.Result(details)
	})

	// ValidateModule - privileged handler
	ipc.RegisterFunc("modules", "validate_module", func(ctx context.Context, args []string, emit ipc.Events) error {
		if !sess.Privileged {
			return fmt.Errorf("privilege required")
		}
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := ValidateModuleAtPath(args[0])
		if err != nil {
			return err
		}
		return emit.Result(result)
	})

	// InstallModule - privileged handler
	ipc.RegisterFunc("modules", "install_module", func(ctx context.Context, args []string, emit ipc.Events) error {
		if !sess.Privileged {
			return fmt.Errorf("privilege required")
		}
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
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
	ipc.RegisterFunc("modules", "uninstall_module", func(ctx context.Context, args []string, emit ipc.Events) error {
		if !sess.Privileged {
			return fmt.Errorf("privilege required")
		}
		if len(args) < 1 {
			return ipc.ErrInvalidArgs
		}
		result, err := UninstallModuleOperation(args[0], streamHandlers)
		if err != nil {
			return err
		}
		return emit.Result(result)
	})
}
