package docker

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/docker/docker/client"
)

const dockerIdleTimeout = 5 * time.Minute

var (
	dockerClientMu   sync.Mutex
	dockerClient     *client.Client
	dockerClientRefs int
	dockerIdleTimer  *time.Timer
	ensureNetOnce    sync.Once
	// watchtowerOnce fires once per bridge session.
	watchtowerOnce sync.Once
)

// getClient returns the shared Docker client, creating it if necessary.
// Callers must call releaseClient when done so the idle timer can run.
func getClient() (*client.Client, error) {
	dockerClientMu.Lock()
	defer dockerClientMu.Unlock()

	// Cancel any pending idle close.
	if dockerIdleTimer != nil {
		dockerIdleTimer.Stop()
		dockerIdleTimer = nil
	}

	if dockerClient == nil {
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return nil, err
		}
		dockerClient = cli
		// Ensure the shared Docker network exists once per client lifetime.
		go ensureNetOnce.Do(func() {
			ctx, cancel := detachedDockerStartupContext()
			defer cancel()
			EnsureLinuxIONetwork(ctx)
		})
	}

	dockerClientRefs++
	return dockerClient, nil
}

// releaseClient decrements the reference count. When the count reaches zero
// a timer is started; if no new request arrives within dockerIdleTimeout the
// client is closed and its resources (connection pool, goroutines) are freed.
func releaseClient(_ *client.Client) {
	dockerClientMu.Lock()
	defer dockerClientMu.Unlock()

	if dockerClientRefs > 0 {
		dockerClientRefs--
	}
	if dockerClientRefs > 0 {
		return
	}

	// No active callers — schedule a close after the idle period.
	dockerIdleTimer = time.AfterFunc(dockerIdleTimeout, func() {
		dockerClientMu.Lock()
		defer dockerClientMu.Unlock()
		if dockerClientRefs > 0 || dockerClient == nil {
			dockerIdleTimer = nil
			return
		}
		_ = dockerClient.Close()
		dockerClient = nil
		dockerIdleTimer = nil
		// Allow EnsureLinuxIONetwork to run again for the next client.
		ensureNetOnce = sync.Once{}
		slog.Debug("docker client closed after idle timeout", "component", "docker", "mode", dockerIdleTimeout.String())
		// Force GC and return freed pages to the OS immediately.
		go func() {
			runtime.GC()
			debug.FreeOSMemory()
		}()
	})
}

// CheckDockerAvailability verifies that Docker is installed and accessible.
// It uses a short-lived throwaway client so that repeated capability polls
// (which the frontend runs every ~minute) do not reset the shared client's
// idle timer and prevent it from ever being released.
func CheckDockerAvailability(ctx context.Context) (bool, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return false, fmt.Errorf("docker client error: %w", err)
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		return false, fmt.Errorf("docker daemon not accessible: %w", err)
	}
	return true, nil
}

// detachedDockerStartupContext bounds Docker setup work started from client creation.
func detachedDockerStartupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}
