package accounts

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	godbus "github.com/godbus/dbus/v5"
	"github.com/shirou/gopsutil/v4/process"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/control"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/loginhistory"
	"github.com/mordilloSan/LinuxIO/backend/common/dbusclient"
)

// ListUserLogins returns the most recent login events for a user.
func ListUserLogins(ctx context.Context, username string, limit int) ([]apischema.AccountUserLogin, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, fmt.Errorf("%w: username", bridgeipc.ErrInvalidArgs)
	}
	if limit <= 0 {
		limit = 24
	}

	logins, err := loginhistory.FetchRecentEvents(ctx, username, limit)
	if err != nil {
		return nil, err
	}

	result := make([]apischema.AccountUserLogin, 0, len(logins))
	for _, login := range logins {
		startedAt := ""
		if !login.StartedAt.IsZero() {
			startedAt = login.StartedAt.Format(time.RFC3339)
		}
		result = append(result, apischema.AccountUserLogin{
			ID:        login.ID,
			Username:  login.Username,
			Terminal:  login.Terminal,
			Source:    login.Source,
			Time:      login.Time,
			StartedAt: startedAt,
			Status:    login.Status,
		})
	}
	return result, nil
}

// GetUserDetails returns security, runtime, and filesystem health for a user.
func GetUserDetails(ctx context.Context, username string) (apischema.AccountUserDetails, error) {
	user, err := GetUser(ctx, username)
	if err != nil {
		return apischema.AccountUserDetails{}, err
	}

	allGroups := allGroupsForUser(*user)
	details := apischema.AccountUserDetails{
		Username:       user.Username,
		ActiveSessions: getActiveSessions(ctx, user.Username),
		Password:       getPasswordState(user.Username),
		Admin:          getAdminAccess(*user, allGroups),
		Home:           getHomeHealth(ctx, *user),
		SSH:            getSSHAccess(*user),
		Processes:      getProcessSummary(ctx, user.Username),
	}

	failedAttempts, err := loginhistory.FetchFailedAttempts(ctx, user.Username, time.Time{})
	if err != nil {
		details.FailedLoginAttemptsError = err.Error()
	} else {
		details.FailedLoginAttempts = failedAttempts
		details.FailedLoginAttemptsAvailable = true
	}

	return details, nil
}

func allGroupsForUser(user apischema.AccountUser) []string {
	groups := []string{user.PrimaryGroup}
	for _, group := range user.Groups {
		if group != "" && !slices.Contains(groups, group) {
			groups = append(groups, group)
		}
	}
	return groups
}

func getActiveSessions(ctx context.Context, username string) []apischema.AccountActiveSession {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	sessions := make([]apischema.AccountActiveSession, 0)
	err := dbusclient.Login1Manager.UseSession(ctx, func(session dbusclient.SystemSession) error {
		var listed [][]any
		if err := session.CallStore(dbusclient.LoginMgrIface+".ListSessions", dbusclient.CallPolicy{}, nil, &listed); err != nil {
			return err
		}
		for _, entry := range listed {
			// ListSessions returns (id, uid, user, seat, path).
			if len(entry) < 5 {
				continue
			}
			if name, _ := entry[2].(string); name != username {
				continue
			}
			path, ok := entry[4].(godbus.ObjectPath)
			if !ok {
				continue
			}
			var props map[string]godbus.Variant
			if err := session.ObjectAt(path).CallWithContext(session.Context(), "org.freedesktop.DBus.Properties.GetAll", 0, loginSessionIface).Store(&props); err != nil {
				return err
			}
			if s, ok := sessionFromLogind(props, time.Now()); ok {
				sessions = append(sessions, s)
			}
		}
		return nil
	})
	if err != nil {
		slog.Warn("login1 ListSessions failed", "user", username, "error", err)
	}
	return sessions
}

const loginSessionIface = "org.freedesktop.login1.Session"

