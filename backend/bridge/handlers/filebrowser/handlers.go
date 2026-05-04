package filebrowser

import (
	"context"
	"log/slog"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type filebrowserRegistration struct {
	command string
	handler ipc.HandlerFunc
}

// RegisterHandlers registers all filebrowser handlers with the global registry
func RegisterHandlers() {
	RegisterJobRunners()

	registerFilebrowserHandlers([]filebrowserRegistration{
		{command: "resource_get", handler: emitFilebrowserArgsResult(resourceGet)},
		{command: "resource_stat", handler: emitFilebrowserArgsResult(resourceStat)},
		{command: "resource_delete", handler: emitFilebrowserLoggedArgsResult("resource_delete requested", resourceDelete)},
		{command: "resource_post", handler: emitFilebrowserLoggedArgsResult("resource_post requested", resourcePost)},
		{command: "resource_patch", handler: handleResourcePatch},
		{command: "dir_size", handler: emitFilebrowserArgsResult(dirSize)},
		{command: "indexer_status", handler: emitFilebrowserArgsResult(indexerStatus)},
		{command: "subfolders", handler: emitFilebrowserArgsResult(subfolders)},
		{command: "search", handler: emitFilebrowserArgsResult(searchFiles)},
		{command: "users_groups", handler: handleUsersGroups},
	})
}

func registerFilebrowserHandlers(registrations []filebrowserRegistration) {
	for _, registration := range registrations {
		ipc.RegisterFunc("filebrowser", registration.command, registration.handler)
	}
}

func emitFilebrowserArgsResult(fn func([]string) (any, error)) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		result, err := fn(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	}
}

func emitFilebrowserLoggedArgsResult(message string, fn func([]string) (any, error)) ipc.HandlerFunc {
	return func(ctx context.Context, args []string, emit ipc.Events) error {
		slog.Info(message, "component", "filebrowser")
		result, err := fn(args)
		if err != nil {
			return err
		}
		return emit.Result(result)
	}
}

func handleResourcePatch(ctx context.Context, args []string, emit ipc.Events) error {
	slog.Info("resource_patch requested")
	result, err := resourcePatchWithProgress(ctx, args, emit)
	if err != nil {
		return err
	}
	return emit.Result(result)
}

func handleUsersGroups(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := usersGroups()
	if err != nil {
		return err
	}
	return emit.Result(result)
}
