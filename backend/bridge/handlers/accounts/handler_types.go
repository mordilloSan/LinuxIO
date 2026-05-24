package accounts

import bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"

// ListUsersRequest is the request contract for accounts.list_users.
type ListUsersRequest = bridgeipc.NoRequest

// ListUsersResponse is the response contract for accounts.list_users.
type ListUsersResponse = []User

// GetUserDetailsRequest is the request contract for accounts.get_user_details.
type GetUserDetailsRequest = UsernameRef

// GetUserDetailsResponse is the response contract for accounts.get_user_details.
type GetUserDetailsResponse = UserDetails

// ListUserLoginsRequest is the request contract for accounts.list_user_logins.
type ListUserLoginsRequest = UsernameRef

// ListUserLoginsResponse is the response contract for accounts.list_user_logins.
type ListUserLoginsResponse = []UserLogin

// TerminateSessionRequest is the request contract for accounts.terminate_session.
type TerminateSessionRequest struct {
	SessionID string `json:"sessionId"`
	PID       int    `json:"pid"`
}

// TerminateSessionResponse is the response contract for accounts.terminate_session.
type TerminateSessionResponse = bridgeipc.NoResponse

// CreateUserResponse is the response contract for accounts.create_user.
type CreateUserResponse = bridgeipc.NoResponse

// DeleteUserRequest is the request contract for accounts.delete_user.
type DeleteUserRequest = UsernameRef

// DeleteUserResponse is the response contract for accounts.delete_user.
type DeleteUserResponse = bridgeipc.NoResponse

// ModifyUserResponse is the response contract for accounts.modify_user.
type ModifyUserResponse = bridgeipc.NoResponse

// ChangePasswordRequest is the request contract for accounts.change_password.
type ChangePasswordRequest struct {
	UsernameRef
	Password string `json:"password"`
}

// ChangePasswordResponse is the response contract for accounts.change_password.
type ChangePasswordResponse = bridgeipc.NoResponse

// LockUserRequest is the request contract for accounts.lock_user.
type LockUserRequest = UsernameRef

// LockUserResponse is the response contract for accounts.lock_user.
type LockUserResponse = bridgeipc.NoResponse

// UnlockUserRequest is the request contract for accounts.unlock_user.
type UnlockUserRequest = UsernameRef

// UnlockUserResponse is the response contract for accounts.unlock_user.
type UnlockUserResponse = bridgeipc.NoResponse

// ListGroupsRequest is the request contract for accounts.list_groups.
type ListGroupsRequest = bridgeipc.NoRequest

// ListGroupsResponse is the response contract for accounts.list_groups.
type ListGroupsResponse = []Group

// CreateGroupResponse is the response contract for accounts.create_group.
type CreateGroupResponse = bridgeipc.NoResponse

// DeleteGroupRequest is the request contract for accounts.delete_group.
type DeleteGroupRequest = GroupNameRef

// DeleteGroupResponse is the response contract for accounts.delete_group.
type DeleteGroupResponse = bridgeipc.NoResponse

// ModifyGroupMembersResponse is the response contract for accounts.modify_group_members.
type ModifyGroupMembersResponse = bridgeipc.NoResponse

// ListShellsRequest is the request contract for accounts.list_shells.
type ListShellsRequest = bridgeipc.NoRequest

// ListShellsResponse is the response contract for accounts.list_shells.
type ListShellsResponse = []string
