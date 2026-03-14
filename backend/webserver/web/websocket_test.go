package web

import (
	"net/http/httptest"
	"testing"
)

func TestCheckWebSocketOrigin(t *testing.T) {
	tests := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{name: "no origin", host: "example.com", want: true},
		{name: "same host", host: "example.com:8443", origin: "https://example.com:8443", want: true},
		{name: "host match is case-insensitive", host: "Example.com", origin: "https://example.com", want: true},
		{name: "different host", host: "example.com", origin: "https://evil.example", want: false},
		{name: "malformed origin", host: "example.com", origin: "://bad-origin", want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "https://"+tc.host+"/ws", nil)
			req.Host = tc.host
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}

			if got := checkWebSocketOrigin(req); got != tc.want {
				t.Fatalf("checkWebSocketOrigin() = %v, want %v", got, tc.want)
			}
		})
	}
}
