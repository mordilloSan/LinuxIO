package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

func TestRunCLIAnswersVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runCLI(context.Background(), []string{"--version"}, &stdout, &stderr)
	if want := "LinuxIO Worker " + version.Version + "\n"; code != 0 || stdout.String() != want {
		t.Fatalf("--version = %d %q, want 0 %q", code, stdout.String(), want)
	}
}

func TestRunCLIHelpListsTheWordsSystemdRuns(t *testing.T) {
	for _, arg := range []string{"--help", "-h"} {
		var stdout, stderr bytes.Buffer
		if code := runCLI(context.Background(), []string{arg}, &stdout, &stderr); code != 0 {
			t.Fatalf("%s exit code = %d, want 0", arg, code)
		}
		for _, want := range []string{"Started by systemd", "  linuxio-worker snapshot [--now RFC3339]\n", "  linuxio-worker scheduled-run begin|end --unit <unit>\n"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("%s output lacks %q:\n%s", arg, want, stdout.String())
			}
		}
	}
}

// help is not one of the worker's words: its words are jobs and hooks.
func TestRunCLIRefusesTheHelpAndVersionWords(t *testing.T) {
	for _, word := range []string{"help", "version", "-v"} {
		var stdout, stderr bytes.Buffer
		if code := runCLI(context.Background(), []string{word}, &stdout, &stderr); code != 2 {
			t.Errorf("%s exit code = %d, want 2", word, code)
		}
		if !strings.Contains(stderr.String(), "unknown command") {
			t.Errorf("%s stderr = %q, want an unknown command", word, stderr.String())
		}
	}
}
