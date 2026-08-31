package daemon

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAuthorizeTransportMiddlewareAllowsRootUnix(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			called := false
			handler := authorizeTransportMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				called = true
				w.WriteHeader(http.StatusNoContent)
			}))

			req := httptest.NewRequest(method, "/status", nil)
			ctx := withConnectionKind(req.Context(), connectionKindUnix)
			ctx = withPeerCred(ctx, peerCred{uid: 0, gid: 0})
			req = req.WithContext(ctx)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if !called {
				t.Fatal("inner handler was not called")
			}
			if rr.Code != http.StatusNoContent {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
			}
		})
	}
}

func TestAuthorizeTransportMiddlewareAllowsReadOnlyTCP(t *testing.T) {
	for _, path := range []string{
		"/openapi.json", "/status", "/search", "/dirsize", "/entrycount",
		"/subfolders", "/entries", "/config",
	} {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			t.Run(method+" "+path, func(t *testing.T) {
				called := false
				handler := authorizeTransportMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					called = true
					w.WriteHeader(http.StatusNoContent)
				}))

				req := httptest.NewRequest(method, path, nil)
				req = req.WithContext(withConnectionKind(req.Context(), connectionKindTCP))
				rr := httptest.NewRecorder()
				handler.ServeHTTP(rr, req)

				if !called || rr.Code != http.StatusNoContent {
					t.Fatalf("called = %t, status = %d; want true, %d", called, rr.Code, http.StatusNoContent)
				}
			})
		}
	}
}

func TestAuthorizeTransportMiddlewareRejectsTCPMutations(t *testing.T) {
	for _, request := range []struct {
		method string
		path   string
	}{
		{method: http.MethodPost, path: "/index"},
		{method: http.MethodPut, path: "/config"},
		{method: http.MethodDelete, path: "/delete"},
		{method: http.MethodGet, path: "/index"},
	} {
		t.Run(request.method+" "+request.path, func(t *testing.T) {
			called := false
			handler := authorizeTransportMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			}))

			req := httptest.NewRequest(request.method, request.path, nil)
			req = req.WithContext(withConnectionKind(req.Context(), connectionKindTCP))
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if called || rr.Code != http.StatusForbidden {
				t.Fatalf("called = %t, status = %d; want false, %d", called, rr.Code, http.StatusForbidden)
			}
			if !strings.Contains(rr.Body.String(), "TCP listener is read-only") {
				t.Fatalf("body = %q, want read-only message", rr.Body.String())
			}
		})
	}
}

func TestAuthorizeTransportMiddlewareRejectsUnixWithoutPeerCred(t *testing.T) {
	called := false
	handler := authorizeTransportMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/vacuum", nil)
	req = req.WithContext(withConnectionKind(req.Context(), connectionKindUnix))
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Fatal("inner handler was called")
	}
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
	if !strings.Contains(rr.Body.String(), "root privileges") {
		t.Fatalf("body = %q, want root privileges message", rr.Body.String())
	}
}

func TestAuthorizeTransportMiddlewareRejectsNonRootUnix(t *testing.T) {
	called := false
	handler := authorizeTransportMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/vacuum", nil)
	ctx := withConnectionKind(req.Context(), connectionKindUnix)
	ctx = withPeerCred(ctx, peerCred{uid: 1000, gid: 1000})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Fatal("inner handler was called")
	}
	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusForbidden)
	}
	if !strings.Contains(rr.Body.String(), "root privileges") {
		t.Fatalf("body = %q, want root privileges message", rr.Body.String())
	}
}

func TestAuthorizeTransportMiddlewareAllowsMutationFromRootUnix(t *testing.T) {
	called := false
	handler := authorizeTransportMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/vacuum", nil)
	ctx := withConnectionKind(req.Context(), connectionKindUnix)
	ctx = withPeerCred(ctx, peerCred{uid: 0, gid: 0})
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Fatal("inner handler was not called")
	}
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNoContent)
	}
}

