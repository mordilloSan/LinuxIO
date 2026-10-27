package peercred

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestConnContextAttachesOwnUID(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "s.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan uint32, 1)
	srv := &http.Server{
		ConnContext: ConnContext,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			uid, ok := RequestUID(r)
			if !ok {
				t.Error("no peer uid on request")
			}
			got <- uid
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })

	client := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", sock)
	}}}
	resp, err := client.Get("http://unix/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if uid := <-got; uid != uint32(os.Getuid()) {
		t.Fatalf("uid = %d, want %d", uid, os.Getuid())
	}
}

func TestUIDAbsentOnPlainContext(t *testing.T) {
	if _, ok := UID(httptest.NewRequest(http.MethodGet, "/", nil).Context()); ok {
		t.Fatal("expected no uid")
	}
}

func TestReadReturnsOwnCredentialsOverSocketpair(t *testing.T) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	var conns []*net.UnixConn
	for _, fd := range fds {
		f := os.NewFile(uintptr(fd), "socketpair")
		c, connErr := net.FileConn(f)
		_ = f.Close()
		if connErr != nil {
			t.Fatal(connErr)
		}
		t.Cleanup(func() { _ = c.Close() })
		uc, ok := c.(*net.UnixConn)
		if !ok {
			t.Fatalf("FileConn returned %T, want *net.UnixConn", c)
		}
		conns = append(conns, uc)
	}

	cred, err := Read(conns[0])
	if err != nil {
		t.Fatal(err)
	}
	want := Cred{UID: uint32(os.Getuid()), GID: uint32(os.Getgid()), PID: int32(os.Getpid())}
	if cred != want {
		t.Fatalf("Read = %+v, want %+v", cred, want)
	}
}
