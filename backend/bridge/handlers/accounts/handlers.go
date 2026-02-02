package accounts

import (
	"context"
	"encoding/json"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// RegisterHandlers registers accounts handlers with the IPC system
func RegisterHandlers() {
	// User management
	ipc.RegisterFunc("accounts", "list_users", func(ctx context.Context, args []string, emit ipc.Events) error {
		users, err := ListUsers()
		if err != nil {
			return err
		}
		return emit.Result(users)
	})

	ipc.RegisterFunc("accounts", "get_user", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		user, err := GetUser(args[0])
		if err != nil {
			return err
		}
		return emit.Result(user)
	})

	ipc.RegisterFunc("accounts", "create_user", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		var req CreateUserRequest
		if err := json.Unmarshal([]byte(args[0]), &req); err != nil {
			return err
		}
		if err := CreateUser(req); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("accounts", "delete_user", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		if err := DeleteUser(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("accounts", "modify_user", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		var req ModifyUserRequest
		if err := json.Unmarshal([]byte(args[0]), &req); err != nil {
			return err
		}
		if err := ModifyUser(req); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("accounts", "change_password", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) < 2 {
			return ipc.ErrInvalidArgs
		}
		if err := ChangePassword(args[0], args[1]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("accounts", "lock_user", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		if err := LockUser(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("accounts", "unlock_user", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		if err := UnlockUser(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	// Group management
	ipc.RegisterFunc("accounts", "list_groups", func(ctx context.Context, args []string, emit ipc.Events) error {
		groups, err := ListGroups()
		if err != nil {
			return err
		}
		return emit.Result(groups)
	})

	ipc.RegisterFunc("accounts", "get_group", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		group, err := GetGroup(args[0])
		if err != nil {
			return err
		}
		return emit.Result(group)
	})

	ipc.RegisterFunc("accounts", "create_group", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		var req CreateGroupRequest
		if err := json.Unmarshal([]byte(args[0]), &req); err != nil {
			return err
		}
		if err := CreateGroup(req); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("accounts", "delete_group", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		if err := DeleteGroup(args[0]); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	ipc.RegisterFunc("accounts", "modify_group_members", func(ctx context.Context, args []string, emit ipc.Events) error {
		if len(args) == 0 {
			return ipc.ErrInvalidArgs
		}
		var req ModifyGroupMembersRequest
		if err := json.Unmarshal([]byte(args[0]), &req); err != nil {
			return err
		}
		if err := ModifyGroupMembers(req); err != nil {
			return err
		}
		return emit.Result(nil)
	})

	// Utility
	ipc.RegisterFunc("accounts", "list_shells", func(ctx context.Context, args []string, emit ipc.Events) error {
		shells, err := ListShells()
		if err != nil {
			return err
		}
		return emit.Result(shells)
	})
}
