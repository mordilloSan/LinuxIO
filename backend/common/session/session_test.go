package session

import (
	"strings"
	"testing"
)

func TestDiagnosticRefDoesNotExposeSessionCredential(t *testing.T) {
	const credential = "0123456789abcdef0123456789abcdef"

	ref := DiagnosticRef(credential)
	if ref == "" {
		t.Fatal("DiagnosticRef returned an empty reference")
	}
	if len(ref) != len("sr-")+32 || !strings.HasPrefix(ref, "sr-") {
		t.Fatalf("DiagnosticRef = %q, want sr- plus 16 digest bytes", ref)
	}
	if strings.Contains(ref, credential) {
		t.Fatalf("diagnostic reference contains session credential: %q", ref)
	}
	if got := DiagnosticRef(credential); got != ref {
		t.Fatalf("DiagnosticRef is not stable: %q != %q", got, ref)
	}
	if got := DiagnosticRef("fedcba9876543210fedcba9876543210"); got == ref {
		t.Fatalf("different credentials produced the same reference: %q", got)
	}
	if got := DiagnosticRef(""); got != "" {
		t.Fatalf("DiagnosticRef(empty) = %q, want empty", got)
	}
}
