package config

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
)

const lockFilePerm = 0o600

// UserStore owns independent core and UI snapshots for one bridge process.
// Core and UI updates use separate locks so a frontend preference write cannot
// block or clobber Docker/job state.
type UserStore struct {
	username   string
	path       string
	uiPath     string
	lockPath   string
	uiLockPath string
	owner      fileOwnership

	mu         sync.RWMutex
	uiMu       sync.RWMutex
	updateMu   sync.Mutex
	uiUpdateMu sync.Mutex
	cfg        Settings
	ui         UIPreferences
}

// OpenUserStore prepares both files and loads both snapshots while holding both
// sidecar locks. The authenticated numeric UID/GID identify the owner of every
// runtime artifact; username is used only to resolve the configuration base.
func OpenUserStore(username string, targetUID, targetGID uint32) (*UserStore, error) {
	owner, err := resolveFileOwnership(targetUID, targetGID)
	if err != nil {
		return nil, err
	}
	base, err := configBase(username)
	if err != nil {
		return nil, err
	}
	cfgPath := filepath.Join(base, cfgFileName)
	uiPath := filepath.Join(base, uiCfgFileName)
	store := &UserStore{
		username:   username,
		path:       cfgPath,
		uiPath:     uiPath,
		lockPath:   cfgPath + ".lock",
		uiLockPath: uiPath + ".lock",
		owner:      owner,
	}
	if err := withConfigLocksOwned(context.Background(), store.lockPath, store.uiLockPath, owner, func() error {
		if err := initializeLockedOwned(cfgPath, uiPath, base, owner); err != nil {
			return err
		}
		cfg, err := readCoreLatestOwned(cfgPath, base)
		if err != nil {
			return err
		}
		ui, err := readUILatestOwned(uiPath, owner)
		if err != nil {
			return err
		}
		store.cfg = *cloneSettings(cfg)
		store.ui = *cloneUIPreferences(ui)
		return nil
	}); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *UserStore) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *UserStore) UIPath() string {
	if s == nil {
		return ""
	}
	return s.uiPath
}

// SnapshotForUser returns core config from the per-user bridge store.
func SnapshotForUser(ctx context.Context, username string, store *UserStore) (*Settings, string, error) {
	if err := validateStoreUser(username, store); err != nil {
		return nil, "", err
	}
	cfg, err := store.Snapshot(ctx)
	if err != nil {
		return nil, "", err
	}
	return cfg, store.Path(), nil
}

// UpdateForUser applies a core mutation through the per-user bridge store.
func UpdateForUser(ctx context.Context, username string, store *UserStore, mutate func(*Settings) error) (*Settings, string, error) {
	if mutate == nil {
		return nil, "", errors.New("config update function is nil")
	}
	if err := validateStoreUser(username, store); err != nil {
		return nil, "", err
	}
	cfg, err := store.Update(ctx, mutate)
	return cfg, store.Path(), err
}

// UISnapshotForUser returns UI preferences from the per-user bridge store.
func UISnapshotForUser(ctx context.Context, username string, store *UserStore) (*UIPreferences, string, error) {
	if err := validateStoreUser(username, store); err != nil {
		return nil, "", err
	}
	ui, err := store.UISnapshot(ctx)
	if err != nil {
		return nil, "", err
	}
	return ui, store.UIPath(), nil
}

// ReplaceUIForUser replaces the complete UI snapshot through the per-user bridge store.
func ReplaceUIForUser(ctx context.Context, username string, store *UserStore, replacement UIPreferences) (*UIPreferences, string, error) {
	if err := validateStoreUser(username, store); err != nil {
		return nil, "", err
	}
	ui, err := store.ReplaceUI(ctx, replacement)
	return ui, store.UIPath(), err
}

func validateStoreUser(username string, store *UserStore) error {
	if store == nil {
		return errors.New("config store is nil")
	}
	if store.username != username {
		return fmt.Errorf("config store user mismatch: store=%q requested=%q", store.username, username)
	}
	return nil
}

