package accounts

import (
	"context"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/runtime"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var routes = apischema.NewRouteCatalog()

var RouteChangePassword = routes.Job("accounts.change_password", apischema.TypeOf[apischema.ChangePasswordRequest](), apischema.NoResponse())
var RouteCreateGroup = routes.Job("accounts.create_group", apischema.TypeOf[apischema.CreateGroupRequest](), apischema.NoResponse())
var RouteCreateUser = routes.Job("accounts.create_user", apischema.TypeOf[apischema.CreateUserRequest](), apischema.NoResponse())
var RouteDeleteGroup = routes.Job("accounts.delete_group", apischema.TypeOf[apischema.GroupNameRequest](), apischema.NoResponse())
var RouteDeleteUser = routes.Job("accounts.delete_user", apischema.TypeOf[apischema.UsernameRequest](), apischema.NoResponse())
var RouteGetUserDetails = routes.Query("accounts.get_user_details", apischema.TypeOf[apischema.UsernameRequest](), apischema.TypeOf[apischema.AccountUserDetails]())
var RouteListGroups = routes.Query("accounts.list_groups", apischema.NoRequest(), apischema.TypeOf[[]apischema.AccountGroup]())
var RouteListShells = routes.Query("accounts.list_shells", apischema.NoRequest(), apischema.TypeOf[[]string]())
var RouteListUserLogins = routes.Query("accounts.list_user_logins", apischema.TypeOf[apischema.UsernameRequest](), apischema.TypeOf[[]apischema.AccountUserLogin]())
var RouteListUsers = routes.Query("accounts.list_users", apischema.NoRequest(), apischema.TypeOf[[]apischema.AccountUser]())
var RouteLockUser = routes.Job("accounts.lock_user", apischema.TypeOf[apischema.UsernameRequest](), apischema.NoResponse())
var RouteModifyGroupMembers = routes.Job("accounts.modify_group_members", apischema.TypeOf[apischema.ModifyGroupMembersRequest](), apischema.NoResponse())
var RouteModifyUser = routes.Job("accounts.modify_user", apischema.TypeOf[apischema.ModifyUserRequest](), apischema.NoResponse())
var RouteTerminateSession = routes.Job("accounts.terminate_session", apischema.TypeOf[apischema.TerminateSessionRequest](), apischema.NoResponse())
var RouteUnlockUser = routes.Job("accounts.unlock_user", apischema.TypeOf[apischema.UsernameRequest](), apischema.NoResponse())

var Routes = routes.All()

// RegisterHandlers registers accounts handlers with the IPC system
func RegisterHandlers(rt runtime.Runtime, router *bridgeipc.Router) {
	apischema.RegisterRoutes(router,
		RouteListUsers.Handle(handleListUsers),
		RouteGetUserDetails.Handle(handleGetUserDetails),
		RouteListUserLogins.Handle(handleListUserLogins),
		RouteTerminateSession.Handle(handleTerminateSession),
		RouteCreateUser.Handle(handleCreateUser),
		RouteDeleteUser.Handle(handleDeleteUser),
		RouteModifyUser.Handle(handleModifyUser),
		RouteChangePassword.Handle(handleChangePassword),
		RouteLockUser.Handle(handleLockUser),
		RouteUnlockUser.Handle(handleUnlockUser),
		RouteListGroups.Handle(handleListGroups),
		RouteCreateGroup.Handle(handleCreateGroup),
		RouteDeleteGroup.Handle(handleDeleteGroup),
		RouteModifyGroupMembers.Handle(handleModifyGroupMembers),
		RouteListShells.Handle(handleListShells),
	)
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
