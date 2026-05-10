package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

const lockFilePerm = 0o600

// UserStore owns the current per-user config snapshot for a bridge process.
//
// Reads are served from memory. Writes are serialized in-process and through a
// sidecar file lock so multiple bridge sessions for the same user do not clobber
// one another's config changes.
type UserStore struct {
	username string
	path     string
	lockPath string

	mu       sync.RWMutex
	updateMu sync.Mutex
	cfg      Settings
}

// OpenUserStore prepares the user's config file, loads it once, and returns an
// in-memory store for runtime reads and write-through updates.
func OpenUserStore(username string) (*UserStore, error) {
	if err := Initialize(username); err != nil {
		return nil, err
	}

	cfg, cfgPath, err := load(username)
	if err != nil {
		return nil, err
	}

	if err := chownIfRoot(filepath.Dir(cfgPath), username); err != nil {
		return nil, err
	}
	if err := chownIfRoot(cfgPath, username); err != nil {
		return nil, err
	}

	return newUserStore(username, cfgPath, cfg), nil
}

func newUserStore(username, cfgPath string, cfg *Settings) *UserStore {
	store := &UserStore{
		username: username,
		path:     cfgPath,
		lockPath: cfgPath + ".lock",
	}
	if cfg != nil {
		store.cfg = *cloneSettings(cfg)
	}
	return store
}

func (s *UserStore) Username() string {
	if s == nil {
		return ""
	}
	return s.username
}

func (s *UserStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *UserStore) LockPath() string {
	if s == nil {
		return ""
	}
	return s.lockPath
}

// SnapshotForUser returns config from the per-user bridge store.
func SnapshotForUser(username string, store *UserStore) (*Settings, string, error) {
	if store == nil {
		return nil, "", errors.New("config store is nil")
	}
	if store.username != username {
		return nil, "", fmt.Errorf("config store user mismatch: store=%q requested=%q", store.username, username)
	}
	cfg, err := store.Snapshot()
	if err != nil {
		return nil, "", err
	}
	return cfg, store.Path(), nil
}

// UpdateForUser applies mutate through the per-user bridge store.
func UpdateForUser(username string, store *UserStore, mutate func(*Settings) error) (*Settings, string, error) {
	if mutate == nil {
		return nil, "", errors.New("config update function is nil")
	}
	if store == nil {
		return nil, "", errors.New("config store is nil")
	}
	if store.username != username {
		return nil, "", fmt.Errorf("config store user mismatch: store=%q requested=%q", store.username, username)
	}
	cfg, err := store.Update(mutate)
	return cfg, store.Path(), err
}

// Snapshot returns a copy of the current in-memory config.
func (s *UserStore) Snapshot() (*Settings, error) {
	if s == nil {
		return nil, errors.New("config store is nil")
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSettings(&s.cfg), nil
}

// Update applies mutate to the latest on-disk config under an exclusive sidecar
// lock, writes it atomically, then refreshes the in-memory snapshot.
func (s *UserStore) Update(mutate func(*Settings) error) (*Settings, error) {
	if s == nil {
		return nil, errors.New("config store is nil")
	}
	if mutate == nil {
		return nil, errors.New("config update function is nil")
	}

	s.updateMu.Lock()
	defer s.updateMu.Unlock()

	var updated *Settings
	if err := withExclusiveConfigLock(s.lockPath, func() error {
		current, err := readConfigStrict(s.path)
		if err != nil {
			return fmt.Errorf("read config: %w", err)
		}

		next := cloneSettings(current)
		if err := mutate(next); err != nil {
			return err
		}
		if errs := ValidateConfig(next); len(errs) > 0 {
			return fmt.Errorf("validate config: %s", strings.Join(errs, "; "))
		}
		if err := writeConfigFrom(s.path, *next); err != nil {
			return fmt.Errorf("write config: %w", err)
		}
		if err := ensureFilePerms(s.path, filePerm); err != nil {
			return fmt.Errorf("set config permissions: %w", err)
		}

		updated = cloneSettings(next)
		return nil
	}); err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.cfg = *cloneSettings(updated)
	s.mu.Unlock()

	return cloneSettings(updated), nil
}

func withExclusiveConfigLock(lockPath string, fn func() error) error {
	lockFile, err := openConfigLockFile(lockPath)
	if err != nil {
		return err
	}

	if err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX); err != nil {
		closeErr := lockFile.Close()
		return errors.Join(fmt.Errorf("lock config: %w", err), closeErr)
	}

	fnErr := fn()
	unlockErr := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	closeErr := lockFile.Close()
	return errors.Join(fnErr, unlockErr, closeErr)
}

func openConfigLockFile(lockPath string) (*os.File, error) {
	if strings.TrimSpace(lockPath) == "" {
		return nil, errors.New("config lock path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(lockPath), dirPerm); err != nil {
		return nil, err
	}

	fd, err := syscall.Open(
		lockPath,
		syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW,
		lockFilePerm,
	)
	if err != nil {
		return nil, fmt.Errorf("open config lock: %w", err)
	}
	return os.NewFile(uintptr(fd), lockPath), nil
}

// load returns the parsed Settings for `username` and the absolute config path.
// It does NOT create/repair the file; call Initialize(username) first if needed.
func load(username string) (*Settings, string, error) {
	base, err := Homedir(username)
	if err != nil {
		// fall back if no home (same logic as Initialize)
		if base, err = fallbackBase(username); err != nil {
			return nil, "", err
		}
	}
	cfgPath := filepath.Join(base, cfgFileName)
	if err = guardConfigPath(cfgPath); err != nil {
		return nil, "", err
	}

	// strict read (unknown keys rejected); your repair path runs in Initialize.
	cfg, err := readConfigStrict(cfgPath)
	if err != nil {
		return nil, "", err
	}
	return cfg, cfgPath, nil
}
