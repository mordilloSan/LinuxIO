// Package dockerclient owns the bridge's shared Docker client. Callers pair
// Get with Release so an idle client is closed and its resources are freed.
package dockerclient

import (
	"log/slog"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/moby/moby/client"
)

const idleTimeout = 5 * time.Minute

var (
	mu        sync.Mutex
	shared    *client.Client
	refs      int
	idleTimer *time.Timer
)

// Get returns the shared Docker client, creating it if necessary.
// Callers must call Release when done so the idle timer can run.
func Get() (*client.Client, error) {
	mu.Lock()
	defer mu.Unlock()

	// Cancel any pending idle close.
	if idleTimer != nil {
		idleTimer.Stop()
		idleTimer = nil
	}

	if shared == nil {
		cli, err := client.New(client.FromEnv)
		if err != nil {
			return nil, err
		}
		shared = cli
	}

	refs++
	return shared, nil
}

// Release decrements the reference count. When the count reaches zero
// a timer is started; if no new request arrives within idleTimeout the
// client is closed and its resources (connection pool, goroutines) are freed.
func Release(_ *client.Client) {
	mu.Lock()
	defer mu.Unlock()

	if refs > 0 {
		refs--
	}
	if refs > 0 {
		return
	}

	// No active callers — schedule a close after the idle period.
	idleTimer = time.AfterFunc(idleTimeout, func() {
		mu.Lock()
		defer mu.Unlock()
		if refs > 0 || shared == nil {
			idleTimer = nil
			return
		}
		_ = shared.Close()
		shared = nil
		idleTimer = nil
		slog.Debug("docker client closed after idle timeout", "component", "docker", "mode", idleTimeout.String())
		// Force GC and return freed pages to the OS immediately.
		go func() {
			runtime.GC()
			debug.FreeOSMemory()
		}()
	})
}
