package cli

import "testing"

func TestMainRejectsUnknownCommand(t *testing.T) {
	if code := Main([]string{"menu"}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
}

func TestMainPrintsVersion(t *testing.T) {
	if code := Main([]string{"--version"}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}
