package dbus

import (
	"errors"
	"net"
	"sync"
	"time"
)

// systemDBusMu ensures D-Bus calls are synchronized (one at a time)
var systemDBusMu sync.Mutex

// RetryOnceIfClosed retries a D-Bus operation once if the connection was closed
func RetryOnceIfClosed(initialErr error, do func() error) error {
	if initialErr == nil {
		err := do()
		if errors.Is(err, net.ErrClosed) {
			time.Sleep(150 * time.Millisecond)
			return do()
		}
		return err
	}
	if errors.Is(initialErr, net.ErrClosed) {
		time.Sleep(150 * time.Millisecond)
		return do()
	}
	return initialErr
}
