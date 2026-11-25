package accounts

import (
	"context"
	"errors"
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

// runAccountTool runs one of the account tools (useradd, userdel, usermod,
// groupdel, gpasswd) and returns what it printed. A var so tests can stand
// in for them.
var runAccountTool = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

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

	output, err := runAccountTool(ctx, "useradd", args...)
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
	// The account exists from here, and deleting it would delete its home,
	// so a failure below says what is left to do instead of undoing it.
	if req.SMBSignIn != nil && *req.SMBSignIn {
		if err := syncSambaPassword(ctx, req.Username, req.Password, req.SMBSignIn); err != nil {
			return fmt.Errorf("user %s was created, but cannot sign in to shared folders yet; set the password again to turn that on: %w", req.Username, err)
		}
	}
	if len(req.Groups) > 0 {
		shareAccessChanged(ctx)
	}
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

	// Samba's entry goes first, while the account is there for Samba to look
	// up; a user who stays after a failed delete has lost only that sign-in.
	if err := removeSambaUser(ctx, username); err != nil {
		return fmt.Errorf("failed to delete user: %w", err)
	}

	output, err := runAccountTool(ctx, "userdel", "-r", username)
	if err != nil {
		return fmt.Errorf("failed to delete user: %s", strings.TrimSpace(string(output)))
	}
	slog.Info("user deleted", "user", username)
	shareAccessChanged(ctx)
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

	output, err := runAccountTool(ctx, "usermod", args...)
	if err != nil {
		return fmt.Errorf("failed to modify user: %s", strings.TrimSpace(string(output)))
	}
	slog.Info("user modified", "user", req.Username)
	if req.Groups != nil {
		shareAccessChanged(ctx)
	}
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

// ChangePassword changes a user's password. The system password is set
// first and the one for shared folders after it (see
// shares.SyncSambaPassword for smbSignIn), so the one Samba holds is never
// the newer of the two.
func ChangePassword(ctx context.Context, username, password string, smbSignIn *bool) error {
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

	if err := setPassword(ctx, username, password); err != nil {
		return err
	}
	slog.Info("user password changed", "user", username)
	if err := syncSambaPassword(ctx, username, password, smbSignIn); err != nil {
		return fmt.Errorf("the password of %s was changed, but not the one for shared folders; set it again: %w", username, err)
	}
	return nil
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

	// Samba checks its own password and never the system one's lock, so its
	// entry is disabled too, first, so a lock that fails half-way leaves the
	// user less access, not more. A Samba that cannot be changed does not
	// keep the account from being locked; the error says what is left.
	sambaErr := setSambaUserEnabled(ctx, username, false)
	output, err := runAccountTool(ctx, "passwd", "-l", username)
	if err != nil {
		return errors.Join(fmt.Errorf("failed to lock user: %s", strings.TrimSpace(string(output))), sambaErr)
	}
	slog.Info("user locked", "user", username)
	if sambaErr != nil {
		return fmt.Errorf("%s was locked, but may still open shared folders from Windows or a Mac: %w", username, sambaErr)
	}
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

	output, err := runAccountTool(ctx, "passwd", "-u", username)
	if err != nil {
		return fmt.Errorf("failed to unlock user: %s", strings.TrimSpace(string(output)))
	}
	slog.Info("user unlocked", "user", username)
	if err := setSambaUserEnabled(ctx, username, true); err != nil {
		return fmt.Errorf("%s was unlocked, but cannot open shared folders yet; unlock again: %w", username, err)
	}
	return nil
}

// setPassword sets a user's password using chpasswd. A var so tests can
// stand in for it.
var setPassword = func(ctx context.Context, username, password string) error {
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
