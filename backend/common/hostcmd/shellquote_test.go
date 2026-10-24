package hostcmd

import (
	"os/exec"
	"testing"
)

func TestShellQuote(t *testing.T) {
	for in, want := range map[string]string{
		"":            "''",
		"/srv/backup": "'/srv/backup'",
		"it's":        `'it'\''s'`,
		"$(rm -rf /)": "'$(rm -rf /)'",
		"a b\nc":      "'a b\nc'",
		"''":          `''\'''\'''`,
	} {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
		if out, err := exec.Command("sh", "-c", "printf '%s' "+ShellQuote(in)).Output(); err != nil || string(out) != in {
			t.Errorf("sh round trip of %q = %q, %v", in, out, err)
		}
	}
}
