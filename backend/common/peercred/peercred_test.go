package peercred

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
