package shares

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

// Signing in to a shared folder from Windows or a Mac (docs/shared-folders.md,
// "Signing in"): Samba cannot check a password through PAM, since the client
// never sends it, so it keeps a hash of its own for each user in its user
// list. A password set in LinuxIO for a user with sign-in switched on is
// written there too, at the moment it is set, the only moment LinuxIO has
// it.

// shadowPath is a var so tests can point it at their own file.
var shadowPath = "/etc/shadow"

// The Samba tools, as vars so tests can stand in for them.
var (
	// smbpasswdSet adds username to Samba's user list with password, or
	// changes the password of one already there: "smbpasswd -a" does both.
	// The password goes over stdin, twice, never on the command line.
	smbpasswdSet = func(ctx context.Context, username, password string) ([]byte, error) {
		args := []string{"-s", "-a", username}
		cmd := exec.CommandContext(ctx, "smbpasswd", args...)
		cmd.Stdin = strings.NewReader(password + "\n" + password + "\n")
		output, err := cmd.CombinedOutput()
		return output, hostcmd.OutputError("smbpasswd", args, output, err)
	}
	smbpasswdRemove = func(ctx context.Context, username string) ([]byte, error) {
		args := []string{"-x", username}
		output, err := exec.CommandContext(ctx, "smbpasswd", args...).CombinedOutput()
		return output, hostcmd.OutputError("smbpasswd", args, output, err)
	}
	// smbpasswdEnable enables a user's entry in Samba's list, or disables
	// it, which keeps the user out whatever password they give.
	smbpasswdEnable = func(ctx context.Context, username string, enabled bool) ([]byte, error) {
		flag := "-d"
		if enabled {
			flag = "-e"
		}
		args := []string{flag, username}
		output, err := exec.CommandContext(ctx, "smbpasswd", args...).CombinedOutput()
		return output, hostcmd.OutputError("smbpasswd", args, output, err)
	}
	// pdbeditList returns Samba's user list in its long form, and false
	// when Samba's tools are not installed. The dates are asked for in
	// English and in UTC, whatever the bridge runs in.
	pdbeditList = func(ctx context.Context) ([]byte, bool, error) {
		if _, err := hostcmd.LookPath("pdbedit"); err != nil {
			return nil, false, nil
		}
		args := []string{"-L", "-v"}
		cmd := exec.CommandContext(ctx, "pdbedit", args...)
		cmd.Env = append(os.Environ(), "LC_ALL=C", "TZ=UTC")
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		return output, true, hostcmd.OutputError("pdbedit", args, stderr.Bytes(), err)
	}
)

// pdbeditTime is how pdbedit prints a time.
const pdbeditTime = "Mon, 02 Jan 2006 15:04:05 MST"

const secondsPerDay = 24 * 60 * 60

// sambaPasswordDays reads pdbedit's long listing: each user in Samba's list
// with the day its password was last set, counted from 1970 as /etc/shadow
// counts, or -1 when the listing gives no date.
func sambaPasswordDays(listing []byte) map[string]int64 {
	days := map[string]int64{}
	var username string
	scanner := bufio.NewScanner(bytes.NewReader(listing))
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch key {
		case "Unix username":
			username = value
			if username != "" {
				days[username] = -1
			}
		case "Password last set":
			if set, err := time.Parse(pdbeditTime, value); err == nil && username != "" {
				days[username] = set.Unix() / secondsPerDay
			}
		}
	}
	return days
}

// shadowPasswordDays returns the day each user's system password last
// changed, as /etc/shadow holds it. Only the name and that day are read.
func shadowPasswordDays() (map[string]int64, error) {
	days := map[string]int64{}
	err := eachColonLine(shadowPath, func(fields []string) {
		if len(fields) < 3 {
			return
		}
		if day, err := strconv.ParseInt(fields[2], 10, 64); err == nil {
			days[fields[0]] = day
		}
	})
	return days, err
}

// ListSambaUsers returns who can sign in to shared folders from Windows and
// a Mac. A user whose system password is newer than the one Samba holds had
// it changed outside LinuxIO (passwd in a terminal), so the two differ; the
// days are all there is to compare, so a change on the day LinuxIO set it
// goes unseen. Without Samba's tools nobody can.
func ListSambaUsers(ctx context.Context) ([]apischema.SambaUser, error) {
	listing, installed, err := pdbeditList(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Samba users: %w", err)
	}
	if !installed {
		return []apischema.SambaUser{}, nil
	}
	sambaDays := sambaPasswordDays(listing)
	shadowDays, err := shadowPasswordDays()
	if err != nil {
		return nil, err
	}
	users := make([]apischema.SambaUser, 0, len(sambaDays))
	for username, sambaDay := range sambaDays {
		shadowDay, known := shadowDays[username]
		users = append(users, apischema.SambaUser{
			Username:      username,
			PasswordStale: known && sambaDay >= 0 && shadowDay > sambaDay,
		})
	}
	slices.SortFunc(users, func(a, b apischema.SambaUser) int { return strings.Compare(a.Username, b.Username) })
	return users, nil
}

