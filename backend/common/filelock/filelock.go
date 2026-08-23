package filelock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const defaultRetryDelay = 250 * time.Millisecond

var ErrTimeout = errors.New("lock timeout exceeded")

type options struct {
	mode       os.FileMode
	dirMode    os.FileMode
	timeout    time.Duration
	retryDelay time.Duration
	ownership  *lockOwnership
}

type lockOwnership struct {
	uid int
	gid int
}

// Option configures lock acquisition.
type Option func(*options)

// WithPermissions sets the lock file permissions used when the file is created.
func WithPermissions(mode os.FileMode) Option {
	return func(o *options) {
		o.mode = mode
	}
}

// WithDirPermissions sets the permissions used when creating the lock directory.
func WithDirPermissions(mode os.FileMode) Option {
	return func(o *options) {
		o.dirMode = mode
	}
}

// WithTimeout bounds how long acquisition waits. A zero timeout waits until the
// context is cancelled.
func WithTimeout(timeout time.Duration) Option {
	return func(o *options) {
		o.timeout = timeout
	}
}

// WithRetryDelay sets how often a contended lock is retried.
func WithRetryDelay(delay time.Duration) Option {
	return func(o *options) {
		o.retryDelay = delay
	}
}

// WithOwnership assigns uid/gid to the opened lock file before acquiring the
// advisory lock. The option is opt-in; without it, lock-file ownership keeps
// the existing behavior.
func WithOwnership(uid, gid int) Option {
	return func(o *options) {
		o.ownership = &lockOwnership{uid: uid, gid: gid}
	}
}

// RunExclusive holds an exclusive advisory lock while fn runs.
func RunExclusive(ctx context.Context, path string, fn func() error, opts ...Option) error {
	if fn == nil {
		return errors.New("lock function is nil")
	}
	release, err := AcquireExclusive(ctx, path, opts...)
	if err != nil {
		return err
	}
	fnErr := fn()
	return errors.Join(fnErr, release())
}

// AcquireExclusive takes an exclusive advisory lock on path. The returned
// release function unlocks and closes the lock file. It is safe to call release
// more than once.
func AcquireExclusive(ctx context.Context, path string, opts ...Option) (func() error, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	cfg := options{
		mode:       0o600,
		dirMode:    0o755,
		retryDelay: defaultRetryDelay,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.retryDelay <= 0 {
		cfg.retryDelay = defaultRetryDelay
	}
	if cfg.ownership != nil {
		if err := validateOwnership(cfg.ownership.uid, cfg.ownership.gid); err != nil {
			return nil, fmt.Errorf("validate lock ownership: %w", err)
		}
	}

	f, err := openLockFile(path, cfg.mode, cfg.dirMode)
	if err != nil {
		return nil, err
	}
	if cfg.ownership != nil {
		if chownErr := f.Chown(cfg.ownership.uid, cfg.ownership.gid); chownErr != nil {
			_ = f.Close()
			return nil, fmt.Errorf("chown lock %q to %d:%d: %w", path, cfg.ownership.uid, cfg.ownership.gid, chownErr)
		}
	}
	release, err := waitExclusive(ctx, f, path, cfg)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return release, nil
}

func validateOwnership(uid, gid int) error {
	if uid < 0 {
		return fmt.Errorf("uid %d is invalid: must be non-negative", uid)
	}
	if gid < 0 {
		return fmt.Errorf("gid %d is invalid: must be non-negative", gid)
	}
	return nil
}

func openLockFile(path string, mode, dirMode os.FileMode) (*os.File, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("lock path is empty")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("mkdir %q: %w", dir, err)
	}

	fd, err := unix.Open(
		path,
		unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW,
		uint32(mode),
	)
	if err != nil {
		return nil, fmt.Errorf("open lock %s: %w", path, err)
	}
	return os.NewFile(uintptr(fd), path), nil
}

func waitExclusive(ctx context.Context, f *os.File, path string, cfg options) (func() error, error) {
	deadline := time.Time{}
	if cfg.timeout > 0 {
		deadline = time.Now().Add(cfg.timeout)
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		lockErr := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if lockErr == nil {
			return releaseFunc(f), nil
		}
		if err := lockAttemptError(path, lockErr); err != nil {
			return nil, err
		}
		wait, err := retryWait(path, deadline, cfg.retryDelay)
		if err != nil {
			return nil, err
		}
		if err := sleepContext(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func lockAttemptError(path string, err error) error {
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return nil
	}
	return fmt.Errorf("lock %s: %w", path, err)
}

func retryWait(path string, deadline time.Time, retryDelay time.Duration) (time.Duration, error) {
	if deadline.IsZero() {
		return retryDelay, nil
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0, fmt.Errorf("lock %s: %w", path, ErrTimeout)
	}
	if remaining < retryDelay {
		return remaining, nil
	}
	return retryDelay, nil
}

func sleepContext(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func releaseFunc(f *os.File) func() error {
	var once sync.Once
	var err error
	return func() error {
		once.Do(func() {
			err = errors.Join(
				unix.Flock(int(f.Fd()), unix.LOCK_UN),
				f.Close(),
			)
		})
		return err
	}
}
