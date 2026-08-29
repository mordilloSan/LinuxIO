package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

const lockFilePerm = 0o600

var fallbackConfigRoot = filepath.Join(version.DataDir, "users")

type StorageMode string

const (
	StorageModeHome     StorageMode = "home"
	StorageModeFallback StorageMode = "fallback"
	StorageModeMemory   StorageMode = "memory"
)

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
	base       string
	mode       StorageMode

	mu         sync.RWMutex
	uiMu       sync.RWMutex
	updateMu   sync.Mutex
	uiUpdateMu sync.Mutex
	cfg        Settings
	ui         UIPreferences
}

// OpenUserStore prefers the authenticated user's home, falls back to
// /var/lib/linuxio, then keeps settings in memory if neither location works.
func OpenUserStore(username string, targetUID, targetGID uint32) (*UserStore, error) {
	owner, err := resolveFileOwnership(targetUID, targetGID)
	if err != nil {
		return nil, err
	}
	homeBase, homeErr := configBase(username)
	return openUserStore(username, targetUID, owner, homeBase, homeErr), nil
}

func openUserStore(username string, targetUID uint32, owner fileOwnership, homeBase string, homeErr error) *UserStore {
	if homeErr == nil {
		store, err := openDiskUserStore(username, homeBase, homeBase, owner, StorageModeHome)
		if err == nil {
			return store
		}
		homeErr = err
	}

	fallbackBase := filepath.Join(fallbackConfigRoot, strconv.FormatUint(uint64(targetUID), 10))
	defaultBase := homeBase
	if defaultBase == "" {
		defaultBase = fallbackBase
	}
	fallbackErr := prepareFallbackConfigBase(fallbackBase, owner)
	if fallbackErr == nil {
		store, err := openDiskUserStore(username, fallbackBase, defaultBase, owner, StorageModeFallback)
		if err == nil {
			slog.Warn("home config store unavailable, using fallback",
				"component", "config",
				"user", username,
				"path", fallbackBase,
				"error", homeErr,
			)
			return store
		}
		fallbackErr = err
	}

	slog.Warn("persistent config stores unavailable, using memory",
		"component", "config",
		"user", username,
		"home_error", homeErr,
		"fallback_error", fallbackErr,
	)
	return &UserStore{
		username: username,
		owner:    owner,
		base:     defaultBase,
		mode:     StorageModeMemory,
		cfg:      *DefaultSettings(defaultBase),
		ui:       DefaultUIPreferences(),
	}
}

