package accounts

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	logindbus "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/loginhistory"
)

const (
	passwdFile = "/etc/passwd"
	shadowFile = "/etc/shadow"
	shellsFile = "/etc/shells"
	systemUID  = 1000
)

// ListUsers returns login users (root + users with UID >= 1000 and valid shell)
func ListUsers() ([]User, error) {
	users := []User{}

	file, err := os.Open(passwdFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open passwd file: %w", err)
	}
	defer file.Close()

	lockedUsers := getLockedUsers()
	userGroups, gidToGroup := parseGroupFile()
	lastLogins := getLastLogins()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if user, ok := parsePasswdLine(scanner.Text(), lockedUsers, userGroups, gidToGroup, lastLogins); ok {
			users = append(users, user)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading passwd file: %w", err)
	}

	return users, nil
}

func parsePasswdLine(
	line string,
	lockedUsers map[string]bool,
	userGroups map[string][]string,
	gidToGroup map[int]string,
	lastLogins map[string]string,
) (User, bool) {
	if line == "" || strings.HasPrefix(line, "#") {
		return User{}, false
	}

	parts := strings.Split(line, ":")
	if len(parts) < 7 {
		return User{}, false
	}

	uid, err := strconv.Atoi(parts[2])
	if err != nil {
		return User{}, false
	}

	gid, err := strconv.Atoi(parts[3])
	if err != nil {
		return User{}, false
	}

	shell := parts[6]
	if uid != 0 && uid < systemUID {
		return User{}, false
	}
	if uid != 0 && isNonLoginShell(shell) {
		return User{}, false
	}

	username := parts[0]
	primaryGroup := gidToGroup[gid]
	if primaryGroup == "" {
		primaryGroup = strconv.Itoa(gid)
	}

	lastLogin := lastLogins[username]
	if lastLogin == "" {
		lastLogin = "Never"
	}

	return User{
		Username:     username,
		UID:          uid,
		GID:          gid,
		Gecos:        parts[4],
		HomeDir:      parts[5],
		Shell:        shell,
		PrimaryGroup: primaryGroup,
		IsSystem:     uid < systemUID,
		IsLocked:     lockedUsers[username],
		Groups:       userGroups[username],
		LastLogin:    lastLogin,
	}, true
}

// isNonLoginShell checks if a shell prevents interactive login
func isNonLoginShell(shell string) bool {
	nonLoginShells := []string{
		"/usr/sbin/nologin",
		"/sbin/nologin",
		"/bin/false",
		"/usr/bin/false",
	}
	return slices.Contains(nonLoginShells, shell)
}

// GetUser returns a single user by username
func GetUser(username string) (*User, error) {
	users, err := ListUsers()
	if err != nil {
		return nil, err
	}

	for _, user := range users {
		if user.Username == username {
			return &user, nil
		}
	}

	return nil, fmt.Errorf("user not found: %s", username)
}

