package version

import (
	"bytes"
	"strings"
	"testing"
)

var testProgram = Program{
	Binary:    "linuxio-example",
	About:     "the example daemon",
	StartedBy: "systemd (linuxio-example.service)",
	Usage:     []string{"linuxio-example [--config PATH] [--verbose]"},
}

// The version line starts with the program's name and ends with the build.
func TestProgramAnswersVersionWithItsNameAndTheBuild(t *testing.T) {
	for binary, name := range map[string]string{
		"linuxio-webserver":     "LinuxIO Web Server",
		"linuxio-bridge":        "LinuxIO Bridge",
		"linuxio-indexer":       "LinuxIO Indexer",
		"linuxio-monitoring":    "LinuxIO Monitoring",
		"linuxio-worker":        "LinuxIO Worker",
		"linuxio-docker-update": "LinuxIO Docker Updater",
		"linuxio-claude-relay":  "LinuxIO Claude Relay",
	} {
		var out bytes.Buffer
		if !(Program{Binary: binary}).Answer([]string{"--version"}, &out) {
			t.Fatalf("%s: --version was not answered", binary)
		}
		if want := name + " " + Version + "\n"; out.String() != want {
			t.Errorf("%s: --version printed %q, want %q", binary, out.String(), want)
		}
	}
}

// A program the table does not hold has only its file name to go by.
func TestProgramOutsideTheTableAnswersVersionWithItsFileName(t *testing.T) {
	var out bytes.Buffer
	if !testProgram.Answer([]string{"--version"}, &out) {
		t.Fatal("--version was not answered")
	}
	if want := "linuxio-example " + Version + "\n"; out.String() != want {
		t.Fatalf("--version printed %q, want %q", out.String(), want)
	}
}

func TestProgramAnswersHelpWithWhoStartsItAndHow(t *testing.T) {
	for _, arg := range []string{"--help", "-h"} {
		var out bytes.Buffer
		if !testProgram.Answer([]string{arg}, &out) {
			t.Fatalf("%s was not answered", arg)
		}
		for _, want := range []string{
			"linuxio-example: the example daemon\n",
			"Started by systemd (linuxio-example.service), not by hand.",
			"Manage LinuxIO with the linuxio command.",
			"  linuxio-example [--config PATH] [--verbose]\n",
			"--help, -h and --version",
		} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s output lacks %q:\n%s", arg, want, out.String())
			}
		}
	}
}

// The words and -v are not spellings of the two requests: a daemon takes no
// word, and -v reads as --verbose beside a program that has that flag.
func TestProgramAnswersNoOtherSpelling(t *testing.T) {
	for _, args := range [][]string{nil, {"version"}, {"help"}, {"-v"}, {"-version"}, {"--verbose", "--version"}} {
		var out bytes.Buffer
		if testProgram.Answer(args, &out) {
			t.Errorf("Answer(%q) = true, want false", args)
		}
		if out.Len() != 0 {
			t.Errorf("Answer(%q) wrote %q", args, out.String())
		}
	}
}