// sessionFromLogind maps login1 Session properties onto the shape the UI
// already renders: idle is "." when active, "old" past a day, else HH:MM.
func sessionFromLogind(props map[string]godbus.Variant, now time.Time) (apischema.AccountActiveSession, bool) {
	str := func(k string) string { v, _ := props[k].Value().(string); return v }
	if class := str("Class"); class != "user" && !strings.HasPrefix(class, "user-") {
		return apischema.AccountActiveSession{}, false
	}
	terminal := str("TTY")
	if terminal == "" {
		terminal = str("Service")
	}
	session := apischema.AccountActiveSession{
		SessionID: str("Id"),
		Terminal:  terminal,
		Source:    str("RemoteHost"),
		Idle:      ".",
	}
	if usec, _ := props["Timestamp"].Value().(uint64); usec > 0 {
		session.StartedAt = time.UnixMicro(int64(usec)).Format("2006-01-02 15:04")
	}
	if leader, _ := props["Leader"].Value().(uint32); leader > 0 {
		pid := int(leader)
		session.PID = &pid
	}
	if idle, _ := props["IdleHint"].Value().(bool); idle {
		since, _ := props["IdleSinceHint"].Value().(uint64)
		switch d := now.Sub(time.UnixMicro(int64(since))); {
		case since == 0:
			session.Idle = "?"
		case d >= 24*time.Hour:
			session.Idle = "old"
		default:
			session.Idle = fmt.Sprintf("%02d:%02d", int(d.Hours()), int(d.Minutes())%60)
		}
	}
	return session, true
}

// TerminateSession ends an active login session through systemd-logind, which
// cleans up its scopes and cgroups.
func TerminateSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("%w: sessionId", bridgeipc.ErrInvalidArgs)
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := control.Logoff(cmdCtx, sessionID); err != nil {
		return fmt.Errorf("terminate session %q: %w", sessionID, err)
	}
	return nil
}

func getPasswordState(username string) apischema.AccountPasswordState {
	file, err := os.Open(shadowFile)
	if err != nil {
		return apischema.AccountPasswordState{Error: err.Error()}
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		parts := strings.Split(scanner.Text(), ":")
		if len(parts) < 9 || parts[0] != username {
			continue
		}
		return parsePasswordState(parts)
	}
	if err := scanner.Err(); err != nil {
		return apischema.AccountPasswordState{Error: err.Error()}
	}
	return apischema.AccountPasswordState{Error: "shadow entry not found"}
}

func parsePasswordState(parts []string) apischema.AccountPasswordState {
	hash := parts[1]
	locked := strings.HasPrefix(hash, "!") || strings.HasPrefix(hash, "*")
	hashWithoutLock := strings.TrimLeft(hash, "!")
	hasPassword := hashWithoutLock != "" &&
		hashWithoutLock != "*" &&
		hashWithoutLock != "!!" &&
		hashWithoutLock != "x"

	lastChangedDays := parseShadowInt(parts[2])
	maxDays := intPtrIfValid(parseShadowInt(parts[4]))
	warningDays := intPtrIfValid(parseShadowInt(parts[5]))

	state := apischema.AccountPasswordState{
		Locked:      locked,
		HasPassword: hasPassword,
		MaxDays:     maxDays,
		WarningDays: warningDays,
	}

	if lastChangedDays > 0 {
		lastChanged := daysSinceEpoch(lastChangedDays)
		state.LastChanged = formatDate(lastChanged)
		if maxDays != nil && *maxDays > 0 && *maxDays < 99999 {
			expires := lastChanged.AddDate(0, 0, *maxDays)
			state.Expires = formatDate(expires)
			days := int(time.Until(expires).Hours() / 24)
			state.ExpiresInDays = &days
		}
	}

	return state
}

func getAdminAccess(user apischema.AccountUser, groups []string) apischema.AccountAdminAccess {
	privilegedGroups := map[string]bool{
		"admin":   true,
		"docker":  true,
		"libvirt": true,
		"lxd":     true,
		"root":    true,
		"sudo":    true,
		"wheel":   true,
	}

	adminGroups := make([]string, 0)
	if user.UID == 0 {
		adminGroups = append(adminGroups, "root")
	}
	for _, group := range groups {
		if privilegedGroups[group] && !slices.Contains(adminGroups, group) {
			adminGroups = append(adminGroups, group)
		}
	}

	return apischema.AccountAdminAccess{
		IsAdmin: len(adminGroups) > 0,
		Groups:  adminGroups,
	}
}

