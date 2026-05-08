package accounts

import "testing"

func TestValidateChpasswdInput(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		wantErr  bool
	}{
		{
			name:     "plain values allowed",
			username: "alice",
			password: "hunter2",
		},
		{
			name:     "username rejects colon",
			username: "ali:ce",
			password: "hunter2",
			wantErr:  true,
		},
		{
			name:     "password rejects newline",
			username: "alice",
			password: "line1\nline2",
			wantErr:  true,
		},
		{
			name:     "password rejects colon",
			username: "alice",
			password: "pass:word",
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateChpasswdInput(tc.username, tc.password)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateChpasswdInput() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

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