// Snapshot returns a copy of the current core snapshot.
func (s *UserStore) Snapshot(ctx context.Context) (*Settings, error) {
	if s == nil {
		return nil, errors.New("config store is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneSettings(&s.cfg), nil
}

// UISnapshot returns a copy of the current UI snapshot.
func (s *UserStore) UISnapshot(ctx context.Context) (*UIPreferences, error) {
	if s == nil {
		return nil, errors.New("config store is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.uiMu.RLock()
	defer s.uiMu.RUnlock()
	return cloneUIPreferences(&s.ui), nil
}

// Update applies a mutation to the latest core file under its sidecar lock.
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
	s.updateMu.Lock()
	defer s.updateMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var updated *Settings
	err := withExclusiveConfigLockOwned(ctx, s.lockPath, s.owner, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		current, err := readCoreLatestOwned(s.path, filepath.Dir(s.path))
		if err != nil {
			return fmt.Errorf("read core config: %w", err)
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
		if err := writeCoreConfigOwned(s.path, *next, s.owner); err != nil {
			return fmt.Errorf("write core config: %w", err)
		}
		updated = cloneSettings(next)
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cfg = *cloneSettings(updated)
	s.mu.Unlock()
	return cloneSettings(updated), nil
}

// ReplaceUI validates and writes one complete UI snapshot under its sidecar
// lock. Replacement semantics deliberately avoid reading or merging the old
// UI file.
func (s *UserStore) ReplaceUI(ctx context.Context, replacement UIPreferences) (*UIPreferences, error) {
	if s == nil {
		return nil, errors.New("config store is nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	next := cloneUIPreferences(&replacement)
	if errs := ValidateUIPreferences(next); len(errs) > 0 {
		return nil, fmt.Errorf("validate UI config: %s", strings.Join(errs, "; "))
	}
	s.uiUpdateMu.Lock()
	defer s.uiUpdateMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var updated *UIPreferences
	err := withExclusiveUILockOwned(ctx, s.uiLockPath, s.owner, func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := writeUIConfigOwned(s.uiPath, *next, s.owner); err != nil {
			return fmt.Errorf("write UI config: %w", err)
		}
		updated = cloneUIPreferences(next)
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.uiMu.Lock()
	s.ui = *cloneUIPreferences(updated)
	s.uiMu.Unlock()
	return cloneUIPreferences(updated), nil
}

func readCoreLatestOwned(path, base string) (*Settings, error) {
	exists, err := CheckConfig(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("core config path is not a regular file: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg, parseErr := parseCoreConfig(raw, path, base)
	if parseErr == nil {
		return cfg, nil
	}
	return nil, fmt.Errorf("invalid core config: %w", parseErr)
}

func readUILatestOwned(path string, owner fileOwnership) (*UIPreferences, error) {
	exists, err := CheckConfig(path)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("UI config path is not a regular file: %s", path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ui, parseErr := parseUIConfig(raw, path)
	if parseErr == nil {
		return ui, nil
	}
	replacement := DefaultUIPreferences()
	if err := writeEmptyUIConfigOwned(path, owner); err != nil {
		return nil, errors.Join(
			fmt.Errorf("invalid UI config: %w", parseErr),
			fmt.Errorf("reset UI config: %w", err),
		)
	}
	return &replacement, nil
}

func withExclusiveConfigLockOwned(ctx context.Context, lockPath string, owner fileOwnership, fn func() error) error {
	return runExclusive(ctx, lockPath, owner, fn)
}

func withExclusiveUILockOwned(ctx context.Context, lockPath string, owner fileOwnership, fn func() error) error {
	return runExclusive(ctx, lockPath, owner, fn)
}

func runExclusive(ctx context.Context, lockPath string, owner fileOwnership, fn func() error) error {
	if fn == nil {
		return errors.New("lock function is nil")
	}
	if err := owner.ensureDirectory(filepath.Dir(lockPath)); err != nil {
		return err
	}
	release, err := filelock.AcquireExclusive(ctx, lockPath, owner.lockOptions()...)
	if err != nil {
		return err
	}
	return errors.Join(fn(), release())
}

func withConfigLocksOwned(ctx context.Context, configLockPath, uiLockPath string, owner fileOwnership, fn func() error) error {
	if fn == nil {
		return errors.New("lock function is nil")
	}
	if err := owner.ensureDirectory(filepath.Dir(configLockPath)); err != nil {
		return err
	}
	releaseConfig, err := filelock.AcquireExclusive(ctx, configLockPath, owner.lockOptions()...)
	if err != nil {
		return err
	}
	if dirErr := owner.ensureDirectory(filepath.Dir(uiLockPath)); dirErr != nil {
		return errors.Join(dirErr, releaseConfig())
	}
	releaseUI, err := filelock.AcquireExclusive(ctx, uiLockPath, owner.lockOptions()...)
	if err != nil {
		return errors.Join(err, releaseConfig())
	}
	return errors.Join(fn(), releaseUI(), releaseConfig())
}
