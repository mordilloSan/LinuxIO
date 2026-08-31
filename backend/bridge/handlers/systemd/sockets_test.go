package systemd

import "testing"

func TestStreamSocketListenAddress(t *testing.T) {
	listeners := [][]any{
		{"Datagram", "127.0.0.1:9000"},
		{"Stream", "[::]:8080"},
	}

	address, ok := streamSocketListenAddress(listeners)
	if !ok || address != "[::]:8080" {
		t.Fatalf("streamSocketListenAddress() = %q, %t", address, ok)
	}
}
