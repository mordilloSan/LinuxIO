package dbus

import (
	"strings"
	"sync"
	"time"
)

// systemDBusMu ensures D-Bus calls are synchronized (one at a time)
var systemDBusMu sync.Mutex

// RetryOnceIfClosed retries a D-Bus operation once if the connection was closed
func RetryOnceIfClosed(initialErr error, do func() error) error {
	if initialErr == nil {
		err := do()
		if err != nil && strings.Contains(err.Error(), "use of closed network connection") {
			time.Sleep(150 * time.Millisecond)
			return do()
		}
		return err
	}
	if strings.Contains(initialErr.Error(), "use of closed network connection") {
		time.Sleep(150 * time.Millisecond)
		return do()
	}
	return initialErr
}
