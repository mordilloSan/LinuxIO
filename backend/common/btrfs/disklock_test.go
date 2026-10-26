package btrfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockDisk(t *testing.T) {
	device := filepath.Join(t.TempDir(), "sdb")
	if err := os.WriteFile(device, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	unlock, err := LockDisk(context.Background(), device)
	if err != nil {
		t.Fatal(err)
	}

	// udevd tries the same lock from its own open of the disk.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if _, err = LockDisk(ctx, device); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second lock: err = %v, want the deadline", err)
	}

	unlock()
	ctx, cancel = context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if unlock, err = LockDisk(ctx, device); err != nil {
		t.Fatalf("lock after unlock: %v", err)
	}
	unlock()
}
