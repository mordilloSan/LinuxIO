package api

import "github.com/mordilloSan/LinuxIO/backend/bridge/apischema"

var routes = apischema.NewRouteCatalog()

var ChangePassword = routes.Job("accounts.change_password", apischema.TypeOf[apischema.ChangePasswordRequest](), apischema.NoResponse())
var CreateGroup = routes.Job("accounts.create_group", apischema.TypeOf[apischema.CreateGroupRequest](), apischema.NoResponse())
var CreateUser = routes.Job("accounts.create_user", apischema.TypeOf[apischema.CreateUserRequest](), apischema.NoResponse())
var DeleteGroup = routes.Job("accounts.delete_group", apischema.TypeOf[apischema.GroupNameRequest](), apischema.NoResponse())
var DeleteUser = routes.Job("accounts.delete_user", apischema.TypeOf[apischema.UsernameRequest](), apischema.NoResponse())
var GetUserDetails = routes.Query("accounts.get_user_details", apischema.TypeOf[apischema.UsernameRequest](), apischema.TypeOf[apischema.AccountUserDetails]())
var ListGroups = routes.Query("accounts.list_groups", apischema.NoRequest(), apischema.TypeOf[[]apischema.AccountGroup]())
var ListShells = routes.Query("accounts.list_shells", apischema.NoRequest(), apischema.TypeOf[[]string]())
var ListUserLogins = routes.Query("accounts.list_user_logins", apischema.TypeOf[apischema.UsernameRequest](), apischema.TypeOf[[]apischema.AccountUserLogin]())
var ListUsers = routes.Query("accounts.list_users", apischema.NoRequest(), apischema.TypeOf[[]apischema.AccountUser]())
var LockUser = routes.Job("accounts.lock_user", apischema.TypeOf[apischema.UsernameRequest](), apischema.NoResponse())
var ModifyGroupMembers = routes.Job("accounts.modify_group_members", apischema.TypeOf[apischema.ModifyGroupMembersRequest](), apischema.NoResponse())
var ModifyUser = routes.Job("accounts.modify_user", apischema.TypeOf[apischema.ModifyUserRequest](), apischema.NoResponse())
var TerminateSession = routes.Job("accounts.terminate_session", apischema.TypeOf[apischema.TerminateSessionRequest](), apischema.NoResponse())
var UnlockUser = routes.Job("accounts.unlock_user", apischema.TypeOf[apischema.UsernameRequest](), apischema.NoResponse())

var Routes = routes.All()
