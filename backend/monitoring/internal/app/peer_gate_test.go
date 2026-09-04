package app

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mordilloSan/LinuxIO/backend/common/peercred"
)

func TestRequireRootPeerRejectsMissingAndNonRoot(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	gate := requireRootPeer(next)

	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("no cred: %d", rec.Code)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(peercred.WithCredForTest(context.Background(), 1000))
	rec = httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("uid 1000: %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(peercred.WithCredForTest(context.Background(), 0))
	rec = httptest.NewRecorder()
	gate.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("uid 0: %d", rec.Code)
	}
}

// TestStartContextRootOnlyControlSocket covers the control-socket wiring:
// the explicit socket mode, ConnContext attaching peer credentials, and the
// gate rejecting a non-root peer.
func TestStartContextRootOnlyControlSocket(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "control.sock")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, err := New(ctx, tmpDir)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() {
		errCh <- a.StartContext(ctx, RunOptions{
			Listeners: []ListenerOptions{{
				Name:     "control",
				Address:  "unix:" + socketPath,
				APIs:     []string{"metrics", "commands"},
				Mode:     0o600,
				RootOnly: true,
			}},
			CollectorInterval: 5 * time.Minute,
		})
	}()

	require.Eventually(t, func() bool {
		return testListenAddr(a) != ""
	}, 20*time.Second, 50*time.Millisecond)

	// Without ConnContext the gate could never see a uid, and a non-root test
	// host cannot tell that apart from a rejected peer over HTTP alone.
	a.runtimeMu.RLock()
	require.Len(t, a.httpRuntimes, 1)
	assert.NotNil(t, a.httpRuntimes[0].server.ConnContext, "root-only listener must attach peer credentials")
	a.runtimeMu.RUnlock()

	info, err := os.Stat(socketPath)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

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

	// The gate reads the real peer uid, so the expected status depends on who
	// runs the test.
	want := http.StatusForbidden
	if os.Getuid() == 0 {
		want = http.StatusOK
	}
	assert.Equal(t, want, resp.StatusCode)

	cancel()
	require.NoError(t, <-errCh)
}
