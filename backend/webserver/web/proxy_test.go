package web

import "testing"

func TestContainerTargetURLIPv4(t *testing.T) {
	target := containerTargetURL("10.0.0.2", "8080")

	if got, want := target.Host, "10.0.0.2:8080"; got != want {
		t.Fatalf("Host = %q, want %q", got, want)
	}
	if got, want := target.String(), "http://10.0.0.2:8080"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestContainerTargetURLIPv6(t *testing.T) {
	target := containerTargetURL("2001:db8::1", "8080")

	if got, want := target.Host, "[2001:db8::1]:8080"; got != want {
		t.Fatalf("Host = %q, want %q", got, want)
	}
	if got, want := target.String(), "http://[2001:db8::1]:8080"; got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}