//nolint:gocognit // Socket setup, peer exchange, and cleanup form one integration scenario.
func TestReadUnixPeerCred(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("SO_PEERCRED is Linux-specific")
	}

	socketPath := filepath.Join(t.TempDir(), "peer.sock")
	ln, listenErr := net.Listen("unix", socketPath)
	if listenErr != nil {
		t.Fatalf("listen unix: %v", listenErr)
	}
	defer func() {
		if closeErr := ln.Close(); closeErr != nil {
			t.Fatalf("close listener: %v", closeErr)
		}
	}()

	credCh := make(chan peerCred, 1)
	errCh := make(chan error, 1)
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			errCh <- acceptErr
			return
		}

		uc, ok := conn.(*net.UnixConn)
		if !ok {
			if closeErr := conn.Close(); closeErr != nil {
				errCh <- fmt.Errorf("close accepted connection: %w", closeErr)
				return
			}
			errCh <- fmt.Errorf("accepted connection is %T, want *net.UnixConn", conn)
			return
		}
		cred, err := readUnixPeerCred(uc)
		if err != nil {
			if closeErr := conn.Close(); closeErr != nil {
				errCh <- fmt.Errorf("close accepted connection after peer cred error: %w", closeErr)
				return
			}
			errCh <- err
			return
		}
		if err := conn.Close(); err != nil {
			errCh <- fmt.Errorf("close accepted connection: %w", err)
			return
		}
		credCh <- cred
	}()

	client, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("dial unix: %v", err)
	}
	defer func() {
		if err := client.Close(); err != nil {
			t.Fatalf("close client connection: %v", err)
		}
	}()

	select {
	case err := <-errCh:
		t.Fatalf("accept/read peer cred: %v", err)
	case cred := <-credCh:
		if cred.uid != uint32(os.Geteuid()) {
			t.Fatalf("uid = %d, want %d", cred.uid, os.Geteuid())
		}
		if cred.gid != uint32(os.Getegid()) {
			t.Fatalf("gid = %d, want %d", cred.gid, os.Getegid())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for peer credentials")
	}
}

func TestGetUnixListenerCreatesRootOnlySocket(t *testing.T) {
	t.Setenv("LISTEN_PID", "")
	t.Setenv("LISTEN_FDS", "")

	socketPath := filepath.Join(t.TempDir(), "indexer.sock")
	d := &daemon{
		cfg: DaemonConfig{
			SocketPath: socketPath,
		},
	}

	l, err := d.getUnixListener()
	if err != nil {
		t.Fatalf("get unix listener: %v", err)
	}
	defer func() {
		if closeErr := l.Close(); closeErr != nil {
			t.Fatalf("close listener: %v", closeErr)
		}
	}()

	info, err := os.Stat(socketPath)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("socket mode = %o, want 600", mode)
	}
}

func TestConfiguredTCPRequiresSocketActivation(t *testing.T) {
	t.Setenv("LISTEN_PID", "")
	t.Setenv("LISTEN_FDS", "")

	reserved, listenErr := net.Listen("tcp", "127.0.0.1:0")
	if listenErr != nil {
		t.Fatalf("reserve TCP address: %v", listenErr)
	}
	tcpAddr := reserved.Addr().String()
	if closeErr := reserved.Close(); closeErr != nil {
		t.Fatalf("release TCP address: %v", closeErr)
	}

	socketPath := filepath.Join(t.TempDir(), "indexer.sock")
	d := &daemon{cfg: DaemonConfig{SocketPath: socketPath, ListenAddr: tcpAddr}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- d.startHTTP(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		if serveErr := <-done; serveErr != nil {
			t.Errorf("stop HTTP server: %v", serveErr)
		}
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("unix", socketPath, 50*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Unix listener did not start: %v", dialErr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	conn, dialErr := net.DialTimeout("tcp", tcpAddr, 100*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		t.Fatal("configured TCP address was bound without socket activation")
	}
}

func TestListenersFromFilesAcceptsUnixAndTCP(t *testing.T) {
	unixListener, err := net.Listen("unix", filepath.Join(t.TempDir(), "indexer.sock"))
	if err != nil {
		t.Fatalf("listen Unix: %v", err)
	}
	defer unixListener.Close()
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen TCP: %v", err)
	}
	defer tcpListener.Close()

	unixSocket, ok := unixListener.(*net.UnixListener)
	if !ok {
		t.Fatalf("Unix listener type = %T", unixListener)
	}
	unixFile, err := unixSocket.File()
	if err != nil {
		t.Fatalf("Unix listener file: %v", err)
	}
	defer unixFile.Close()
	tcpSocket, ok := tcpListener.(*net.TCPListener)
	if !ok {
		t.Fatalf("TCP listener type = %T", tcpListener)
	}
	tcpFile, err := tcpSocket.File()
	if err != nil {
		t.Fatalf("TCP listener file: %v", err)
	}
	defer tcpFile.Close()

	listeners, err := listenersFromFiles([]*os.File{unixFile, tcpFile})
	if err != nil {
		t.Fatalf("listenersFromFiles: %v", err)
	}
	defer closeListeners(listeners)
	activated, err := loadActivatedListenersFrom(listeners)
	if err != nil {
		t.Fatalf("load activated listeners: %v", err)
	}
	if activated.unix == nil || activated.tcp == nil {
		t.Fatalf("activated listeners = %#v", activated)
	}
}
