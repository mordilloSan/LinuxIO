package accounts

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"

	"github.com/mordilloSan/go-logger/logger"
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
	userGroups := getUserGroups()
	gidToGroup := getGIDToGroupName()
	lastLogins := getLastLogins()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) < 7 {
			continue
		}

		uid, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}

		gid, err := strconv.Atoi(parts[3])
		if err != nil {
			continue
		}

		shell := parts[6]

		// Filter: only show root OR users with UID >= 1000 that have a login shell
		if uid != 0 && uid < systemUID {
			continue // Skip system users (except root)
		}

		// Skip users with nologin or false shells (service accounts)
		if uid != 0 && isNonLoginShell(shell) {
			continue
		}

		username := parts[0]
		primaryGroup := gidToGroup[gid]
		if primaryGroup == "" {
			primaryGroup = strconv.Itoa(gid)
		}

		user := User{
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
			LastLogin:    lastLogins[username],
		}

		users = append(users, user)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading passwd file: %w", err)
	}

	return users, nil
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
			logger.Warnf("failed to clean up user %s after password setup failure: %v", req.Username, cleanupErr)
		}
		return fmt.Errorf("failed to set password: %w", err)
	}

	logger.Infof("Created user: %s", req.Username)
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

	logger.Infof("Deleted user: %s", username)
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

	logger.Infof("Modified user: %s", req.Username)
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

	logger.Infof("Locked user: %s", username)
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

	logger.Infof("Unlocked user: %s", username)
	return nil
}

// ListShells returns available login shells
func ListShells() ([]string, error) {
	shells := []string{}

	file, err := os.Open(shellsFile)
	if err != nil {
		// Return common defaults if file doesn't exist
		return []string{"/bin/bash", "/bin/sh", "/usr/bin/zsh", "/sbin/nologin"}, nil
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
	cmd := exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("%s:%s", username, password))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("chpasswd failed: %s", strings.TrimSpace(string(output)))
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

// getUserGroups returns a map of usernames to their secondary groups
func getUserGroups() map[string][]string {
	userGroups := make(map[string][]string)

	file, err := os.Open("/etc/group")
	if err != nil {
		return userGroups
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
		members := parts[3]

		if members == "" {
			continue
		}

		for member := range strings.SplitSeq(members, ",") {
			member = strings.TrimSpace(member)
			if member != "" {
				userGroups[member] = append(userGroups[member], groupName)
			}
		}
	}

	return userGroups
}

// getGIDToGroupName returns a map of GID to group name
func getGIDToGroupName() map[int]string {
	gidToGroup := make(map[int]string)

	file, err := os.Open("/etc/group")
	if err != nil {
		return gidToGroup
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Split(line, ":")
		if len(parts) < 3 {
			continue
		}

		gid, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}

		gidToGroup[gid] = parts[0]
	}

	return gidToGroup
}

// getLastLogins returns a map of usernames to their last login time
func getLastLogins() map[string]string {
	lastLogins := make(map[string]string)

	// Use lastlog command to get last login times
	cmd := exec.Command("lastlog")
	output, err := cmd.Output()
	if err != nil {
		return lastLogins
	}

	lines := strings.Split(string(output), "\n")
	for i, line := range lines {
		// Skip header line
		if i == 0 {
			continue
		}

		// Parse the lastlog output
		// Format: Username         Port     From             Latest
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}

		username := fields[0]

		// Check if "Never logged in" appears in the line
		if strings.Contains(line, "**Never logged in**") {
			lastLogins[username] = "Never"
			continue
		}

		// Extract the date portion (last 4-5 fields typically)
		// Format varies but usually: "Mon Dec 27 10:42:00 +0000 2025"
		if len(fields) >= 4 {
			// Find where the date starts (after "From" field or directly after port)
			dateStart := 3
			if len(fields) > 4 {
				dateStart = max(len(fields)-5, 3)
			}
			dateStr := strings.Join(fields[dateStart:], " ")
			lastLogins[username] = dateStr
		}
	}

	return lastLogins
}
