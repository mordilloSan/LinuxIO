package accounts

import (
	"strings"
	"testing"
)

func TestParseLastlogEntry(t *testing.T) {
	header := "Username         Port     From                                       Latest"
	latestColumn := strings.Index(header, "Latest")

	tests := []struct {
		name      string
		line      string
		username  string
		lastLogin string
		ok        bool
	}{
		{
			name:      "logged in entry preserves spaced date",
			line:      "alice            pts/0    192.168.1.4                                Mon Apr  1 12:34:56 +0000 2026",
			username:  "alice",
			lastLogin: "Mon Apr  1 12:34:56 +0000 2026",
			ok:        true,
		},
		{
			name:      "never logged in entry",
			line:      "daemon                                                           **Never logged in**",
			username:  "daemon",
			lastLogin: "Never",
			ok:        true,
		},
		{
			name: "blank line",
			line: "",
			ok:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			username, lastLogin, ok := parseLastlogEntry(tc.line, latestColumn)
			if ok != tc.ok {
				t.Fatalf("ok mismatch: got %v want %v", ok, tc.ok)
			}
			if username != tc.username {
				t.Fatalf("username mismatch: got %q want %q", username, tc.username)
			}
			if lastLogin != tc.lastLogin {
				t.Fatalf("lastLogin mismatch: got %q want %q", lastLogin, tc.lastLogin)
			}
		})
	}
}

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
