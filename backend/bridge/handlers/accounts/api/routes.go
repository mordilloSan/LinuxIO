package api

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
)

var ChangePassword = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.change_password", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ChangePasswordRequest](), Result: apischema.NoResponse()}
var CreateGroup = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.create_group", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.CreateGroupRequest](), Result: apischema.NoResponse()}
var CreateUser = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.create_user", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.CreateUserRequest](), Result: apischema.NoResponse()}
var DeleteGroup = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.delete_group", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.GroupNameRequest](), Result: apischema.NoResponse()}
var DeleteUser = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.delete_user", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.UsernameRequest](), Result: apischema.NoResponse()}
var GetUserDetails = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.get_user_details", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.UsernameRequest](), Result: apischema.TypeOf[apischema.AccountUserDetails]()}
var ListGroups = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.list_groups", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.AccountGroup]()}
var ListShells = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.list_shells", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]string]()}
var ListUserLogins = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.list_user_logins", Mode: bridgeipc.ModeQuery, Request: apischema.TypeOf[apischema.UsernameRequest](), Result: apischema.TypeOf[[]apischema.AccountUserLogin]()}
var ListUsers = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.list_users", Mode: bridgeipc.ModeQuery, Request: apischema.NoRequest(), Result: apischema.TypeOf[[]apischema.AccountUser]()}
var LockUser = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.lock_user", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.UsernameRequest](), Result: apischema.NoResponse()}
var ModifyGroupMembers = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.modify_group_members", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ModifyGroupMembersRequest](), Result: apischema.NoResponse()}
var ModifyUser = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.modify_user", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.ModifyUserRequest](), Result: apischema.NoResponse()}
var TerminateSession = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.terminate_session", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.TerminateSessionRequest](), Result: apischema.NoResponse()}
var UnlockUser = apischema.RouteSpec{Kind: apischema.KindHandler, Route: "accounts.unlock_user", Mode: bridgeipc.ModeJob, Request: apischema.TypeOf[apischema.UsernameRequest](), Result: apischema.NoResponse()}

var Routes = []apischema.RouteSpec{
	ChangePassword,
	CreateGroup,
	CreateUser,
	DeleteGroup,
	DeleteUser,
	GetUserDetails,
	ListGroups,
	ListShells,
	ListUserLogins,
	ListUsers,
	LockUser,
	ModifyGroupMembers,
	ModifyUser,
	TerminateSession,
	UnlockUser,
}
