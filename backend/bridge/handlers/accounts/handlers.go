package accounts

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/bridge/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers accounts handlers with the IPC system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	bridgeipc.RegisterRoutes(router, "accounts", []bridgeipc.Command{
		{Name: "list_users", Mode: bridgeipc.ModeQuery, Handler: handleListUsers},
		{Name: "get_user_details", Mode: bridgeipc.ModeQuery, Handler: handleGetUserDetails},
		{Name: "list_user_logins", Mode: bridgeipc.ModeQuery, Handler: handleListUserLogins},
		{Name: "terminate_session", Mode: bridgeipc.ModeJob, Handler: handleTerminateSession},
		{Name: "create_user", Mode: bridgeipc.ModeJob, Handler: handleCreateUser},
		{Name: "delete_user", Mode: bridgeipc.ModeJob, Handler: handleDeleteUser},
		{Name: "modify_user", Mode: bridgeipc.ModeJob, Handler: handleModifyUser},
		{Name: "change_password", Mode: bridgeipc.ModeJob, Handler: handleChangePassword},
		{Name: "lock_user", Mode: bridgeipc.ModeJob, Handler: handleLockUser},
		{Name: "unlock_user", Mode: bridgeipc.ModeJob, Handler: handleUnlockUser},
		{Name: "list_groups", Mode: bridgeipc.ModeQuery, Handler: handleListGroups},
		{Name: "create_group", Mode: bridgeipc.ModeJob, Handler: handleCreateGroup},
		{Name: "delete_group", Mode: bridgeipc.ModeJob, Handler: handleDeleteGroup},
		{Name: "modify_group_members", Mode: bridgeipc.ModeJob, Handler: handleModifyGroupMembers},
		{Name: "list_shells", Mode: bridgeipc.ModeQuery, Handler: handleListShells},
	})
}

func handleListUsers(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListUsers()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUserDetails(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 1); err != nil {
		return err
	}
	result, err := GetUserDetails(ctx, args[0])
	return bridgeipc.EmitResult(emit, result, err)
}

func handleListUserLogins(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 1); err != nil {
		return err
	}
	result, err := ListUserLogins(ctx, args[0], 24)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleTerminateSession(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 2); err != nil {
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

func handleCreateUser(ctx context.Context, args []string, emit bridgeipc.Events) error {
	req, err := bridgeipc.DecodeJSONArg[CreateUserRequest](args, 0)
	if err != nil {
		return err
	}
	slog.Info("create user requested", "user", req.Username)
	if err := CreateUser(req); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleDeleteUser(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 1); err != nil {
		return err
	}
	slog.Info("delete user requested", "user", args[0])
	if err := DeleteUser(args[0]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleModifyUser(ctx context.Context, args []string, emit bridgeipc.Events) error {
	req, err := bridgeipc.DecodeJSONArg[ModifyUserRequest](args, 0)
	if err != nil {
		return err
	}
	slog.Info("modify user requested", "user", req.Username)
	if err := ModifyUser(req); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleChangePassword(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 2); err != nil {
		return err
	}
	slog.Info("change password requested", "user", args[0])
	if err := ChangePassword(args[0], args[1]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleLockUser(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 1); err != nil {
		return err
	}
	slog.Info("lock user requested", "user", args[0])
	if err := LockUser(args[0]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleUnlockUser(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 1); err != nil {
		return err
	}
	slog.Info("unlock user requested", "user", args[0])
	if err := UnlockUser(args[0]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleListGroups(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListGroups()
	return bridgeipc.EmitResult(emit, result, err)
}

func handleCreateGroup(ctx context.Context, args []string, emit bridgeipc.Events) error {
	req, err := bridgeipc.DecodeJSONArg[CreateGroupRequest](args, 0)
	if err != nil {
		return err
	}
	slog.Info("create group requested", "group", req.Name)
	if err := CreateGroup(req); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleDeleteGroup(ctx context.Context, args []string, emit bridgeipc.Events) error {
	if err := bridgeipc.RequireArgs(args, 1); err != nil {
		return err
	}
	slog.Info("delete group requested", "group", args[0])
	if err := DeleteGroup(args[0]); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleModifyGroupMembers(ctx context.Context, args []string, emit bridgeipc.Events) error {
	req, err := bridgeipc.DecodeJSONArg[ModifyGroupMembersRequest](args, 0)
	if err != nil {
		return err
	}
	slog.Info("modify group members requested", "group", req.GroupName)
	if err := ModifyGroupMembers(req); err != nil {
		return err
	}
	return emit.Result(nil)
}

func handleListShells(ctx context.Context, args []string, emit bridgeipc.Events) error {
	result, err := ListShells()
	return bridgeipc.EmitResult(emit, result, err)
}
