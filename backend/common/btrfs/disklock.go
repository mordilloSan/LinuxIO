package btrfs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// LockDisk takes the BSD lock systemd-udevd honours on a whole disk, so udev
// neither re-reads its partition table nor probes it until unlock (see
// https://systemd.io/BLOCK_DEVICE_LOCKING/). Callers hold it across creating,
// formatting and mounting a partition; the table functions do not take it,
// since a second lock from this process would wait for the first.
func LockDisk(ctx context.Context, device string) (unlock func(), err error) {
	// Read-only on purpose: closing a descriptor opened for writing fires
	// udev's IN_CLOSE_WRITE watch, which re-reads the partition table.
	f, err := os.Open(device)
	if err != nil {
		return nil, err
	}
	for {
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { f.Close() }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			f.Close()
			return nil, fmt.Errorf("lock %s: %w", device, err)
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, fmt.Errorf("lock %s: %w", device, ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
}
