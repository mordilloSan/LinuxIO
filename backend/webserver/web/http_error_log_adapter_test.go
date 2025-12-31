package web

import "testing"

func TestHTTPErrorLogAdapter_Write_DoesNotError(t *testing.T) {
	adapter := HTTPErrorLogAdapter{}
	msg := []byte("some server warning")
	n, err := adapter.Write(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != len(msg) {
		t.Fatalf("unexpected n: got %d want %d", n, len(msg))
	}

	// A couple of "suppressed" TLS lines
	_, _ = adapter.Write([]byte("http: TLS handshake error from 127.0.0.1: EOF"))
	_, _ = adapter.Write([]byte("http: TLS handshake error from 127.0.0.1: remote error: tls: unknown certificate"))
}
