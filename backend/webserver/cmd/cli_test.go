package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

func TestRun_InvokesRunServer(t *testing.T) {
	called := false
	var gotCfg ServerConfig

	old := runServerFunc
	runServerFunc = func(_ context.Context, cfg ServerConfig) error {
		called = true
		gotCfg = cfg
		return nil
	}
	defer func() { runServerFunc = old }()

	code := Run([]string{"linuxio-webserver", "--port", "18090", "--verbose"})
	if code != 0 {
		t.Fatalf("Run exit code = %d, want 0", code)
	}

	if !called {
		t.Fatal("expected runServerFunc to be called")
	}
	if gotCfg.Port != 18090 || !gotCfg.Verbose {
		t.Fatalf("unexpected cfg: %+v", gotCfg)
	}
}

func TestRun_UnknownArgument_ReturnsUsageError(t *testing.T) {
	var errb bytes.Buffer
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
		if err := r.Close(); err != nil {
			t.Fatalf("close read pipe: %v", err)
		}
	}()

	code := Run([]string{"linuxio-webserver", "wat"})
	if code != 2 {
		t.Fatalf("Run exit code = %d, want 2", code)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close write pipe: %v", err)
	}
	if _, err := errb.ReadFrom(r); err != nil {
		t.Fatalf("read stderr: %v", err)
	}

	if !strings.Contains(errb.String(), "unknown argument") {
		t.Fatalf("expected 'unknown argument' in stderr, got: %q", errb.String())
	}
}

// captureStdout returns what fn wrote to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	old := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = old
	if err = w.Close(); err != nil {
		t.Fatalf("close write pipe: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	_ = r.Close()
	return string(out)
}

func TestRun_HelpSaysWhoStartsItAndHow(t *testing.T) {
	for _, arg := range []string{"--help", "-h"} {
		var code int
		output := captureStdout(t, func() { code = Run([]string{"linuxio-webserver", arg}) })
		if code != 0 {
			t.Fatalf("Run(%s) exit code = %d, want 0", arg, code)
		}
		for _, want := range []string{"Started by systemd (linuxio-webserver.service)", "  linuxio-webserver [--port N] [--verbose]\n"} {
			if !strings.Contains(output, want) {
				t.Errorf("%s output lacks %q:\n%s", arg, want, output)
			}
		}
	}
}

func TestRun_VersionPrintsTheRecordedLine(t *testing.T) {
	var code int
	output := captureStdout(t, func() { code = Run([]string{"linuxio-webserver", "--version"}) })
	if want := "LinuxIO Web Server " + version.Version + "\n"; code != 0 || output != want {
		t.Fatalf("Run(--version) = %d %q, want 0 %q", code, output, want)
	}
}

// The webserver takes no word, and -v is not a spelling of --version.
func TestRun_TakesNoWordAndNoShortVersion(t *testing.T) {
	old := runServerFunc
	runServerFunc = func(context.Context, ServerConfig) error {
		t.Error("the server must not start")
		return nil
	}
	defer func() { runServerFunc = old }()

	for _, arg := range []string{"help", "version", "-v"} {
		if code := Run([]string{"linuxio-webserver", arg}); code != 2 {
			t.Errorf("Run(%s) exit code = %d, want 2", arg, code)
		}
	}
}
