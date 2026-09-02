package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/goccy/go-yaml"

	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

// fileOwnership describes the ownership that applies to runtime artifacts.
// Production stores enforce it regardless of whether the bridge is privileged.
type fileOwnership struct {
	uid     int
	gid     int
	enforce bool
}

func resolveFileOwnership(targetUID, targetGID uint32) (fileOwnership, error) {
	uid := int(targetUID)
	gid := int(targetGID)
	if uint32(uid) != targetUID || uint32(gid) != targetGID {
		return fileOwnership{}, fmt.Errorf("target ownership IDs are not representable: uid=%d gid=%d", targetUID, targetGID)
	}
	if euid := os.Geteuid(); euid != 0 {
		if euid != uid {
			return fileOwnership{}, fmt.Errorf("bridge effective uid %d does not match target uid %d", euid, uid)
		}
		if egid := os.Getegid(); egid != gid {
			return fileOwnership{}, fmt.Errorf("bridge effective gid %d does not match target gid %d", egid, gid)
		}
	}
	return fileOwnership{uid: uid, gid: gid, enforce: true}, nil
}

func (o fileOwnership) ensureDirectory(path string) error {
	if o.enforce {
		return o.verifyDirectoryOwner(path)
	}
	if err := os.MkdirAll(path, dirPerm); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat config directory %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("config directory must not be a symlink: %s", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("config path is not a directory: %s", path)
	}
	return nil
}

func (o fileOwnership) verifyDirectoryOwner(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat config directory %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("config directory must not be a symlink: %s", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("config path is not a directory: %s", path)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("config directory %q has no ownership metadata", path)
	}
	if uint64(stat.Uid) != uint64(o.uid) {
		return fmt.Errorf("config directory %q is owned by uid %d, want %d", path, stat.Uid, o.uid)
	}
	return nil
}

func (o fileOwnership) ensureFile(path string) error {
	if !o.enforce {
		return nil
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open config file %q: %w", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat config file %q: %w", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("config path is not a regular file: %s", path)
	}
	if err := f.Chown(o.uid, o.gid); err != nil {
		return fmt.Errorf("own config file %q: %w", path, err)
	}
	if err := f.Chmod(filePerm); err != nil {
		return fmt.Errorf("set config file permissions %q: %w", path, err)
	}
	return nil
}

func (o fileOwnership) writeAtomic(path string, data []byte, mode fs.FileMode) error {
	if o.enforce {
		return utils.WriteFileAtomic(path, data, mode, o.uid, o.gid)
	}
	return utils.WriteFileAtomic(path, data, mode)
}

func (o fileOwnership) lockOptions() []filelock.Option {
	opts := []filelock.Option{
		filelock.WithPermissions(lockFilePerm),
		filelock.WithDirPermissions(dirPerm),
	}
	if o.enforce {
		opts = append(opts, filelock.WithOwnership(o.uid, o.gid))
	}
	return opts
}

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

// CheckConfig returns true if the config file exists and is a regular file (not a symlink).
func CheckConfig(path string) (bool, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return false, errors.New("config path must not be a symlink")
		}
		return info.Mode().IsRegular(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

func writeCoreConfigOwned(cfgPath string, cfg Settings, owner fileOwnership) error {
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	return owner.writeAtomic(cfgPath, data, filePerm)
}

func writeUIConfigOwned(uiPath string, ui UIPreferences, owner fileOwnership) error {
	data, err := yaml.Marshal(&ui)
	if err != nil {
		return err
	}
	return owner.writeAtomic(uiPath, data, filePerm)
}

func writeEmptyUIConfigOwned(uiPath string, owner fileOwnership) error {
	return owner.writeAtomic(uiPath, []byte("{}\n"), filePerm)
}