func openDiskUserStore(username, configBase, defaultBase string, owner fileOwnership, mode StorageMode) (*UserStore, error) {
	cfgPath := filepath.Join(configBase, cfgFileName)
	uiPath := filepath.Join(configBase, uiCfgFileName)
	store := &UserStore{
		username:   username,
		path:       cfgPath,
		uiPath:     uiPath,
		lockPath:   cfgPath + ".lock",
		uiLockPath: uiPath + ".lock",
		owner:      owner,
		base:       defaultBase,
		mode:       mode,
	}
	if err := withConfigLocksOwned(context.Background(), store.lockPath, store.uiLockPath, owner, func() error {
		if err := initializeLockedOwned(cfgPath, uiPath, defaultBase, owner); err != nil {
			return err
		}
		cfg, err := loadCoreOrQuarantineOwned(cfgPath, defaultBase, owner)
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

func prepareFallbackConfigBase(path string, owner fileOwnership) error {
	root := filepath.Dir(path)
	if err := prepareFallbackConfigRoot(root); err != nil {
		return err
	}

	created := false
	if err := os.Mkdir(path, 0o700); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create fallback config directory: %w", err)
		}
	} else {
		created = true
	}
	if created && owner.enforce {
		if err := os.Chown(path, owner.uid, owner.gid); err != nil {
			return fmt.Errorf("own fallback config directory: %w", err)
		}
	}
	if err := owner.ensureDirectory(path); err != nil {
		return fmt.Errorf("verify fallback config directory: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("set fallback config directory permissions: %w", err)
	}
	return nil
}

func prepareFallbackConfigRoot(root string) error {
	rootCreated := false
	if err := os.Mkdir(root, 0o711); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("create fallback config root: %w", err)
		}
	} else {
		rootCreated = true
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return fmt.Errorf("stat fallback config root: %w", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return fmt.Errorf("fallback config root is not a directory: %s", root)
	}
	if rootInfo.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("fallback config root is writable by group or others: %s", root)
	}
	if rootCreated {
		if err := os.Chmod(root, 0o711); err != nil {
			return fmt.Errorf("set fallback config root permissions: %w", err)
		}
	}
	return nil
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

func (s *UserStore) StorageMode() StorageMode {
	if s == nil {
		return ""
	}
	return s.mode
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

// Update applies a mutation to the latest disk snapshot or the memory store.
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
	var err error
	if s.mode == StorageModeMemory {
		s.mu.RLock()
		current := cloneSettings(&s.cfg)
		s.mu.RUnlock()
		updated, err = applySettingsMutation(ctx, current, mutate)
	} else {
		err = withExclusiveConfigLockOwned(ctx, s.lockPath, s.owner, func() error {
			current, readErr := readCoreLatestOwned(s.path, s.base)
			if readErr != nil {
				return fmt.Errorf("read core config: %w", readErr)
			}
			next, updateErr := applySettingsMutation(ctx, current, mutate)
			if updateErr != nil {
				return updateErr
			}
			if writeErr := writeCoreConfigOwned(s.path, *next, s.owner); writeErr != nil {
				return fmt.Errorf("write core config: %w", writeErr)
			}
			updated = next
			return nil
		})
	}
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cfg = *cloneSettings(updated)
	s.mu.Unlock()
	return cloneSettings(updated), nil
}

func applySettingsMutation(ctx context.Context, current *Settings, mutate func(*Settings) error) (*Settings, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	next := cloneSettings(current)
	if err := mutate(next); err != nil {
		return nil, err
	}
	if errs := ValidateConfig(next); len(errs) > 0 {
		return nil, fmt.Errorf("validate config: %s", strings.Join(errs, "; "))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return next, nil
}

// ReplaceUI validates and replaces the disk or memory UI snapshot. Disk
// replacement deliberately avoids reading or merging the old UI file.
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
	updated := cloneUIPreferences(next)
	var err error
	if s.mode != StorageModeMemory {
		err = withExclusiveUILockOwned(ctx, s.uiLockPath, s.owner, func() error {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			if writeErr := writeUIConfigOwned(s.uiPath, *next, s.owner); writeErr != nil {
				return fmt.Errorf("write UI config: %w", writeErr)
			}
			return nil
		})
	}
	if err != nil {
		return nil, err
	}
	s.uiMu.Lock()
	s.ui = *cloneUIPreferences(updated)
	s.uiMu.Unlock()
	return cloneUIPreferences(updated), nil
}

// errInvalidCoreConfig marks a core document that exists and was read but
// failed to decode or validate. The boot-time loader quarantines only this
// class of failure; symlink, type, and I/O failures are never repaired.
var errInvalidCoreConfig = errors.New("invalid core config")

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
	return nil, fmt.Errorf("%w: %w", errInvalidCoreConfig, parseErr)
}

func quarantineCoreConfig(path, timestamp string) (string, error) {
	basePath := path + ".broken-" + timestamp
	quarantinePath := basePath
	for copyNumber := 2; ; copyNumber++ {
		_, err := os.Lstat(quarantinePath)
		if errors.Is(err, os.ErrNotExist) {
			if renameErr := os.Rename(path, quarantinePath); renameErr != nil {
				return "", renameErr
			}
			return quarantinePath, nil
		}
		if err != nil {
			return "", fmt.Errorf("check quarantine path: %w", err)
		}
		quarantinePath = fmt.Sprintf("%s(%d)", basePath, copyNumber)
	}
}

// loadCoreOrQuarantineOwned is the boot-time core read. A document that fails
// to decode or validate is moved to <path>.broken-<UTC timestamp> (with a
// numbered suffix if needed) and replaced with defaults. One bad edit or a
// downgrade past an unknown field therefore cannot lock the user out; the
// original stays on disk for manual recovery. Every
// other failure is returned unchanged. UserStore.Update never calls this: a
// mutation must not reset a file it could not read.
func loadCoreOrQuarantineOwned(path, base string, owner fileOwnership) (*Settings, error) {
	cfg, err := readCoreLatestOwned(path, base)
	if err == nil {
		return cfg, nil
	}
	if !errors.Is(err, errInvalidCoreConfig) {
		return nil, err
	}
	quarantinePath, renameErr := quarantineCoreConfig(path, time.Now().UTC().Format("20060102T150405Z"))
	if renameErr != nil {
		return nil, errors.Join(err, fmt.Errorf("quarantine core config: %w", renameErr))
	}
	defaults := DefaultSettings(base)
	if writeErr := writeCoreConfigOwned(path, *defaults, owner); writeErr != nil {
		return nil, errors.Join(err, fmt.Errorf("write default core config: %w", writeErr))
	}
	slog.Warn("core config quarantined, defaults written",
		"component", "config",
		"path", path,
		"quarantined_path", quarantinePath,
		"error", err,
	)
	return defaults, nil
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
	slog.Warn("UI config reset to defaults", "component", "config", "path", path, "error", parseErr)
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
