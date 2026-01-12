package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

// Homedir determines the user's home folder
func Homedir(username string) (string, error) {
	if strings.TrimSpace(username) == "" {
		return "", errors.New("empty username")
	}
	// 1) Prefer the target user's passwd entry (correct when running as root)
	if u, err := user.Lookup(username); err == nil && u.HomeDir != "" {
		if fi, err2 := os.Stat(u.HomeDir); err2 == nil && fi.IsDir() {
			return u.HomeDir, nil
		} else if err2 != nil {
			return "", err2
		}
		return "", errors.New("home path is not a directory")
	}
	// 2) Fall back to the process $HOME
	if home := os.Getenv("HOME"); home != "" && filepath.IsAbs(home) {
		if fi, err := os.Stat(home); err == nil && fi.IsDir() {
			return home, nil
		}
	}
	return "", errors.New("could not resolve home dir for user")
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

	p := filepathJoinClean("/var", "tmp", "linuxio", "users", username)
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
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	f, err := os.CreateTemp(dir, base+".*.tmp")
	if err != nil {
		return err
	}
	tmp := f.Name()
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

// readConfigStrict parses YAML and FAILS on unknown fields.
func readConfigStrict(path string) (*Settings, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out Settings
	dec := yaml.NewDecoder(bytes.NewReader(b), yaml.Strict())
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// init.go (near other helpers)
func chownIfRoot(path, username string) error {
	if os.Geteuid() != 0 {
		return nil // Nothing to do if not root
	}
	u, err := user.Lookup(username)
	if err != nil {
		return err
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)

	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to chown symlink")
	}
	return os.Chown(path, uid, gid)
}
