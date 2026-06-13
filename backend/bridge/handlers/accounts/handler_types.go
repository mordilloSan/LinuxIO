package accounts

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// ListUsersRequest is the request contract for accounts.list_users.
type ListUsersRequest = apischema.NoRequest

// ListUsersResponse is the response contract for accounts.list_users.
type ListUsersResponse = []apischema.AccountUser

// GetUserDetailsRequest is the request contract for accounts.get_user_details.
type GetUserDetailsRequest = UsernameRef

// GetUserDetailsResponse is the response contract for accounts.get_user_details.
type GetUserDetailsResponse = UserDetails

// ListUserLoginsRequest is the request contract for accounts.list_user_logins.
type ListUserLoginsRequest = UsernameRef

// ListUserLoginsResponse is the response contract for accounts.list_user_logins.
type ListUserLoginsResponse = []UserLogin

// TerminateSessionRequest is the request contract for accounts.terminate_session.
type TerminateSessionRequest = apischema.TerminateSessionRequest

// TerminateSessionResponse is the response contract for accounts.terminate_session.
type TerminateSessionResponse = apischema.NoResponse

// CreateUserResponse is the response contract for accounts.create_user.
type CreateUserResponse = apischema.NoResponse

// DeleteUserRequest is the request contract for accounts.delete_user.
type DeleteUserRequest = UsernameRef

// DeleteUserResponse is the response contract for accounts.delete_user.
type DeleteUserResponse = apischema.NoResponse

// ModifyUserResponse is the response contract for accounts.modify_user.
type ModifyUserResponse = apischema.NoResponse

// ChangePasswordRequest is the request contract for accounts.change_password.
type ChangePasswordRequest = apischema.ChangePasswordRequest

// ChangePasswordResponse is the response contract for accounts.change_password.
type ChangePasswordResponse = apischema.NoResponse

// LockUserRequest is the request contract for accounts.lock_user.
type LockUserRequest = UsernameRef

// LockUserResponse is the response contract for accounts.lock_user.
type LockUserResponse = apischema.NoResponse

// UnlockUserRequest is the request contract for accounts.unlock_user.
type UnlockUserRequest = UsernameRef

// UnlockUserResponse is the response contract for accounts.unlock_user.
type UnlockUserResponse = apischema.NoResponse

// ListGroupsRequest is the request contract for accounts.list_groups.
type ListGroupsRequest = apischema.NoRequest

// ListGroupsResponse is the response contract for accounts.list_groups.
type ListGroupsResponse = []Group

// CreateGroupResponse is the response contract for accounts.create_group.
type CreateGroupResponse = apischema.NoResponse

// DeleteGroupRequest is the request contract for accounts.delete_group.
type DeleteGroupRequest = GroupNameRef

// DeleteGroupResponse is the response contract for accounts.delete_group.
type DeleteGroupResponse = apischema.NoResponse

// ModifyGroupMembersResponse is the response contract for accounts.modify_group_members.
type ModifyGroupMembersResponse = apischema.NoResponse

// ListShellsRequest is the request contract for accounts.list_shells.
type ListShellsRequest = apischema.NoRequest

// ListShellsResponse is the response contract for accounts.list_shells.
type ListShellsResponse = []string
