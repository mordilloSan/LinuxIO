package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientRemoteHost_UsesForwardedForFromTrustedProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "172.18.0.4:49832"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 172.18.0.4")

	if got := ClientRemoteHost(req); got != "203.0.113.9" {
		t.Fatalf("remote host = %q, want %q", got, "203.0.113.9")
	}
}

func TestClientRemoteHost_UsesRightmostUntrustedForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:49832"
	req.Header.Set("X-Forwarded-For", "127.0.0.1, 192.168.1.239")

	if got := ClientRemoteHost(req); got != "192.168.1.239" {
		t.Fatalf("remote host = %q, want %q", got, "192.168.1.239")
	}
}

func TestClientRemoteHost_FallsBackToRealIPWhenForwardedForOnlyHasTrustedProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:49832"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Real-IP", "192.168.1.239")

	if got := ClientRemoteHost(req); got != "192.168.1.239" {
		t.Fatalf("remote host = %q, want %q", got, "192.168.1.239")
	}
}

func TestClientRemoteHost_IgnoresForwardedForFromUntrustedPeer(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "203.0.113.10:49832"
	req.Header.Set("X-Forwarded-For", "198.51.100.7")

	if got := ClientRemoteHost(req); got != "203.0.113.10" {
		t.Fatalf("remote host = %q, want %q", got, "203.0.113.10")
	}
}

func TestClientRemoteHost_UsesCustomTrustedProxyCIDR(t *testing.T) {
	t.Setenv(trustedProxyCIDRsEnv, "10.10.0.0/16")

	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "10.10.1.20:49832"
	req.Header.Set("X-Real-IP", "198.51.100.7")

	if got := ClientRemoteHost(req); got != "198.51.100.7" {
		t.Fatalf("remote host = %q, want %q", got, "198.51.100.7")
	}
}

func TestClientRemoteHost_ParsesRFCForwardedHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "172.18.0.4:49832"
	req.Header.Set("Forwarded", `for="[2001:db8::1]:443";proto=https`)

	if got := ClientRemoteHost(req); got != "2001:db8::1" {
		t.Fatalf("remote host = %q, want %q", got, "2001:db8::1")
	}
}

func TestClientRemoteHost_FallsBackToForwardedWhenEarlierHeadersOnlyHaveTrustedProxy(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.RemoteAddr = "127.0.0.1:49832"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.Header.Set("X-Real-IP", "127.0.0.1")
	req.Header.Set("Forwarded", `for=127.0.0.1, for=192.168.1.239`)

	if got := ClientRemoteHost(req); got != "192.168.1.239" {
		t.Fatalf("remote host = %q, want %q", got, "192.168.1.239")
	}
}
