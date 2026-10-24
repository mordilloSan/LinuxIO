package accounts

import (
	osuser "os/user"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func TestValidateChpasswdInput(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
	}{
		{
			name:     "plain values allowed",
			username: "alice",
			password: "hunter2",
		},
		{
			name:     "username rejects colon",
			username: "ali:ce",
			password: "hunter2",
			wantErr:  true,
		},
		{
			name:     "password rejects newline",
			username: "alice",
			password: "line1\nline2",
			wantErr:  true,
		},
		{
			name:     "password rejects colon",
			username: "alice",
			password: "pass:word",
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateChpasswdInput(tc.username, tc.password)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateChpasswdInput() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

func TestRequireUserAcceptsSystemAccounts(t *testing.T) {
	orig := lookupUser
	t.Cleanup(func() { lookupUser = orig })
	lookupUser = func(name string) (*osuser.User, error) {
		if name == "syslog" {
			return &osuser.User{Username: name, Uid: "104"}, nil
		}
		return nil, osuser.UnknownUserError(name)
	}

	require.NoError(t, requireUser("syslog"))
	require.ErrorIs(t, requireUser("ghost"), bridgeipc.ErrNotFound)
	require.ErrorContains(t, requireUser("ghost"), "user ghost")
}

func TestModifyUserArgsEmptyGroupsClearsSecondaryGroups(t *testing.T) {
	require.Equal(t, []string{"-G", ""}, modifyUserArgs(apischema.ModifyUserRequest{Groups: []string{}}))
	require.Equal(t, []string{"-G", "a,b"}, modifyUserArgs(apischema.ModifyUserRequest{Groups: []string{"a", "b"}}))
	require.Empty(t, modifyUserArgs(apischema.ModifyUserRequest{}))
}

func TestRootUserRefusalsAreForbidden(t *testing.T) {
	ctx := t.Context()
	require.ErrorIs(t, DeleteUser(ctx, "root"), bridgeipc.ErrForbidden)
	require.ErrorIs(t, ModifyUser(ctx, apischema.ModifyUserRequest{Username: "root"}), bridgeipc.ErrForbidden)
	require.ErrorIs(t, LockUser(ctx, "root"), bridgeipc.ErrForbidden)
}
