package docker

import (
	"context"
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	"github.com/docker/docker/client"
	"github.com/mordilloSan/go-logger/logger"
)

const dockerIdleTimeout = 5 * time.Minute

var (
	dockerClientMu   sync.Mutex
	dockerClient     *client.Client
	dockerClientRefs int
	dockerIdleTimer  *time.Timer
	ensureNetOnce    sync.Once
	// watchtowerOnce fires once per bridge session on the first Docker operation.
	watchtowerOnce sync.Once
	// sessionUsername is set by RegisterHandlers and read by getClient.
	sessionUsername string
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
		go ensureNetOnce.Do(EnsureLinuxIONetwork)
		// Sync Watchtower once per session on first Docker operation.
		go watchtowerOnce.Do(func() { SyncWatchtowerStack(sessionUsername) })
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
		logger.Debugf("docker client closed after %s idle", dockerIdleTimeout)
		// Force GC and return freed pages to the OS immediately.
		go func() {
			runtime.GC()
			debug.FreeOSMemory()
		}()
	})
}

// dockerAvailable verifies that Docker client initialization and daemon ping both work.
func dockerAvailable() (bool, error) {
	cli, err := getClient()
	if err != nil {
		return false, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	if _, err := cli.Ping(context.Background()); err != nil {
		return false, fmt.Errorf("docker daemon not accessible: %w", err)
	}

	return true, nil
}

// CheckDockerAvailability verifies that Docker is installed and accessible
func CheckDockerAvailability() (bool, error) {
	ok, err := dockerAvailable()
	if err != nil {
		logger.Infof("docker service not available")
		return false, err
	}

	logger.Infof("docker service available")
	return ok, nil
}
