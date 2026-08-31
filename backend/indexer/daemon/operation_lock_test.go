package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestOperationLockExcludesConcurrentAcquire(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")

	lock, acquireErr := tryAcquireOperationLock(dbPath)
	if acquireErr != nil {
		t.Fatalf("tryAcquireOperationLock first: %v", acquireErr)
	}

	if _, err := tryAcquireOperationLock(dbPath); !errors.Is(err, errOperationAlreadyRunning) {
		t.Fatalf("second tryAcquireOperationLock err = %v, want errOperationAlreadyRunning", err)
	}

	if err := lock.Close(); err != nil {
		t.Fatalf("close first lock: %v", err)
	}

	lock, acquireErr = tryAcquireOperationLock(dbPath)
	if acquireErr != nil {
		t.Fatalf("tryAcquireOperationLock after close: %v", acquireErr)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("close second lock: %v", err)
	}
}

func TestOperationLockDoubleCloseIsSafe(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "index.db")

	lock, err := tryAcquireOperationLock(dbPath)
	if err != nil {
		t.Fatalf("tryAcquireOperationLock: %v", err)
	}

	if err := lock.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := lock.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestOperationLockNilReceiver(t *testing.T) {
	var lock *operationLock
	if err := lock.Close(); err != nil {
		t.Fatalf("nil-receiver Close: %v", err)
	}
}

func TestOperationLockCreatesParentDirectory(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "nested", "subdir", "index.db")

	lock, err := tryAcquireOperationLock(dbPath)
	if err != nil {
		t.Fatalf("tryAcquireOperationLock: %v", err)
	}
	t.Cleanup(func() {
		if err := lock.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})

	if _, err := os.Stat(dbPath + ".lock"); err != nil {
		t.Fatalf("lock file missing at %q: %v", dbPath+".lock", err)
	}
}

func TestOperationLockOpenFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; file mode permissions don't apply")
	}

	parent := t.TempDir()
	if err := os.Chmod(parent, 0o500); err != nil {
		t.Fatalf("chmod parent: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(parent, 0o700); err != nil {
			t.Errorf("restore parent mode: %v", err)
		}
	})

	dbPath := filepath.Join(parent, "index.db")
	_, err := tryAcquireOperationLock(dbPath)
	if err == nil {
		t.Fatalf("expected acquire to fail under read-only parent dir")
	}
	if errors.Is(err, errOperationAlreadyRunning) {
		t.Fatalf("expected non-contention error, got %v", err)
	}
}
