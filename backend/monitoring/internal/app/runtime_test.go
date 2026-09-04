package app

import (
	"context"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartContextCreatesDatabaseAndServesAPI(t *testing.T) {
	tmpDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, err := New(ctx, tmpDir)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- a.StartContext(ctx, RunOptions{
			Listeners:         []ListenerOptions{{Name: "metrics", Address: "127.0.0.1:0", APIs: []string{"metrics"}, Plugins: []string{"cpu"}}},
			CollectorInterval: 5 * time.Minute,
		})
	}()

	require.Eventually(t, func() bool {
		return testListenAddr(a) != ""
	}, 20*time.Second, 50*time.Millisecond)
	listenAddr := testListenAddr(a)
	assert.Equal(t, []string{"cpu"}, a.Listeners()[0].Plugins)

	_, err = os.Stat(tmpDir + "/metrics.db")
	require.NoError(t, err)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + listenAddr + "/api/v1/all")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), `"cpu":`)

	// Age the recorded collector tick well past twice the 5 minute collector
	// interval so /healthz reports the daemon as stale.
	a.lastCollectedMs.Store(time.Now().Add(-time.Hour).UnixMilli())

	resp, err = client.Get("http://" + listenAddr + "/healthz")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	assert.Contains(t, string(body), `"healthy":false`)

	cancel()
	require.NoError(t, <-errCh)
}

func TestStartContextUnixSocket(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "agent.sock")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, err := New(ctx, tmpDir)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- a.StartContext(ctx, RunOptions{
			Listeners:         []ListenerOptions{{Name: "metrics", Address: "unix:" + socketPath, APIs: []string{"metrics"}}},
			CollectorInterval: 5 * time.Minute,
		})
	}()

	require.Eventually(t, func() bool {
		return testListenAddr(a) != ""
	}, 20*time.Second, 50*time.Millisecond)
	assert.Equal(t, socketPath, testListenAddr(a))

	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}
	resp, err := client.Get("http://unix/api/v1/meta")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, string(body), `"version":`)
	assert.Contains(t, string(body), `"db_size_bytes":`)
	assert.Positive(t, a.StatusMeta().DBSizeBytes, "the start-up tick created metrics.db")

	cancel()
	require.NoError(t, <-errCh)
	// The unix listener unlinks its socket file on shutdown.
	assert.NoFileExists(t, socketPath)
}

func TestStartContextDisabledHTTP(t *testing.T) {
	tmpDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, err := New(ctx, tmpDir)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- a.StartContext(ctx, RunOptions{
			CollectorInterval: 5 * time.Minute,
		})
	}()

	// The collector still persists snapshots without the HTTP server. The last
	// collected timestamp is recorded after the first snapshot is written, so
	// waiting for it means the initial collection is done and the event loop
	// is up.
	require.Eventually(t, func() bool {
		_, ok := a.LastCollected()
		return ok
	}, 20*time.Second, 50*time.Millisecond)
	_, err = os.Stat(tmpDir + "/metrics.db")
	require.NoError(t, err)
	assert.Empty(t, a.Listeners())

	cancel()
	require.NoError(t, <-errCh)
}

func TestReplaceIntervalUpdate(t *testing.T) {
	buffered := make(chan time.Duration, 1)
	buffered <- time.Minute
	replaceIntervalUpdate(buffered, 2*time.Minute)
	assert.Equal(t, 2*time.Minute, <-buffered)

	updates := make(chan time.Duration)
	done := make(chan struct{})
	go func() {
		replaceIntervalUpdate(updates, time.Minute)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("interval update blocked without a receiver")
	}
}

func testListenAddr(a *App) string {
	listeners := a.Listeners()
	if len(listeners) == 0 {
		return ""
	}
	return listeners[0].EffectiveAddress
}

// The status and summary responses read the SMART interval from request
// goroutines while config reloads rewrite it under the app lock; the read must
// take the same lock or the race detector reports it.
func TestSmartRefreshIntervalStringTakesTheAppLock(t *testing.T) {
	a := &App{smartManager: &SmartManager{refreshInterval: time.Hour}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := range 100 {
			a.Lock()
			a.smartManager.refreshInterval = time.Duration(i+1) * time.Minute
			a.Unlock()
		}
	}()
	for range 100 {
		assert.NotEmpty(t, a.smartRefreshIntervalString())
	}
	<-done
}

func TestHistoryIntervalsReachTheStoreAndConfigMeta(t *testing.T) {
	tmpDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, err := New(ctx, tmpDir)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- a.StartContext(ctx, RunOptions{
			CollectorInterval: 5 * time.Minute,
			History:           "cpu,mem",
			HistorySet:        true,
			HistoryIntervals:  map[string]time.Duration{"mem": 10 * time.Minute},
		})
	}()
	require.Eventually(t, func() bool {
		_, ok := a.LastCollected()
		return ok
	}, 20*time.Second, 50*time.Millisecond)
	assert.Equal(t, map[string]string{"mem": "10m0s"}, a.configInfo().HistoryIntervals)

	// The start-up tick wrote both plugins; two more ticks add two cpu rows
	// and, at every second tick, one mem row.
	require.NoError(t, a.collectAndPersist(ctx, time.Now().UTC()))
	require.NoError(t, a.collectAndPersist(ctx, time.Now().UTC()))
	now := time.Now().UTC().UnixMilli()
	cpu, err := a.store.PluginHistory(ctx, "cpu", "1m", 0, now, 10)
	require.NoError(t, err)
	mem, err := a.store.PluginHistory(ctx, "mem", "1m", 0, now, 10)
	require.NoError(t, err)
	assert.Len(t, cpu, 3)
	assert.Len(t, mem, 2)

	err = a.ReloadRuntime(ReloadOptions{
		CollectorInterval: 5 * time.Minute,
		History:           "cpu,mem",
		HistorySet:        true,
		HistoryIntervals:  map[string]time.Duration{"mem": 7 * time.Minute},
	})
	require.Error(t, err, "reload must reject an interval that is not a multiple of the tick")

	require.NoError(t, a.ReloadRuntime(ReloadOptions{
		CollectorInterval: 5 * time.Minute,
		History:           "cpu,mem",
		HistorySet:        true,
	}))
	assert.Empty(t, a.configInfo().HistoryIntervals)
	assert.NotNil(t, a.configInfo().HistoryIntervals)

	cancel()
	require.NoError(t, <-errCh)
}
