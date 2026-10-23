package cli

import (
	"testing"
)

func TestRunRejectsUnknownCommand(t *testing.T) {
	if code := Run([]string{"linuxio-monitoring", "menu"}); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

func TestRunPrintsVersion(t *testing.T) {
	if code := Run([]string{"linuxio-monitoring", "--version"}); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
}
