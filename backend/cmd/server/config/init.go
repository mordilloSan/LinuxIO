//go:generate go run ./generator.go

package config

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/internal/logger"
	"gopkg.in/yaml.v3"
)

const (
	cfgFileName = ".linuxio-config.yaml"
	filePerm    = 0o664 // file:  rw-rw-r--
	dirPerm     = 0o775 // dir:   rwxrwxr-x
)

// Initialize prepares the per-user LinuxIO configuration file for `username`.
//
// Flow:
//  1. Resolve a base folder: try Homedir(username); if that fails, choose a
//     writable POSIX/XDG fallback (no root required).
//  2. Build <base>/.linuxio-config.yaml.
//  3. If the file exists: repair in place (keep valid fields, fix only bad ones).
//     If the file does not exist: create defaults.
//  4. Ensure file permissions to 0o664.
func Initialize(username string) error {
	base, baseErr := Homedir(username)
	if baseErr != nil {
		logger.Warnf("homedir not available for %q: %v — using fallback", username, baseErr)
		b, err := fallbackBase(username)
		if err != nil {
			logger.Errorf("fallback base resolution failed: %v", err)
			return err
		}
		base = b
	}
	cfgPath := filepath.Join(base, cfgFileName)

	exists, err := CheckConfig(cfgPath)
	if err != nil {
		logger.Errorf("check config: %v", err)
		return err
	}

	if exists {
		if err := repairConfig(cfgPath, base); err != nil {
			return err
		}
		if err := ensureFilePerms(cfgPath, filePerm); err != nil {
			logger.Errorf("chmod existing config: %v", err)
			return err
		}
		logger.Debugf("Loaded config from %s", cfgPath)
		return nil
	}

	// Create new with defaults (Docker.Folder = <base>/docker)
	logger.Infof("New user detected - Generating default config for: %v", username)
	if err := writeConfig(cfgPath, base); err != nil {
		logger.Errorf("write default config: %v", err)
		return err
	}
	logger.Infof("Created default config at %s", cfgPath)
	return nil
}

// Homedir determines the user's home folder:
// 1) $HOME (absolute & exists), else
// 2) /etc/passwd (user.Lookup), exists.
// Returns error if neither works.
func Homedir(username string) (string, error) {
	if strings.TrimSpace(username) == "" {
		return "", errors.New("empty username")
	}

	// 1) $HOME
	if home := os.Getenv("HOME"); home != "" && filepath.IsAbs(home) {
		if fi, err := os.Stat(home); err == nil && fi.IsDir() {
			return home, nil
		}
	}

	// 2) passwd entry
	u, err := user.Lookup(username)
	if err != nil {
		return "", err
	}
	if u.HomeDir == "" {
		return "", errors.New("no home directory in passwd")
	}
	if fi, err := os.Stat(u.HomeDir); err == nil && fi.IsDir() {
		return u.HomeDir, nil
	} else if err != nil {
		return "", err
	}
	return "", errors.New("home path is not a directory")
}

// fallbackBase returns a writable, non-root-required base folder:
//   - $XDG_DATA_HOME/linuxio/users/<username>
//   - else ~/.local/share/linuxio/users/<username> (of the running process)
//   - else /var/tmp/linuxio-<procUID>/users/<username>
func fallbackBase(username string) (string, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return "", errors.New("empty username")
	}

	if base := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); base != "" && filepath.IsAbs(base) {
		p := filepathJoinClean(base, "linuxio", "users", username)
		if err := os.MkdirAll(p, dirPerm); err == nil {
			return p, nil
		}
	}

	if h, _ := os.UserHomeDir(); h != "" {
		p := filepathJoinClean(h, ".local", "share", "linuxio", "users", username)
		if err := os.MkdirAll(p, dirPerm); err == nil {
			return p, nil
		}
	}

	uid := os.Getuid()
	p := filepathJoinClean("/var", "tmp", "linuxio-"+strconv.Itoa(uid), "users", username)
	if err := os.MkdirAll(p, dirPerm); err != nil {
		return "", fmt.Errorf("mkdir fallback %s: %w", p, err)
	}
	return p, nil
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

// writeConfig writes a default config atomically to the given path (0o664)
// and ensures the parent directory exists with dirPerm.
func writeConfig(path string, base string) error {
	if err := os.MkdirAll(filepath.Dir(path), dirPerm); err != nil {
		return err
	}
	return writeConfigFrom(path, *DefaultSettings(base))
}

func ensureFilePerms(path string, mode os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("config path is a symlink")
	}
	return os.Chmod(path, mode)
}

// writeConfigFrom writes the provided Settings atomically to cfgPath with filePerm.
func writeConfigFrom(cfgPath string, cfg Settings) error {
	if err := os.MkdirAll(filepath.Dir(cfgPath), dirPerm); err != nil {
		return err
	}
	data, err := yaml.Marshal(&cfg)
	if err != nil {
		return err
	}
	return writeYAMLAtomic(cfgPath, data, filePerm)
}

// writeYAMLAtomic writes data to path atomically using O_EXCL temp file + rename.
func writeYAMLAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		_ = os.Remove(tmp)
		return werr
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return cerr
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, perm)
}

// filepathJoinClean joins then cleans the result (normalizes).
func filepathJoinClean(elem ...string) string {
	return filepath.Clean(filepath.Join(elem...))
}
