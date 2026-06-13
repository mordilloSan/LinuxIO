package accounts

import (
	"context"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var api = apischema.Bindings(
	apischema.Query[apischema.NoRequest, []apischema.AccountUser]("accounts.list_users").Handle(handleListUsers),
	apischema.Query[apischema.UsernameRequest, apischema.AccountUserDetails]("accounts.get_user_details").Handle(handleGetUserDetails),
	apischema.Query[apischema.UsernameRequest, []apischema.AccountUserLogin]("accounts.list_user_logins").Handle(handleListUserLogins),
	apischema.Job[apischema.TerminateSessionRequest, apischema.NoResponse]("accounts.terminate_session").Handle(handleTerminateSession),
	apischema.Job[apischema.CreateUserRequest, apischema.NoResponse]("accounts.create_user").Handle(handleCreateUser),
	apischema.Job[apischema.UsernameRequest, apischema.NoResponse]("accounts.delete_user").Handle(handleDeleteUser),
	apischema.Job[apischema.ModifyUserRequest, apischema.NoResponse]("accounts.modify_user").Handle(handleModifyUser),
	apischema.Job[apischema.ChangePasswordRequest, apischema.NoResponse]("accounts.change_password").Handle(handleChangePassword),
	apischema.Job[apischema.UsernameRequest, apischema.NoResponse]("accounts.lock_user").Handle(handleLockUser),
	apischema.Job[apischema.UsernameRequest, apischema.NoResponse]("accounts.unlock_user").Handle(handleUnlockUser),
	apischema.Query[apischema.NoRequest, []apischema.AccountGroup]("accounts.list_groups").Handle(handleListGroups),
	apischema.Job[apischema.CreateGroupRequest, apischema.NoResponse]("accounts.create_group").Handle(handleCreateGroup),
	apischema.Job[apischema.GroupNameRequest, apischema.NoResponse]("accounts.delete_group").Handle(handleDeleteGroup),
	apischema.Job[apischema.ModifyGroupMembersRequest, apischema.NoResponse]("accounts.modify_group_members").Handle(handleModifyGroupMembers),
	apischema.Query[apischema.NoRequest, []string]("accounts.list_shells").Handle(handleListShells),
)

var Routes = api.Routes()

// RegisterHandlers registers accounts handlers with the IPC system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleListUsers(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
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

func handleListGroups(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
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

func handleListShells(ctx context.Context, _ apischema.NoRequest, emit bridgeipc.Events) error {
	result, err := ListShells(ctx)
	return bridgeipc.EmitResult(emit, result, err)
}
