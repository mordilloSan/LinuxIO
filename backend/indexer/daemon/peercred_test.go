package daemon

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuthorizeTransportMiddlewareRequiresRootUnix(t *testing.T) {
	withUnixConnection := func(ctx context.Context) context.Context {
		return context.WithValue(ctx, connectionKindContextKey{}, true)
	}
	for _, test := range []struct {
		name    string
		context context.Context
		want    int
	}{
		{name: "root Unix", context: withPeerCred(withUnixConnection(context.Background()), peerCred{uid: 0}), want: http.StatusNoContent},
		{name: "non-root Unix", context: withPeerCred(withUnixConnection(context.Background()), peerCred{uid: 1000}), want: http.StatusForbidden},
		{name: "missing transport", context: context.Background(), want: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := authorizeTransportMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			req := httptest.NewRequest(http.MethodGet, "/status", nil).WithContext(test.context)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, req)
			if rr.Code != test.want {
				t.Fatalf("status = %d, want %d", rr.Code, test.want)
			}
		})
	}
}

func TestReadUnixPeerCred(t *testing.T) {
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(t.TempDir(), "indexer.sock"), Net: "unix"})
	if err != nil {
		t.Fatalf("listen Unix: %v", err)
	}
	defer listener.Close()
	credentials := make(chan peerCred, 1)
	errors := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.AcceptUnix()
		if acceptErr != nil {
			errors <- acceptErr
			return
		}
		defer conn.Close()
		cred, credErr := readUnixPeerCred(conn)
		if credErr != nil {
			errors <- credErr
			return
		}
		credentials <- cred
	}()
	unixAddr, ok := listener.Addr().(*net.UnixAddr)
	if !ok {
		t.Fatalf("listener address has type %T, want *net.UnixAddr", listener.Addr())
	}
	client, err := net.DialUnix("unix", nil, unixAddr)
	if err != nil {
		t.Fatalf("dial Unix: %v", err)
	}
	defer client.Close()
	select {
	case err := <-errors:
		t.Fatalf("read peer cred: %v", err)
	case cred := <-credentials:
		if cred.uid != uint32(os.Geteuid()) || cred.gid != uint32(os.Getegid()) {
			t.Fatalf("credentials = %+v, want uid=%d gid=%d", cred, os.Geteuid(), os.Getegid())
		}
	case <-time.After(time.Second):
		t.Fatal("timed out reading peer credentials")
	}
}

func TestStartHTTPRequiresSocketActivation(t *testing.T) {
	t.Setenv("LISTEN_PID", "")
	t.Setenv("LISTEN_FDS", "")
	if err := (&daemon{}).startHTTP(context.Background()); err == nil || !strings.Contains(err.Error(), "socket activation") {
		t.Fatalf("startHTTP error = %v", err)
	}
}
