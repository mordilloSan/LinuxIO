package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
)

var errOperationAlreadyRunning = errors.New("indexer operation already running")

type operationLock struct {
	file *os.File
}

func tryAcquireOperationLock(dbPath string) (*operationLock, error) {
	lockPath := operationLockPath(dbPath)
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return nil, fmt.Errorf("create lock directory: %w", err)
	}

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open operation lock: %w", err)
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if closeErr := file.Close(); closeErr != nil {
			return nil, fmt.Errorf("close operation lock after failed acquire: %w", closeErr)
		}
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errOperationAlreadyRunning
		}
		return nil, fmt.Errorf("acquire operation lock: %w", err)
	}
	return &operationLock{file: file}, nil
}

func operationLockPath(dbPath string) string {
	if dbPath == "" {
		dbPath = configfile.Defaults().DBPath
	}
	return dbPath + ".lock"
}

func (l *operationLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	fd := int(l.file.Fd())
	unlockErr := unix.Flock(fd, unix.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
