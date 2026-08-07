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
	apischema.Query[apischema.TerminateSessionRequest, apischema.NoResponse]("accounts.terminate_session").HandleVoid(handleTerminateSession),
	apischema.Query[apischema.CreateUserRequest, apischema.NoResponse]("accounts.create_user").HandleVoid(CreateUser),
	apischema.Query[apischema.UsernameRequest, apischema.NoResponse]("accounts.delete_user").HandleVoid(handleDeleteUser),
	apischema.Query[apischema.ModifyUserRequest, apischema.NoResponse]("accounts.modify_user").HandleVoid(ModifyUser),
	apischema.Query[apischema.ChangePasswordRequest, apischema.NoResponse]("accounts.change_password").HandleVoid(handleChangePassword),
	apischema.Query[apischema.UsernameRequest, apischema.NoResponse]("accounts.lock_user").HandleVoid(handleLockUser),
	apischema.Query[apischema.UsernameRequest, apischema.NoResponse]("accounts.unlock_user").HandleVoid(handleUnlockUser),
	apischema.Query[apischema.NoRequest, []apischema.AccountGroup]("accounts.list_groups").Handle(handleListGroups),
	apischema.Query[apischema.CreateGroupRequest, apischema.NoResponse]("accounts.create_group").HandleVoid(CreateGroup),
	apischema.Query[apischema.GroupNameRequest, apischema.NoResponse]("accounts.delete_group").HandleVoid(handleDeleteGroup),
	apischema.Query[apischema.ModifyGroupMembersRequest, apischema.NoResponse]("accounts.modify_group_members").HandleVoid(ModifyGroupMembers),
	apischema.Query[apischema.NoRequest, []string]("accounts.list_shells").Handle(handleListShells),
)

var Routes = api.Routes()

// RegisterHandlers registers accounts handlers with the IPC system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	api.Register(router)
}

func handleListUsers(ctx context.Context, _ apischema.NoRequest) ([]apischema.AccountUser, error) {
	return ListUsers(ctx)
}

func handleGetUserDetails(ctx context.Context, req apischema.UsernameRequest) (apischema.AccountUserDetails, error) {
	result, err := GetUserDetails(ctx, req.Username)
	return accountUserDetailsToAPI(result), err
}

func handleListUserLogins(ctx context.Context, req apischema.UsernameRequest) ([]apischema.AccountUserLogin, error) {
	result, err := ListUserLogins(ctx, req.Username, 24)
	return accountUserLoginsToAPI(result), err
}

func handleTerminateSession(ctx context.Context, req apischema.TerminateSessionRequest) error {
	pid, _ := strconv.Atoi(req.PID)
	return TerminateSession(ctx, req.SessionID, pid)
}

func handleDeleteUser(ctx context.Context, req apischema.UsernameRequest) error {
	return DeleteUser(ctx, req.Username)
}

func handleChangePassword(ctx context.Context, req apischema.ChangePasswordRequest) error {
	return ChangePassword(ctx, req.Username, req.Password)
}

func handleLockUser(ctx context.Context, req apischema.UsernameRequest) error {
	return LockUser(ctx, req.Username)
}

func handleUnlockUser(ctx context.Context, req apischema.UsernameRequest) error {
	return UnlockUser(ctx, req.Username)
}

func handleDeleteGroup(ctx context.Context, req apischema.GroupNameRequest) error {
	return DeleteGroup(ctx, req.GroupName)
}

func handleListGroups(ctx context.Context, _ apischema.NoRequest) ([]apischema.AccountGroup, error) {
	result, err := ListGroups(ctx)
	return accountGroupsToAPI(result), err
}

func handleListShells(ctx context.Context, _ apischema.NoRequest) ([]string, error) {
	return ListShells(ctx)
}
