package accounts

import (
	"context"
	"errors"
	osuser "os/user"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// accountSeams stands in for the account tools and for the shared-folder
// side of an account change, and records the order of the calls.
type accountSeams struct {
	calls []string
	fail  map[string]error
}

func setupAccountSeams(t *testing.T) *accountSeams {
	t.Helper()
	seams := &accountSeams{fail: map[string]error{}}
	origLookup, origRun, origSet := lookupUser, runAccountTool, setPassword
	origSync, origRemove, origReapply, origEnable := syncSambaPassword, removeSambaUser, reapplyShareAccess, setSambaUserEnabled
	t.Cleanup(func() {
		lookupUser, runAccountTool, setPassword = origLookup, origRun, origSet
		syncSambaPassword, removeSambaUser, reapplyShareAccess, setSambaUserEnabled = origSync, origRemove, origReapply, origEnable
	})
	setSambaUserEnabled = func(_ context.Context, username string, enabled bool) error {
		seams.calls = append(seams.calls, map[bool]string{true: "samba-enable ", false: "samba-disable "}[enabled]+username)
		return seams.fail["samba-enable"]
	}
	lookupUser = func(name string) (*osuser.User, error) { return &osuser.User{Username: name, Uid: "1000"}, nil }
	runAccountTool = func(_ context.Context, name string, args ...string) ([]byte, error) {
		seams.calls = append(seams.calls, name+" "+strings.Join(args, " "))
		return []byte(name + " said no"), seams.fail[name]
	}
	setPassword = func(_ context.Context, username, _ string) error {
		seams.calls = append(seams.calls, "chpasswd "+username)
		return seams.fail["chpasswd"]
	}
	syncSambaPassword = func(_ context.Context, username, _ string, signIn *bool) error {
		state := "kept"
		if signIn != nil {
			state = map[bool]string{true: "on", false: "off"}[*signIn]
		}
		seams.calls = append(seams.calls, "samba "+username+" "+state)
		return seams.fail["samba"]
	}
	removeSambaUser = func(_ context.Context, username string) error {
		seams.calls = append(seams.calls, "samba-remove "+username)
		return seams.fail["samba-remove"]
	}
	reapplyShareAccess = func(context.Context) error {
		seams.calls = append(seams.calls, "reapply")
		return seams.fail["reapply"]
	}
	return seams
}

// The system password is set first, so the one Samba holds is never the
// newer of the two.
func TestChangePasswordAlsoSetsItForSharedFolders(t *testing.T) {
	seams := setupAccountSeams(t)
	on := true
	require.NoError(t, ChangePassword(t.Context(), "alice", "pw", &on))
	require.Equal(t, []string{"chpasswd alice", "samba alice on"}, seams.calls)

	seams.calls = nil
	require.NoError(t, ChangePassword(t.Context(), "alice", "pw", nil))
	require.Equal(t, []string{"chpasswd alice", "samba alice kept"}, seams.calls)
}

func TestChangePasswordSaysWhenOnlyTheSystemPasswordChanged(t *testing.T) {
	seams := setupAccountSeams(t)
	seams.fail["samba"] = errors.New("boom")
	err := ChangePassword(t.Context(), "alice", "pw", nil)
	require.ErrorContains(t, err, "the password of alice was changed, but not the one for shared folders")
	require.ErrorIs(t, err, seams.fail["samba"])

	seams.calls = nil
	seams.fail["chpasswd"] = errors.New("refused")
	require.Error(t, ChangePassword(t.Context(), "alice", "pw", nil))
	require.Equal(t, []string{"chpasswd alice"}, seams.calls, "Samba is not given a password the system refused")
}

func TestCreateUserWithSharedFolderSignIn(t *testing.T) {
	seams := setupAccountSeams(t)
	on := true
	req := apischema.CreateUserRequest{Username: "dave", Password: "pw", Groups: []string{"family"}, SMBSignIn: &on}
	require.NoError(t, CreateUser(t.Context(), req))
	// The new member of a group may be one a shared folder refuses.
	require.Equal(t, []string{"useradd -G family dave", "chpasswd dave", "samba dave on", "reapply"}, seams.calls)

	seams.calls = nil
	req.SMBSignIn = nil
	req.Groups = nil
	require.NoError(t, CreateUser(t.Context(), req))
	require.Equal(t, []string{"useradd dave", "chpasswd dave"}, seams.calls, "a new user is not in Samba's list unless asked")
}

// The account exists by then, and deleting it would delete its home: the
// error says what is left to do.
func TestCreateUserKeepsTheUserWhenSambaFails(t *testing.T) {
	seams := setupAccountSeams(t)
	seams.fail["samba"] = errors.New("boom")
	on := true
	err := CreateUser(t.Context(), apischema.CreateUserRequest{Username: "dave", Password: "pw", SMBSignIn: &on})
	require.ErrorContains(t, err, "user dave was created, but cannot sign in to shared folders yet")
	require.Equal(t, []string{"useradd dave", "chpasswd dave", "samba dave on"}, seams.calls)
}

// Samba's entry goes while the account is still there to look up, and the
// rows of the shared folders go after it, so a later account of the same
// name or ID does not inherit them.
func TestDeleteUserLeavesNothingBehindInSharedFolders(t *testing.T) {
	seams := setupAccountSeams(t)
	require.NoError(t, DeleteUser(t.Context(), "alice"))
	require.Equal(t, []string{"samba-remove alice", "userdel -r alice", "reapply"}, seams.calls)

	seams.calls = nil
	seams.fail["samba-remove"] = errors.New("boom")
	require.Error(t, DeleteUser(t.Context(), "alice"))
	require.Equal(t, []string{"samba-remove alice"}, seams.calls, "the user stays while Samba still lets them in")
}

// A failure to apply the folders' access again does not undo the account
// change that already happened; it is logged.
func TestAccountChangeSurvivesAFailedReapply(t *testing.T) {
	seams := setupAccountSeams(t)
	seams.fail["reapply"] = errors.New("pool offline")
	require.NoError(t, DeleteUser(t.Context(), "alice"))
	groups := []string{"kids"}
	require.NoError(t, ModifyUser(t.Context(), apischema.ModifyUserRequest{Username: "alice", Groups: groups}))
	require.Equal(t, "reapply", seams.calls[len(seams.calls)-1])
}

// Samba checks its own password and not the system account's lock, so a
// lock reaches it too: first, so a lock that fails half-way leaves less
// access, not more; an unlock reaches it last for the same reason.
func TestLockAndUnlockReachSharedFolderSignIn(t *testing.T) {
	seams := setupAccountSeams(t)
	require.NoError(t, LockUser(t.Context(), "alice"))
	require.NoError(t, UnlockUser(t.Context(), "alice"))
	require.Equal(t, []string{"samba-disable alice", "passwd -l alice", "passwd -u alice", "samba-enable alice"}, seams.calls)

	// A Samba that cannot be changed does not keep the account from being
	// locked; the error says what is left.
	seams.calls = nil
	seams.fail["samba-enable"] = errors.New("boom")
	err := LockUser(t.Context(), "alice")
	require.ErrorContains(t, err, "alice was locked, but may still open shared folders")
	require.ErrorContains(t, err, "boom")
	require.Equal(t, []string{"samba-disable alice", "passwd -l alice"}, seams.calls)
	seams.calls = nil
	require.ErrorContains(t, UnlockUser(t.Context(), "alice"), "alice was unlocked, but cannot open shared folders yet")
	require.Equal(t, []string{"passwd -u alice", "samba-enable alice"}, seams.calls)
}

func TestModifyUserReappliesOnlyWhenGroupsChange(t *testing.T) {
	seams := setupAccountSeams(t)
	shell := "/bin/zsh"
	require.NoError(t, ModifyUser(t.Context(), apischema.ModifyUserRequest{Username: "alice", Shell: &shell}))
	require.Equal(t, []string{"usermod -s /bin/zsh alice"}, seams.calls)
}
