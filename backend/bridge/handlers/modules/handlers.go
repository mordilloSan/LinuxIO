package modules

import (
	"context"
	"fmt"
	"net"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
)

type moduleRegistration struct {
	command string
	handler ipc.HandlerFunc
}

// RegisterHandlers registers module handlers with the new handler system
func RegisterHandlers(
	sess *session.Session,
	streamHandlers map[string]func(*session.Session, net.Conn, []string) error,
) {
	registerModuleHandlers([]moduleRegistration{
		{command: "get_modules", handler: handleGetModules},
		{command: "get_module_details", handler: requirePrivilegedModuleHandler(sess, handleGetModuleDetails)},
		{command: "validate_module", handler: requirePrivilegedModuleHandler(sess, handleValidateModule)},
		{command: "install_module", handler: requirePrivilegedModuleHandler(sess, installModuleHandler(streamHandlers))},
		{command: "uninstall_module", handler: requirePrivilegedModuleHandler(sess, uninstallModuleHandler(streamHandlers))},
	})
}

func registerModuleHandlers(registrations []moduleRegistration) {
	for _, registration := range registrations {
		ipc.RegisterFunc("modules", registration.command, registration.handler)
	}
}

func requirePrivilegedModuleHandler(sess *session.Session, next ipc.HandlerFunc) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if !sess.Privileged {
			return fmt.Errorf("privilege required")
		}
		return next(ctx, args, emit)
	}
}

func handleGetModules(ctx context.Context, args []string, emit ipc.Events) error {
	return emitModuleCall(emit, GetLoadedModulesForFrontend)
}

func handleGetModuleDetails(ctx context.Context, args []string, emit ipc.Events) error {
	if err := requireModuleArgs(args, 1); err != nil {
		return err
	}
	return emitModuleArgCall(emit, args[0], GetModuleDetailsInfo)
}

func handleValidateModule(ctx context.Context, args []string, emit ipc.Events) error {
	if err := requireModuleArgs(args, 1); err != nil {
		return err
	}
	return emitModuleArgCall(emit, args[0], ValidateModuleAtPath)
}

func installModuleHandler(
	streamHandlers map[string]func(*session.Session, net.Conn, []string) error,
) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if err := requireModuleArgs(args, 1); err != nil {
			return err
		}
		targetName := ""
		if len(args) > 1 {
			targetName = args[1]
		}
		createSymlink := len(args) > 2 && args[2] == "true"
		logger.Infof("install_module requested: source=%s target=%s create_symlink=%v", args[0], targetName, createSymlink)
		result, err := InstallModuleOperation(args[0], targetName, createSymlink, streamHandlers)
		return emitModuleResult(emit, result, err)
	}
}

func uninstallModuleHandler(
	streamHandlers map[string]func(*session.Session, net.Conn, []string) error,
) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		if err := requireModuleArgs(args, 1); err != nil {
			return err
		}
		logger.Infof("uninstall_module requested: name=%s", args[0])
		result, err := UninstallModuleOperation(args[0], streamHandlers)
		return emitModuleResult(emit, result, err)
	}
}

func requireModuleArgs(args []string, min int) error {
	if len(args) < min {
		return ipc.ErrInvalidArgs
	}
	return nil
}

func emitModuleResult(emit ipc.Events, result any, err error) error {
	if err != nil {
		return err
	}
	return emit.Result(result)
}

func emitModuleCall[T any](emit ipc.Events, fn func() (T, error)) error {
	result, err := fn()
	return emitModuleResult(emit, result, err)
}

func emitModuleArgCall[A any, T any](emit ipc.Events, arg A, fn func(A) (T, error)) error {
	result, err := fn(arg)
	return emitModuleResult(emit, result, err)
}
