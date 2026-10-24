package userconfig

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Homedir determines the user's home folder
func Homedir(username string) (string, error) {
	if strings.TrimSpace(username) == "" {
		return "", errors.New("empty username")
	}
	u, err := user.Lookup(username)
	if err != nil {
		return "", fmt.Errorf("lookup user %q: %w", username, err)
	}
	if u.HomeDir == "" {
		return "", errors.New("user has no home directory")
	}
	if !filepath.IsAbs(u.HomeDir) {
		return "", fmt.Errorf("user home directory is not absolute: %s", u.HomeDir)
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return "", fmt.Errorf("user %q has invalid uid %q: %w", username, u.Uid, err)
	}
	return resolveHomePath(u.HomeDir, uint32(uid))
}

// resolveHomePath follows the passwd home path, then verifies the resolved
// directory belongs to the authenticated user. Returning the resolved path
// also keeps later config-path checks from traversing a home symlink.
func resolveHomePath(home string, uid uint32) (string, error) {
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("user home directory is not absolute: %s", home)
	}
	info, err := os.Stat(home)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("home path is not a directory")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("home directory has no ownership metadata")
	}
	if uint64(stat.Uid) != uint64(uid) {
		return "", fmt.Errorf("home directory %q is owned by uid %d, want %d", home, stat.Uid, uid)
	}
	resolved, err := filepath.EvalSymlinks(home)
	if err != nil {
		return "", fmt.Errorf("resolve home directory %q: %w", home, err)
	}
	return filepath.Clean(resolved), nil
}
