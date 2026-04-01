package accounts

import (
	"context"
	"encoding/json"

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

type accountRegistration struct {
	command string
	handler ipc.HandlerFunc
}

// RegisterHandlers registers accounts handlers with the IPC system
func RegisterHandlers() {
	registerAccountHandlers([]accountRegistration{
		{command: "list_users", handler: handleListUsers},
		{command: "create_user", handler: handleCreateUser},
		{command: "delete_user", handler: handleDeleteUser},
		{command: "modify_user", handler: handleModifyUser},
		{command: "change_password", handler: handleChangePassword},
		{command: "lock_user", handler: handleLockUser},
		{command: "unlock_user", handler: handleUnlockUser},
		{command: "list_groups", handler: handleListGroups},
		{command: "create_group", handler: handleCreateGroup},
		{command: "delete_group", handler: handleDeleteGroup},
		{command: "modify_group_members", handler: handleModifyGroupMembers},
		{command: "list_shells", handler: handleListShells},
	})
}

func registerAccountHandlers(registrations []accountRegistration) {
	for _, registration := range registrations {
		ipc.RegisterFunc("accounts", registration.command, registration.handler)
	}
}

func handleListUsers(ctx context.Context, args []string, emit ipc.Events) error {
	return emitAccountCall(emit, ListUsers)
}

func handleCreateUser(ctx context.Context, args []string, emit ipc.Events) error {
	req, err := decodeAccountJSON[CreateUserRequest](args)
	if err != nil {
		return err
	}
	logger.Infof("create_user requested: username=%s", req.Username)
	if err := CreateUser(req); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleDeleteUser(ctx context.Context, args []string, emit ipc.Events) error {
	if err := requireAccountArgs(args, 1); err != nil {
		return err
	}
	logger.Infof("delete_user requested: username=%s", args[0])
	if err := DeleteUser(args[0]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleModifyUser(ctx context.Context, args []string, emit ipc.Events) error {
	req, err := decodeAccountJSON[ModifyUserRequest](args)
	if err != nil {
		return err
	}
	logger.Infof("modify_user requested: username=%s", req.Username)
	if err := ModifyUser(req); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleChangePassword(ctx context.Context, args []string, emit ipc.Events) error {
	if err := requireAccountArgs(args, 2); err != nil {
		return err
	}
	logger.Infof("change_password requested: username=%s", args[0])
	if err := ChangePassword(args[0], args[1]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleLockUser(ctx context.Context, args []string, emit ipc.Events) error {
	if err := requireAccountArgs(args, 1); err != nil {
		return err
	}
	logger.Infof("lock_user requested: username=%s", args[0])
	if err := LockUser(args[0]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleUnlockUser(ctx context.Context, args []string, emit ipc.Events) error {
	if err := requireAccountArgs(args, 1); err != nil {
		return err
	}
	logger.Infof("unlock_user requested: username=%s", args[0])
	if err := UnlockUser(args[0]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleListGroups(ctx context.Context, args []string, emit ipc.Events) error {
	return emitAccountCall(emit, ListGroups)
}

func handleCreateGroup(ctx context.Context, args []string, emit ipc.Events) error {
	req, err := decodeAccountJSON[CreateGroupRequest](args)
	if err != nil {
		return err
	}
	logger.Infof("create_group requested: group=%s", req.Name)
	if err := CreateGroup(req); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleDeleteGroup(ctx context.Context, args []string, emit ipc.Events) error {
	if err := requireAccountArgs(args, 1); err != nil {
		return err
	}
	logger.Infof("delete_group requested: group=%s", args[0])
	if err := DeleteGroup(args[0]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleModifyGroupMembers(ctx context.Context, args []string, emit ipc.Events) error {
	req, err := decodeAccountJSON[ModifyGroupMembersRequest](args)
	if err != nil {
		return err
	}
	logger.Infof("modify_group_members requested: group=%s", req.GroupName)
	if err := ModifyGroupMembers(req); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleListShells(ctx context.Context, args []string, emit ipc.Events) error {
	return emitAccountCall(emit, ListShells)
}

func decodeAccountJSON[T any](args []string) (T, error) {
	var zero T
	if err := requireAccountArgs(args, 1); err != nil {
		return zero, err
	}
	var payload T
	if err := json.Unmarshal([]byte(args[0]), &payload); err != nil {
		return zero, err
	}
	return payload, nil
}

func requireAccountArgs(args []string, min int) error {
	if len(args) < min {
		return ipc.ErrInvalidArgs
	}
	return nil
}

func emitAccountResult(emit ipc.Events, result any, err error) error {
	if err != nil {
		return err
	}
	return emit.Result(result)
}

func emitAccountCall[T any](emit ipc.Events, fn func() (T, error)) error {
	result, err := fn()
	return emitAccountResult(emit, result, err)
}

func emitAccountArgCall[A any, T any](emit ipc.Events, arg A, fn func(A) (T, error)) error {
	result, err := fn(arg)
	return emitAccountResult(emit, result, err)
}
