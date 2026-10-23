package accounts

import (
	"testing"
)

func TestIsNonLoginShellRecognizesDebianAndRHELPaths(t *testing.T) {
	tests := []struct {
		shell string
		want  bool
	}{
		{shell: "/usr/sbin/nologin", want: true},
		{shell: "/sbin/nologin", want: true},
		{shell: "/bin/false", want: true},
		{shell: "/usr/bin/false", want: true},
		{shell: "/bin/bash", want: false},
	}

	for _, tc := range tests {
		if got := isNonLoginShell(tc.shell); got != tc.want {
			t.Fatalf("isNonLoginShell(%q) = %v, want %v", tc.shell, got, tc.want)
		}
	}
}
