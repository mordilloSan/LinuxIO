package accounts

import (
	"context"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
)

var api = apischema.Bindings(
	apischema.Call[apischema.NoRequest, []apischema.AccountUser]("accounts.list_users", apischema.RetrySafe()).Handle(handleListUsers),
	apischema.Call[apischema.UsernameRequest, apischema.AccountUserDetails]("accounts.get_user_details", apischema.RetrySafe()).Handle(handleGetUserDetails),
	apischema.Call[apischema.UsernameRequest, []apischema.AccountUserLogin]("accounts.list_user_logins", apischema.RetrySafe()).Handle(handleListUserLogins),
	apischema.MutationVoid("accounts.terminate_session", handleTerminateSession),
	apischema.MutationVoid("accounts.create_user", CreateUser, apischema.Privileged()),
	apischema.MutationVoid("accounts.delete_user", handleDeleteUser, apischema.Privileged()),
	apischema.MutationVoid("accounts.modify_user", ModifyUser, apischema.Privileged()),
	apischema.MutationVoid("accounts.change_password", handleChangePassword, apischema.Privileged()),
	apischema.MutationVoid("accounts.lock_user", handleLockUser, apischema.Privileged()),
	apischema.MutationVoid("accounts.unlock_user", handleUnlockUser, apischema.Privileged()),
	apischema.Call[apischema.NoRequest, []apischema.AccountGroup]("accounts.list_groups", apischema.RetrySafe()).Handle(handleListGroups),
	apischema.MutationVoid("accounts.create_group", CreateGroup, apischema.Privileged()),
	apischema.MutationVoid("accounts.delete_group", handleDeleteGroup, apischema.Privileged()),
	apischema.MutationVoid("accounts.modify_group_members", ModifyGroupMembers, apischema.Privileged()),
	apischema.Call[apischema.NoRequest, []string]("accounts.list_shells", apischema.RetrySafe()).Handle(handleListShells),
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
	return GetUserDetails(ctx, req.Username)
}

func handleListUserLogins(ctx context.Context, req apischema.UsernameRequest) ([]apischema.AccountUserLogin, error) {
	return ListUserLogins(ctx, req.Username, 24)
}

func handleTerminateSession(ctx context.Context, req apischema.TerminateSessionRequest) error {
	return TerminateSession(ctx, req.SessionID)
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
	return ListGroups(ctx)
}

func handleListShells(ctx context.Context, _ apischema.NoRequest) ([]string, error) {
	return ListShells(ctx)
}
