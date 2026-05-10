package accounts

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/internal/rpc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers accounts handlers with the IPC system
func RegisterHandlers(rt runtime.Runtime) {
	rpc.Register("accounts", rt, []rpc.Command{
		{Name: "list_users", Handler: handleListUsers},
		{Name: "get_user_details", Handler: handleGetUserDetails},
		{Name: "list_user_logins", Handler: handleListUserLogins},
		{Name: "terminate_session", Handler: handleTerminateSession},
		{Name: "create_user", Handler: handleCreateUser},
		{Name: "delete_user", Handler: handleDeleteUser},
		{Name: "modify_user", Handler: handleModifyUser},
		{Name: "change_password", Handler: handleChangePassword},
		{Name: "lock_user", Handler: handleLockUser},
		{Name: "unlock_user", Handler: handleUnlockUser},
		{Name: "list_groups", Handler: handleListGroups},
		{Name: "create_group", Handler: handleCreateGroup},
		{Name: "delete_group", Handler: handleDeleteGroup},
		{Name: "modify_group_members", Handler: handleModifyGroupMembers},
		{Name: "list_shells", Handler: handleListShells},
	})
}

func handleListUsers(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := ListUsers()
	return rpc.EmitResult(emit, result, err)
}

func handleGetUserDetails(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 1); err != nil {
		return err
	}
	result, err := GetUserDetails(ctx, args[0])
	return rpc.EmitResult(emit, result, err)
}

func handleListUserLogins(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 1); err != nil {
		return err
	}
	result, err := ListUserLogins(ctx, args[0], 24)
	return rpc.EmitResult(emit, result, err)
}

func handleTerminateSession(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 2); err != nil {
		return err
	}
	sessionID := args[0]
	pid, _ := strconv.Atoi(args[1])
	slog.Info("terminate session requested", "sessionID", sessionID, "pid", pid)
	if err := TerminateSession(ctx, sessionID, pid); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleCreateUser(ctx context.Context, args []string, emit ipc.Events) error {
	req, err := rpc.DecodeJSONArg[CreateUserRequest](args, 0)
	if err != nil {
		return err
	}
	slog.Info("create user requested", "user", req.Username)
	if err := CreateUser(req); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleDeleteUser(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 1); err != nil {
		return err
	}
	slog.Info("delete user requested", "user", args[0])
	if err := DeleteUser(args[0]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleModifyUser(ctx context.Context, args []string, emit ipc.Events) error {
	req, err := rpc.DecodeJSONArg[ModifyUserRequest](args, 0)
	if err != nil {
		return err
	}
	slog.Info("modify user requested", "user", req.Username)
	if err := ModifyUser(req); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleChangePassword(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 2); err != nil {
		return err
	}
	slog.Info("change password requested", "user", args[0])
	if err := ChangePassword(args[0], args[1]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleLockUser(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 1); err != nil {
		return err
	}
	slog.Info("lock user requested", "user", args[0])
	if err := LockUser(args[0]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleUnlockUser(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 1); err != nil {
		return err
	}
	slog.Info("unlock user requested", "user", args[0])
	if err := UnlockUser(args[0]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleListGroups(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := ListGroups()
	return rpc.EmitResult(emit, result, err)
}

func handleCreateGroup(ctx context.Context, args []string, emit ipc.Events) error {
	req, err := rpc.DecodeJSONArg[CreateGroupRequest](args, 0)
	if err != nil {
		return err
	}
	slog.Info("create group requested", "group", req.Name)
	if err := CreateGroup(req); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleDeleteGroup(ctx context.Context, args []string, emit ipc.Events) error {
	if err := rpc.RequireArgs(args, 1); err != nil {
		return err
	}
	slog.Info("delete group requested", "group", args[0])
	if err := DeleteGroup(args[0]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleModifyGroupMembers(ctx context.Context, args []string, emit ipc.Events) error {
	req, err := rpc.DecodeJSONArg[ModifyGroupMembersRequest](args, 0)
	if err != nil {
		return err
	}
	slog.Info("modify group members requested", "group", req.GroupName)
	if err := ModifyGroupMembers(req); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleListShells(ctx context.Context, args []string, emit ipc.Events) error {
	result, err := ListShells()
	return rpc.EmitResult(emit, result, err)
}
