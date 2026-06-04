package accounts

import (
	"context"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	accountsapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/accounts/api"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

// RegisterHandlers registers accounts handlers with the IPC system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router, []apischema.HandlerBinding{
		{Route: accountsapi.ListUsers, Handle: handleListUsers},
		{Route: accountsapi.GetUserDetails, Handle: handleGetUserDetails},
		{Route: accountsapi.ListUserLogins, Handle: handleListUserLogins},
		{Route: accountsapi.TerminateSession, Handle: handleTerminateSession},
		{Route: accountsapi.CreateUser, Handle: handleCreateUser},
		{Route: accountsapi.DeleteUser, Handle: handleDeleteUser},
		{Route: accountsapi.ModifyUser, Handle: handleModifyUser},
		{Route: accountsapi.ChangePassword, Handle: handleChangePassword},
		{Route: accountsapi.LockUser, Handle: handleLockUser},
		{Route: accountsapi.UnlockUser, Handle: handleUnlockUser},
		{Route: accountsapi.ListGroups, Handle: handleListGroups},
		{Route: accountsapi.CreateGroup, Handle: handleCreateGroup},
		{Route: accountsapi.DeleteGroup, Handle: handleDeleteGroup},
		{Route: accountsapi.ModifyGroupMembers, Handle: handleModifyGroupMembers},
		{Route: accountsapi.ListShells, Handle: handleListShells},
	})
}

func handleListUsers(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := ListUsers(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleGetUserDetails(ctx context.Context, req apischema.UsernameRequest, emit bridgeipc.Events) error {
	result, err := GetUserDetails(ctx, req.Username)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleListUserLogins(ctx context.Context, req apischema.UsernameRequest, emit bridgeipc.Events) error {
	result, err := ListUserLogins(ctx, req.Username, 24)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleTerminateSession(ctx context.Context, req apischema.TerminateSessionRequest, emit bridgeipc.Events) error {
	pid, _ := strconv.Atoi(req.PID)
	if err := TerminateSession(ctx, req.SessionID, pid); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, nil, nil)
}

func handleCreateUser(ctx context.Context, req apischema.CreateUserRequest, emit bridgeipc.Events) error {
	if err := CreateUser(ctx, req); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, nil, nil)
}

func handleDeleteUser(ctx context.Context, req apischema.UsernameRequest, emit bridgeipc.Events) error {
	if err := DeleteUser(ctx, req.Username); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, nil, nil)
}

func handleModifyUser(ctx context.Context, req apischema.ModifyUserRequest, emit bridgeipc.Events) error {
	if err := ModifyUser(ctx, req); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, nil, nil)
}

func handleChangePassword(ctx context.Context, req apischema.ChangePasswordRequest, emit bridgeipc.Events) error {
	if err := ChangePassword(ctx, req.Username, req.Password); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, nil, nil)
}

func handleLockUser(ctx context.Context, req apischema.UsernameRequest, emit bridgeipc.Events) error {
	if err := LockUser(ctx, req.Username); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, nil, nil)
}

func handleUnlockUser(ctx context.Context, req apischema.UsernameRequest, emit bridgeipc.Events) error {
	if err := UnlockUser(ctx, req.Username); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, nil, nil)
}

func handleListGroups(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := ListGroups(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}

func handleCreateGroup(ctx context.Context, req apischema.CreateGroupRequest, emit bridgeipc.Events) error {
	if err := CreateGroup(ctx, req); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, nil, nil)
}

func handleDeleteGroup(ctx context.Context, req apischema.GroupNameRequest, emit bridgeipc.Events) error {
	if err := DeleteGroup(ctx, req.GroupName); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, nil, nil)
}

func handleModifyGroupMembers(ctx context.Context, req apischema.ModifyGroupMembersRequest, emit bridgeipc.Events) error {
	if err := ModifyGroupMembers(ctx, req); err != nil {
		return err
	}
	return bridgeipc.EmitResult(emit, nil, nil)
}

func handleListShells(ctx context.Context, _ bridgeipc.NoRequest, emit bridgeipc.Events) error {
	result, err := ListShells(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}
