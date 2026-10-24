package accounts

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	osuser "os/user"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// lookupUser is a seam for tests.
var lookupUser = osuser.Lookup

// requireUser fails unless the account exists in the passwd database, including
// system and nologin accounts that ListUsers filters out.
func requireUser(username string) error {
	if _, err := lookupUser(username); err != nil {
		return fmt.Errorf("%w: user %s: %w", bridgeipc.ErrNotFound, username, err)
	}
	return nil
}

// CreateUser creates a new system user
func CreateUser(ctx context.Context, req apischema.CreateUserRequest) error {
	if req.Username == "" {
		return fmt.Errorf("%w: username", bridgeipc.ErrInvalidArgs)
	}
	if req.Password == "" {
		return fmt.Errorf("%w: password", bridgeipc.ErrInvalidArgs)
	}

	args := []string{}

	if req.CreateHome != nil && *req.CreateHome {
		args = append(args, "-m")
	}

	if req.FullName != nil && *req.FullName != "" {
		args = append(args, "-c", *req.FullName)
	}

	if req.HomeDir != nil && *req.HomeDir != "" {
		args = append(args, "-d", *req.HomeDir)
	}

	if req.Shell != nil && *req.Shell != "" {
		args = append(args, "-s", *req.Shell)
	}

	if len(req.Groups) > 0 {
		args = append(args, "-G", strings.Join(req.Groups, ","))
	}

	args = append(args, req.Username)

	cmd := exec.CommandContext(ctx, "useradd", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create user: %s", strings.TrimSpace(string(output)))
	}

	// Set the password
	if err := setPassword(ctx, req.Username, req.Password); err != nil {
		// Try to clean up the user if password setting fails
		if cleanupErr := DeleteUser(ctx, req.Username); cleanupErr != nil {
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
func DeleteUser(ctx context.Context, username string) error {
	if username == "" {
		return fmt.Errorf("%w: username", bridgeipc.ErrInvalidArgs)
	}

	if username == "root" {
		return fmt.Errorf("%w: delete root user", bridgeipc.ErrForbidden)
	}

	// Check if user exists
	if err := requireUser(username); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "userdel", "-r", username)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to delete user: %s", strings.TrimSpace(string(output)))
	}
	slog.Info("user deleted", "user", username)
	return nil
}

// ModifyUser modifies user properties
func ModifyUser(ctx context.Context, req apischema.ModifyUserRequest) error {
	if req.Username == "" {
		return fmt.Errorf("%w: username", bridgeipc.ErrInvalidArgs)
	}

	if req.Username == "root" {
		return fmt.Errorf("%w: modify root user", bridgeipc.ErrForbidden)
	}

	// Check if user exists
	if err := requireUser(req.Username); err != nil {
		return err
	}

	args := modifyUserArgs(req)
	if len(args) == 0 {
		return nil
	}

	args = append(args, req.Username)

	cmd := exec.CommandContext(ctx, "usermod", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to modify user: %s", strings.TrimSpace(string(output)))
	}
	slog.Info("user modified", "user", req.Username)
	return nil
}

// modifyUserArgs builds the usermod flags for the fields present in req.
func modifyUserArgs(req apischema.ModifyUserRequest) []string {
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

	// A non-nil empty list clears the secondary groups (-G "").
	if req.Groups != nil {
		args = append(args, "-G", strings.Join(req.Groups, ","))
	}

	return args
}

// ChangePassword changes a user's password
func ChangePassword(ctx context.Context, username, password string) error {
	if username == "" {
		return fmt.Errorf("%w: username", bridgeipc.ErrInvalidArgs)
	}
	if password == "" {
		return fmt.Errorf("%w: password", bridgeipc.ErrInvalidArgs)
	}

	// Check if user exists
	if err := requireUser(username); err != nil {
		return err
	}

	return setPassword(ctx, username, password)
}

// LockUser locks a user account
func LockUser(ctx context.Context, username string) error {
	if username == "" {
		return fmt.Errorf("%w: username", bridgeipc.ErrInvalidArgs)
	}

	if username == "root" {
		return fmt.Errorf("%w: lock root user", bridgeipc.ErrForbidden)
	}

	// Check if user exists
	if err := requireUser(username); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "passwd", "-l", username)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to lock user: %s", strings.TrimSpace(string(output)))
	}
	slog.Info("user locked", "user", username)
	return nil
}

// UnlockUser unlocks a user account
func UnlockUser(ctx context.Context, username string) error {
	if username == "" {
		return fmt.Errorf("%w: username", bridgeipc.ErrInvalidArgs)
	}

	// Check if user exists
	if err := requireUser(username); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "passwd", "-u", username)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to unlock user: %s", strings.TrimSpace(string(output)))
	}
	slog.Info("user unlocked", "user", username)
	return nil
}

// setPassword sets a user's password using chpasswd
func setPassword(ctx context.Context, username, password string) error {
	if err := validateChpasswdInput(username, password); err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, "chpasswd")
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