// SetSambaPassword lets a regular user sign in to shared folders from
// Windows and a Mac with password, adding the user to Samba's list or
// changing the password it holds.
func SetSambaPassword(ctx context.Context, username, password string) error {
	accounts, err := readLocalAccounts()
	if err != nil {
		return err
	}
	// The name is an argument of smbpasswd: one that could be read as an
	// option never gets there.
	user, ok := accounts.user(username)
	if !ok || !regularID(user.uid) || !validAccountName.MatchString(username) {
		return fmt.Errorf("%w: %s is not a regular local user", bridgeipc.ErrInvalidArgs, username)
	}
	// smbpasswd reads the password as one line.
	if password == "" || strings.ContainsAny(password, "\r\n") {
		return fmt.Errorf("%w: a password for shared folders is one line", bridgeipc.ErrInvalidArgs)
	}
	if _, err := smbpasswdSet(ctx, username, password); err != nil {
		return fmt.Errorf("set the Samba password of %s: %w", username, err)
	}
	// LinuxIO sets the system password first, with chpasswd, which unlocks
	// a locked account; an entry LockUser disabled is enabled to match.
	if _, err := smbpasswdEnable(ctx, username, true); err != nil {
		return fmt.Errorf("enable the Samba user %s: %w", username, err)
	}
	slog.Info("Samba password set", "user", username)
	return nil
}

// listedSambaUser says username is in Samba's list. A listed name that could
// be read as an option of smbpasswd is refused: LinuxIO never put it there.
func listedSambaUser(ctx context.Context, username string) (bool, error) {
	users, err := ListSambaUsers(ctx)
	if err != nil {
		return false, err
	}
	if !slices.ContainsFunc(users, func(u apischema.SambaUser) bool { return u.Username == username }) {
		return false, nil
	}
	if !validAccountName.MatchString(username) {
		return false, fmt.Errorf("%w: LinuxIO does not change the Samba user %q; use smbpasswd", bridgeipc.ErrInvalidArgs, username)
	}
	return true, nil
}

// SetSambaUserEnabled disables a user's entry in Samba's list, so a locked
// account cannot sign in to shared folders either (Samba checks its own
// password and never the system one's lock), or enables it again. A user
// who is not in the list is left alone.
func SetSambaUserEnabled(ctx context.Context, username string, enabled bool) error {
	listed, err := listedSambaUser(ctx, username)
	if err != nil || !listed {
		return err
	}
	if _, err := smbpasswdEnable(ctx, username, enabled); err != nil {
		return fmt.Errorf("change the Samba user %s: %w", username, err)
	}
	if enabled {
		slog.Info("Samba user enabled", "user", username)
	} else {
		slog.Info("Samba user disabled", "user", username)
	}
	return nil
}

// RemoveSambaUser takes a user out of Samba's list, so they can no longer
// sign in to shared folders from Windows and a Mac. A user who is not in it
// is left alone.
func RemoveSambaUser(ctx context.Context, username string) error {
	listed, err := listedSambaUser(ctx, username)
	if err != nil || !listed {
		return err
	}
	if _, err := smbpasswdRemove(ctx, username); err != nil {
		return fmt.Errorf("remove %s from the Samba users: %w", username, err)
	}
	slog.Info("Samba user removed", "user", username)
	return nil
}

// SyncSambaPassword follows a password LinuxIO just set for username: with
// signIn true it is written to Samba, with false the user leaves Samba's
// list, and with nil a user already in the list gets the new password, so
// the two never drift apart through LinuxIO.
func SyncSambaPassword(ctx context.Context, username, password string, signIn *bool) error {
	switch {
	case signIn != nil && *signIn:
		return SetSambaPassword(ctx, username, password)
	case signIn != nil:
		return RemoveSambaUser(ctx, username)
	}
	users, err := ListSambaUsers(ctx)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(users, func(u apischema.SambaUser) bool { return u.Username == username }) {
		return nil
	}
	return SetSambaPassword(ctx, username, password)
}
