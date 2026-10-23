package accounts

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/loginhistory"
)

const (
	passwdFile = "/etc/passwd"
	shadowFile = "/etc/shadow"
	shellsFile = "/etc/shells"
	systemUID  = 1000
)

// ListUsers returns login users (root + users with UID >= 1000 and valid shell)
func ListUsers(ctx context.Context) ([]apischema.AccountUser, error) {
	users := []apischema.AccountUser{}

	file, err := os.Open(passwdFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open passwd file: %w", err)
	}
	defer file.Close()

	lockedUsers := getLockedUsers()
	userGroups, gidToGroup := groupIndex(ctx)
	lastLogins := getLastLogins(ctx)

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
) (apischema.AccountUser, bool) {
	if line == "" || strings.HasPrefix(line, "#") {
		return apischema.AccountUser{}, false
	}

	parts := strings.Split(line, ":")
	if len(parts) < 7 {
		return apischema.AccountUser{}, false
	}

	uid, err := strconv.Atoi(parts[2])
	if err != nil {
		return apischema.AccountUser{}, false
	}

	gid, err := strconv.Atoi(parts[3])
	if err != nil {
		return apischema.AccountUser{}, false
	}

	shell := parts[6]
	if uid != 0 && uid < systemUID {
		return apischema.AccountUser{}, false
	}
	if uid != 0 && isNonLoginShell(shell) {
		return apischema.AccountUser{}, false
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

	return apischema.AccountUser{
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
func GetUser(ctx context.Context, username string) (*apischema.AccountUser, error) {
	users, err := ListUsers(ctx)
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

// ListShells returns available login shells
func ListShells(ctx context.Context) ([]string, error) {
	shells := []string{}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

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
	if err := scanner.Err(); err != nil {
		slog.Warn("failed to read shadow file", "file", shadowFile, "error", err)
	}

	return locked
}

// getLastLogins returns a map of usernames to their last login time
func getLastLogins(parent context.Context) map[string]string {
	lastLogins := make(map[string]string)

	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
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