func getHomeHealth(ctx context.Context, user apischema.AccountUser) apischema.AccountHomeHealth {
	if user.HomeDir == "" {
		return apischema.AccountHomeHealth{Error: "home directory is not configured"}
	}

	info, err := os.Stat(user.HomeDir)
	if errors.Is(err, os.ErrNotExist) {
		return apischema.AccountHomeHealth{Exists: false}
	}
	if err != nil {
		return apischema.AccountHomeHealth{Error: err.Error()}
	}

	health := apischema.AccountHomeHealth{
		Exists:      true,
		IsDirectory: info.IsDir(),
		Mode:        formatFileMode(info.Mode()),
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		_, gidToGroup := groupIndex(ctx)
		ownerUID, groupGID := int(stat.Uid), int(stat.Gid)
		health.OwnerUID = &ownerUID
		health.GroupGID = &groupGID
		health.GroupName = gidToGroup[groupGID]
		health.OwnerMatches = ownerUID == user.UID
	}
	return health
}

func getSSHAccess(user apischema.AccountUser) apischema.AccountSSHAccess {
	if user.HomeDir == "" {
		return apischema.AccountSSHAccess{Error: "home directory is not configured"}
	}

	sshDir := filepath.Join(user.HomeDir, ".ssh")
	authKeysPath := filepath.Join(sshDir, "authorized_keys")
	access := apischema.AccountSSHAccess{}

	sshInfo, err := os.Stat(sshDir)
	if errors.Is(err, os.ErrNotExist) {
		return access
	}
	if err != nil {
		access.Error = err.Error()
		return access
	}
	access.SSHDirExists = true
	access.SSHDirMode = formatFileMode(sshInfo.Mode())

	keysInfo, err := os.Stat(authKeysPath)
	if errors.Is(err, os.ErrNotExist) {
		return access
	}
	if err != nil {
		access.Error = err.Error()
		return access
	}
	access.AuthorizedKeysExists = true
	access.AuthorizedKeysMode = formatFileMode(keysInfo.Mode())
	if stat, ok := keysInfo.Sys().(*syscall.Stat_t); ok {
		access.AuthorizedKeysOwnerMatches = int(stat.Uid) == user.UID
	}

	count, err := countAuthorizedKeys(authKeysPath)
	if err != nil {
		access.Error = err.Error()
		return access
	}
	access.AuthorizedKeysCount = count
	return access
}

func getProcessSummary(ctx context.Context, username string) apischema.AccountProcessSummary {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	summary := apischema.AccountProcessSummary{Top: []apischema.AccountUserProcess{}}
	account, err := user.Lookup(username)
	if err != nil {
		summary.Error = err.Error()
		return summary
	}
	uid, err := strconv.ParseUint(account.Uid, 10, 32)
	if err != nil {
		summary.Error = err.Error()
		return summary
	}
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		summary.Error = err.Error()
		return summary
	}

	for _, proc := range procs {
		// Like ps -u, match on the effective UID; processes that exit mid-scan are skipped.
		uids, err := proc.UidsWithContext(ctx)
		if err != nil || len(uids) < 2 || uint64(uids[1]) != uid {
			continue
		}
		name, _ := proc.NameWithContext(ctx)
		cpu, _ := proc.CPUPercentWithContext(ctx)
		mem, _ := proc.MemoryPercentWithContext(ctx)
		summary.Count++
		summary.Top = append(summary.Top, apischema.AccountUserProcess{
			PID:     int(proc.Pid),
			Command: name,
			CPU:     math.Round(cpu*10) / 10,
			Memory:  math.Round(float64(mem)*10) / 10,
		})
	}
	slices.SortFunc(summary.Top, func(a, b apischema.AccountUserProcess) int {
		return cmp.Compare(b.CPU, a.CPU)
	})
	return summary
}

func countAuthorizedKeys(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	count := 0
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		count++
	}
	return count, scanner.Err()
}

func parseShadowInt(value string) int {
	if value == "" {
		return -1
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return n
}

func intPtrIfValid(value int) *int {
	if value < 0 {
		return nil
	}
	return &value
}

func daysSinceEpoch(days int) time.Time {
	return time.Unix(0, 0).UTC().AddDate(0, 0, days)
}

func formatDate(value time.Time) string {
	return value.Format("2006-01-02")
}

func formatFileMode(mode os.FileMode) string {
	return fmt.Sprintf("%04o", mode.Perm())
}
