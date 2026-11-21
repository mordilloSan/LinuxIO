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

func TestRunPrintsHelp(t *testing.T) {
	for _, arg := range []string{"--help", "-h"} {
		if code := Run([]string{"linuxio-monitoring", arg}); code != 0 {
			t.Errorf("Run(%s) exit = %d, want 0", arg, code)
		}
	}
}

// The daemon takes no word, and -v is not a spelling of --version.
func TestRunTakesNoWordAndNoShortVersion(t *testing.T) {
	for _, arg := range []string{"version", "help", "-v"} {
		if code := Run([]string{"linuxio-monitoring", arg}); code != 2 {
			t.Errorf("Run(%s) exit = %d, want 2", arg, code)
		}
	}
}
