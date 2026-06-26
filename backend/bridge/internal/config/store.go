package config

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
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

func (s *UserStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// SnapshotForUser returns config from the per-user bridge store.
func SnapshotForUser(ctx context.Context, username string, store *UserStore) (*Settings, string, error) {
	if store == nil {
		return nil, "", errors.New("config store is nil")
	}
	if store.username != username {
		return nil, "", fmt.Errorf("config store user mismatch: store=%q requested=%q", store.username, username)
	}
	cfg, err := store.Snapshot(ctx)
	if err != nil {
		return nil, "", err
	}
	return cfg, store.Path(), nil
}

// UpdateForUser applies mutate through the per-user bridge store.
func UpdateForUser(ctx context.Context, username string, store *UserStore, mutate func(*Settings) error) (*Settings, string, error) {
	if mutate == nil {
		return nil, "", errors.New("config update function is nil")
	}
	if store == nil {
		return nil, "", errors.New("config store is nil")
	}
	if store.username != username {
		return nil, "", fmt.Errorf("config store user mismatch: store=%q requested=%q", store.username, username)
	}
	cfg, err := store.Update(ctx, mutate)
	return cfg, store.Path(), err
}

// Snapshot returns a copy of the current in-memory config.
func (s *UserStore) Snapshot(ctx context.Context) (*Settings, error) {
	if s == nil {
		return nil, errors.New("config store is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// sync.RWMutex.RLock is not cancellable once entered.
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSettings(&s.cfg), nil
}

// Update applies mutate to the latest on-disk config under an exclusive sidecar
// lock, writes it atomically, then refreshes the in-memory snapshot.
func (s *UserStore) Update(ctx context.Context, mutate func(*Settings) error) (*Settings, error) {
	if s == nil {
		return nil, errors.New("config store is nil")
	}
	if mutate == nil {
		return nil, errors.New("config update function is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// sync.Mutex.Lock is not cancellable once entered.
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	var updated *Settings
	if err := withExclusiveConfigLock(ctx, s.lockPath, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
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
		if err := ctx.Err(); err != nil {
			return err
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

func withExclusiveConfigLock(ctx context.Context, lockPath string, fn func() error) error {
	return filelock.WithExclusive(
		ctx,
		lockPath,
		fn,
		filelock.WithPermissions(lockFilePerm),
		filelock.WithDirPermissions(dirPerm),
	)
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
