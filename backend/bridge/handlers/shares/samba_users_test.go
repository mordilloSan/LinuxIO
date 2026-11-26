package shares

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// What "pdbedit -L -v" prints for two users, as Samba 4 writes it.
const pdbeditListing = `---------------
Unix username:        alice
NT username:          
Account Flags:        [U          ]
User SID:             S-1-5-21-1-2-3-1000
Primary Group SID:    S-1-5-21-1-2-3-513
Full Name:            Alice
Home Directory:       \\server\alice
Logon time:           0
Password last set:    Wed, 25 Nov 2026 10:00:00 UTC
Password can change:  Wed, 25 Nov 2026 10:00:00 UTC
Password must change: never
---------------
Unix username:        bob
NT username:          
Account Flags:        [U          ]
Password last set:    Mon, 02 Nov 2026 08:30:00 UTC
Password must change: never
`

// sambaTools stands in for smbpasswd and pdbedit, and records each call.
type sambaTools struct {
	calls   []string
	listing string
}

func setupSambaTools(t *testing.T) *sambaTools {
	t.Helper()
	setupFolders(t)
	tools := &sambaTools{listing: pdbeditListing}
	origSet, origRemove, origList, origShadow, origEnable := smbpasswdSet, smbpasswdRemove, pdbeditList, shadowPath, smbpasswdEnable
	t.Cleanup(func() {
		smbpasswdSet, smbpasswdRemove, pdbeditList, shadowPath, smbpasswdEnable = origSet, origRemove, origList, origShadow, origEnable
	})
	smbpasswdEnable = func(_ context.Context, username string, enabled bool) ([]byte, error) {
		tools.calls = append(tools.calls, map[bool]string{true: "enable ", false: "disable "}[enabled]+username)
		return nil, nil
	}
	smbpasswdSet = func(_ context.Context, username, password string) ([]byte, error) {
		tools.calls = append(tools.calls, "set "+username+" "+password)
		return nil, nil
	}
	smbpasswdRemove = func(_ context.Context, username string) ([]byte, error) {
		tools.calls = append(tools.calls, "remove "+username)
		return nil, nil
	}
	pdbeditList = func(context.Context) ([]byte, bool, error) { return []byte(tools.listing), true, nil }
	// 20782 days after 1970 is 2026-11-25; bob's system password is of the
	// 10th, eight days after the one Samba holds.
	shadowPath = filepath.Join(t.TempDir(), "shadow")
	if err := os.WriteFile(shadowPath, []byte("root:*:19000:0:99999:7:::\nalice:$y$hash:20782:0:99999:7:::\nbob:$y$hash:20767:0:99999:7:::\ncarol:$y$hash::0:99999:7:::\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return tools
}

func TestListSambaUsersFlagsAPasswordChangedOutside(t *testing.T) {
	setupSambaTools(t)
	users, err := ListSambaUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []apischema.SambaUser{{Username: "alice"}, {Username: "bob", PasswordStale: true}}
	if !slices.Equal(users, want) {
		t.Errorf("ListSambaUsers() = %+v, want %+v", users, want)
	}
}

// A date Samba prints in another form, or none, is no reason to flag.
func TestListSambaUsersWithoutADate(t *testing.T) {
	tools := setupSambaTools(t)
	tools.listing = "---------------\nUnix username:        bob\nPassword last set:    0\n---------------\nUnix username:        carol\n"
	users, err := ListSambaUsers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []apischema.SambaUser{{Username: "bob"}, {Username: "carol"}}
	if !slices.Equal(users, want) {
		t.Errorf("ListSambaUsers() = %+v, want %+v", users, want)
	}
}

func TestListSambaUsersWithoutSamba(t *testing.T) {
	setupSambaTools(t)
	pdbeditList = func(context.Context) ([]byte, bool, error) { return nil, false, nil }
	users, err := ListSambaUsers(context.Background())
	if err != nil || users == nil || len(users) != 0 {
		t.Errorf("ListSambaUsers() = %v, %v, want an empty list", users, err)
	}
}

func TestSetSambaPassword(t *testing.T) {
	tools := setupSambaTools(t)
	ctx := context.Background()
	if err := SetSambaPassword(ctx, "alice", "s3cret word"); err != nil {
		t.Fatalf("SetSambaPassword() = %v", err)
	}
	// chpasswd, which sets the system password first, unlocks the account,
	// so the Samba entry is enabled to match.
	if !slices.Equal(tools.calls, []string{"set alice s3cret word", "enable alice"}) {
		t.Errorf("calls = %v", tools.calls)
	}
	tools.calls = nil
	for name, tc := range map[string]struct{ username, password string }{
		"a system account":         {"root", "x"},
		"a user that is not there": {"dave", "x"},
		"an option as a name":      {"-a", "x"},
		"no password":              {"alice", ""},
		"a second line":            {"alice", "one\ntwo"},
	} {
		if err := SetSambaPassword(ctx, tc.username, tc.password); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
			t.Errorf("%s = %v, want ErrInvalidArgs", name, err)
		}
	}
	if len(tools.calls) != 0 {
		t.Errorf("a refused password reached smbpasswd: %v", tools.calls)
	}
}

func TestRemoveSambaUser(t *testing.T) {
	tools := setupSambaTools(t)
	ctx := context.Background()
	if err := RemoveSambaUser(ctx, "alice"); err != nil {
		t.Fatalf("RemoveSambaUser() = %v", err)
	}
	// carol is not in Samba's list: nothing to remove, and no error.
	if err := RemoveSambaUser(ctx, "carol"); err != nil {
		t.Fatalf("RemoveSambaUser() of a user not listed = %v", err)
	}
	if !slices.Equal(tools.calls, []string{"remove alice"}) {
		t.Errorf("calls = %v", tools.calls)
	}
}

// Locking an account in LinuxIO locks its Samba entry too: Samba checks its
// own password and never looks at the system one's lock.
func TestSetSambaUserEnabled(t *testing.T) {
	tools := setupSambaTools(t)
	ctx := context.Background()
	if err := SetSambaUserEnabled(ctx, "alice", false); err != nil {
		t.Fatalf("disable = %v", err)
	}
	if err := SetSambaUserEnabled(ctx, "alice", true); err != nil {
		t.Fatalf("enable = %v", err)
	}
	// carol is not in Samba's list: nothing to do.
	if err := SetSambaUserEnabled(ctx, "carol", false); err != nil {
		t.Fatalf("a user not listed = %v", err)
	}
	if !slices.Equal(tools.calls, []string{"disable alice", "enable alice"}) {
		t.Errorf("calls = %v", tools.calls)
	}
}

// A name Samba lists that could be read as an option is not handed to
// smbpasswd.
func TestRemoveSambaUserRefusesANameLikeAnOption(t *testing.T) {
	tools := setupSambaTools(t)
	tools.listing = "Unix username:        -x\n"
	if err := RemoveSambaUser(context.Background(), "-x"); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Fatalf("RemoveSambaUser() = %v, want ErrInvalidArgs", err)
	}
	if err := SetSambaUserEnabled(context.Background(), "-x", false); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
		t.Fatalf("SetSambaUserEnabled() = %v, want ErrInvalidArgs", err)
	}
	if len(tools.calls) != 0 {
		t.Errorf("calls = %v", tools.calls)
	}
}

func TestSyncSambaPassword(t *testing.T) {
	on, off := true, false
	tests := []struct {
		name     string
		username string
		signIn   *bool
		want     []string
	}{
		{"switched on", "carol", &on, []string{"set carol pw", "enable carol"}},
		{"switched off", "alice", &off, []string{"remove alice"}},
		{"switched off for a user not listed", "carol", &off, nil},
		{"not said, already on: kept in step", "alice", nil, []string{"set alice pw", "enable alice"}},
		{"not said, off: left off", "carol", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tools := setupSambaTools(t)
			if err := SyncSambaPassword(context.Background(), tt.username, "pw", tt.signIn); err != nil {
				t.Fatalf("SyncSambaPassword() = %v", err)
			}
			if !slices.Equal(tools.calls, tt.want) {
				t.Errorf("calls = %v, want %v", tools.calls, tt.want)
			}
		})
	}
}