// ListUserLogins returns the most recent login events for a user.
func ListUserLogins(ctx context.Context, username string, limit int) ([]UserLogin, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if limit <= 0 {
		limit = 24
	}

	logins, err := loginhistory.FetchRecentEvents(ctx, username, limit)
	if err != nil {
		return nil, err
	}

	result := make([]UserLogin, 0, len(logins))
	for _, login := range logins {
		startedAt := ""
		if !login.StartedAt.IsZero() {
			startedAt = login.StartedAt.Format(time.RFC3339)
		}
		result = append(result, UserLogin{
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
func GetUserDetails(ctx context.Context, username string) (UserDetails, error) {
	user, err := GetUser(username)
	if err != nil {
		return UserDetails{}, err
	}

	allGroups := allGroupsForUser(*user)
	details := UserDetails{
		Username:       user.Username,
		ActiveSessions: getActiveSessions(ctx, user.Username),
		Password:       getPasswordState(user.Username),
		Admin:          getAdminAccess(*user, allGroups),
		Home:           getHomeHealth(*user),
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

func allGroupsForUser(user User) []string {
	groups := []string{user.PrimaryGroup}
	for _, group := range user.Groups {
		if group != "" && !slices.Contains(groups, group) {
			groups = append(groups, group)
		}
	}
	return groups
}

func getActiveSessions(ctx context.Context, username string) []UserActiveSession {
	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	output, err := exec.CommandContext(cmdCtx, "who", "-u").Output()
	if err != nil {
		return []UserActiveSession{}
	}

	sessions := make([]UserActiveSession, 0)
	for line := range strings.SplitSeq(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] != username {
			continue
		}

		session := UserActiveSession{
			Terminal:  fields[1],
			StartedAt: fields[2] + " " + fields[3],
		}
		if len(fields) >= 5 {
			session.Idle = fields[4]
		}
		if len(fields) >= 6 {
			session.PID, _ = strconv.Atoi(fields[5])
		}
		if len(fields) >= 7 {
			session.Source = strings.Trim(strings.Join(fields[6:], " "), "()")
		}
		if session.PID > 0 {
			session.SessionID = logindSessionFromPID(session.PID)
		}
		sessions = append(sessions, session)
	}
	return sessions
}

var logindSessionRegex = regexp.MustCompile(`session-([^.]+)\.scope`)

// logindSessionFromPID returns the systemd-logind session ID for a PID by
// reading /proc/<pid>/cgroup. Returns "" if no session scope is found
// (e.g. processes outside logind, or kernel-managed sessions).
func logindSessionFromPID(pid int) string {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return ""
	}
	match := logindSessionRegex.FindStringSubmatch(string(data))
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

// TerminateSession ends an active login session. Prefers systemd-logind when a
// systemd session ID is provided (cleans up scopes/cgroups), falls back to
// kill -HUP on the session leader PID.
func TerminateSession(ctx context.Context, sessionID string, pid int) error {
	if sessionID == "" && pid <= 0 {
		return errors.New("session identifier required")
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if sessionID != "" {
		if err := logindbus.TerminateLogin1Session(cmdCtx, sessionID); err != nil {
			slog.Warn("login1 TerminateSession failed, falling back to kill",
				"sessionID", sessionID, "pid", pid, "err", err)
		} else {
			return nil
		}
	}

	if pid <= 0 {
		return fmt.Errorf("failed to terminate session %q and no PID available", sessionID)
	}
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return fmt.Errorf("failed to send SIGHUP to pid %d: %w", pid, err)
	}
	return nil
}

func getPasswordState(username string) UserPasswordState {
	file, err := os.Open(shadowFile)
	if err != nil {
		return UserPasswordState{Error: err.Error()}
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
		return UserPasswordState{Error: err.Error()}
	}
	return UserPasswordState{Error: "shadow entry not found"}
}

func parsePasswordState(parts []string) UserPasswordState {
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

	state := UserPasswordState{
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

func getAdminAccess(user User, groups []string) UserAdminAccess {
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

	return UserAdminAccess{
		IsAdmin: len(adminGroups) > 0,
		Groups:  adminGroups,
	}
}

func getHomeHealth(user User) UserHomeHealth {
	if user.HomeDir == "" {
		return UserHomeHealth{Error: "home directory is not configured"}
	}

	info, err := os.Stat(user.HomeDir)
	if errors.Is(err, os.ErrNotExist) {
		return UserHomeHealth{Exists: false}
	}
	if err != nil {
		return UserHomeHealth{Error: err.Error()}
	}

	health := UserHomeHealth{
		Exists:      true,
		IsDirectory: info.IsDir(),
		Mode:        formatFileMode(info.Mode()),
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		_, gidToGroup := parseGroupFile()
		health.OwnerUID = int(stat.Uid)
		health.GroupGID = int(stat.Gid)
		health.GroupName = gidToGroup[health.GroupGID]
		health.OwnerMatches = health.OwnerUID == user.UID
	}
	return health
}

func getSSHAccess(user User) UserSSHAccess {
	if user.HomeDir == "" {
		return UserSSHAccess{Error: "home directory is not configured"}
	}

	sshDir := filepath.Join(user.HomeDir, ".ssh")
	authKeysPath := filepath.Join(sshDir, "authorized_keys")
	access := UserSSHAccess{}

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

func getProcessSummary(ctx context.Context, username string) UserProcessSummary {
	cmdCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	summary := UserProcessSummary{Top: []UserProcess{}}
	output, err := exec.CommandContext(
		cmdCtx,
		"ps",
		"-u",
		username,
		"-o",
		"pid=,comm=,pcpu=,pmem=",
		"--sort=-pcpu",
	).Output()
	if err != nil {
		if isEmptyProcessListExit(output, err) {
			return summary
		}
		summary.Error = processSummaryError(err)
		return summary
	}

	for line := range strings.SplitSeq(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		cpu, _ := strconv.ParseFloat(fields[2], 64)
		mem, _ := strconv.ParseFloat(fields[3], 64)
		summary.Count++
		summary.Top = append(summary.Top, UserProcess{
			PID:     pid,
			Command: fields[1],
			CPU:     cpu,
			Memory:  mem,
		})
	}
	return summary
}

func isEmptyProcessListExit(output []byte, err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) &&
		strings.TrimSpace(string(output)) == "" &&
		strings.TrimSpace(string(exitErr.Stderr)) == ""
}

func processSummaryError(err error) string {
	if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
		if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
			return stderr
		}
	}
	return err.Error()
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

// CreateUser creates a new system user
func CreateUser(req CreateUserRequest) error {
	if req.Username == "" {
		return fmt.Errorf("username is required")
	}
	if req.Password == "" {
		return fmt.Errorf("password is required")
	}

	args := []string{}

	if req.CreateHome {
		args = append(args, "-m")
	}

	if req.FullName != "" {
		args = append(args, "-c", req.FullName)
	}

	if req.HomeDir != "" {
		args = append(args, "-d", req.HomeDir)
	}

	if req.Shell != "" {
		args = append(args, "-s", req.Shell)
	}

	if len(req.Groups) > 0 {
		args = append(args, "-G", strings.Join(req.Groups, ","))
	}

	args = append(args, req.Username)

	cmd := exec.Command("useradd", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create user: %s", strings.TrimSpace(string(output)))
	}

	// Set the password
	if err := setPassword(req.Username, req.Password); err != nil {
		// Try to clean up the user if password setting fails
		if cleanupErr := DeleteUser(req.Username); cleanupErr != nil {
			slog.Warn("failed to clean up user after password setup failure",
				"user", req.Username,
				"error", cleanupErr)
		}
		return fmt.Errorf("failed to set password: %w", err)
	}
	slog.Info("user created", "user", req.Username)
	return nil
}

// DeleteUser deletes a system user
func DeleteUser(username string) error {
	if username == "" {
		return fmt.Errorf("username is required")
	}

	if username == "root" {
		return fmt.Errorf("cannot delete root user")
	}

	// Check if user exists
	_, err := GetUser(username)
	if err != nil {
		return err
	}

	cmd := exec.Command("userdel", "-r", username)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete user: %s", strings.TrimSpace(string(output)))
	}
	slog.Info("user deleted", "user", username)
	return nil
}

// ModifyUser modifies user properties
func ModifyUser(req ModifyUserRequest) error {
	if req.Username == "" {
		return fmt.Errorf("username is required")
	}

	if req.Username == "root" {
		return fmt.Errorf("cannot modify root user")
	}

	// Check if user exists
	_, err := GetUser(req.Username)
	if err != nil {
		return err
	}

	args := []string{}

	if req.FullName != nil {
		args = append(args, "-c", *req.FullName)
	}

	if req.HomeDir != nil {
		args = append(args, "-d", *req.HomeDir, "-m")
	}

	if req.Shell != nil {
		args = append(args, "-s", *req.Shell)
	}

	if len(req.Groups) > 0 {
		args = append(args, "-G", strings.Join(req.Groups, ","))
	}

	if len(args) == 0 {
		return nil
	}

	args = append(args, req.Username)

	cmd := exec.Command("usermod", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to modify user: %s", strings.TrimSpace(string(output)))
	}
	slog.Info("user modified", "user", req.Username)
	return nil
}

// ChangePassword changes a user's password
func ChangePassword(username, password string) error {
	if username == "" {
		return fmt.Errorf("username is required")
	}
	if password == "" {
		return fmt.Errorf("password is required")
	}

	// Check if user exists
	_, err := GetUser(username)
	if err != nil {
		return err
	}

	return setPassword(username, password)
}

// LockUser locks a user account
func LockUser(username string) error {
	if username == "" {
		return fmt.Errorf("username is required")
	}

	if username == "root" {
		return fmt.Errorf("cannot lock root user")
	}

	// Check if user exists
	_, err := GetUser(username)
	if err != nil {
		return err
	}

	cmd := exec.Command("passwd", "-l", username)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to lock user: %s", strings.TrimSpace(string(output)))
	}
	slog.Info("user locked", "user", username)
	return nil
}

// UnlockUser unlocks a user account
func UnlockUser(username string) error {
	if username == "" {
		return fmt.Errorf("username is required")
	}

	// Check if user exists
	_, err := GetUser(username)
	if err != nil {
		return err
	}

	cmd := exec.Command("passwd", "-u", username)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to unlock user: %s", strings.TrimSpace(string(output)))
	}
	slog.Info("user unlocked", "user", username)
	return nil
}

// ListShells returns available login shells
func ListShells() ([]string, error) {
	shells := []string{}

	file, err := os.Open(shellsFile)
	if err != nil {
		// Return common defaults if file doesn't exist
		return []string{
			"/bin/bash",
			"/bin/sh",
			"/usr/bin/zsh",
			"/usr/sbin/nologin",
			"/sbin/nologin",
			"/bin/false",
			"/usr/bin/false",
		}, nil
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		shells = append(shells, line)
	}

	return shells, scanner.Err()
}

// setPassword sets a user's password using chpasswd
func setPassword(username, password string) error {
	if err := validateChpasswdInput(username, password); err != nil {
		return err
	}

	cmd := exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s\n", username, password))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("chpasswd failed: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

func validateChpasswdInput(username, password string) error {
	if strings.ContainsAny(username, ":\r\n") {
		return fmt.Errorf("username contains unsupported chpasswd separator characters")
	}
	if strings.ContainsAny(password, ":\r\n") {
		return fmt.Errorf("password contains unsupported chpasswd separator characters")
	}
	return nil
}

// getLockedUsers returns a map of usernames to their locked status
func getLockedUsers() map[string]bool {
	locked := make(map[string]bool)

	file, err := os.Open(shadowFile)
	if err != nil {
		return locked
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) < 2 {
			continue
		}

		username := parts[0]
		passwordHash := parts[1]

		// Account is locked if password starts with ! or *
		locked[username] = strings.HasPrefix(passwordHash, "!") || strings.HasPrefix(passwordHash, "*")
	}

	return locked
}

// parseGroupFile reads /etc/group once and returns both the username→groups
// map (secondary memberships) and the GID→name map.
func parseGroupFile() (map[string][]string, map[int]string) {
	userGroups := make(map[string][]string)
	gidToGroup := make(map[int]string)

	file, err := os.Open("/etc/group")
	if err != nil {
		return userGroups, gidToGroup
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) < 4 {
			continue
		}

		groupName := parts[0]

		if gid, err := strconv.Atoi(parts[2]); err == nil {
			gidToGroup[gid] = groupName
		}

		for member := range strings.SplitSeq(parts[3], ",") {
			member = strings.TrimSpace(member)
			if member != "" {
				userGroups[member] = append(userGroups[member], groupName)
			}
		}
	}

	return userGroups, gidToGroup
}

// getLastLogins returns a map of usernames to their last login time
func getLastLogins() map[string]string {
	lastLogins := make(map[string]string)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	logins, err := loginhistory.FetchByUser(ctx)
	if err != nil {
		return lastLogins
	}

	for username, login := range logins {
		lastLogins[username] = login.Time
	}

	return lastLogins
}
