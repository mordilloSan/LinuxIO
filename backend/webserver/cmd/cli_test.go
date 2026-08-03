package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestRun_InvokesRunServer(t *testing.T) {
	called := false
	var gotCfg ServerConfig

	old := runServerFunc
	runServerFunc = func(cfg ServerConfig) error {
		called = true
		gotCfg = cfg
		return nil
	}
	defer func() { runServerFunc = old }()

	code := Run([]string{"linuxio-webserver", "run", "-port", "18090", "-verbose"})
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

func TestRun_UnknownCommand_ReturnsUsageError(t *testing.T) {
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

	if !strings.Contains(errb.String(), "unknown command") {
		t.Fatalf("expected 'unknown command' in stderr, got: %q", errb.String())
	}
}

func TestRun_HelpUsesWebserverBinaryName(t *testing.T) {
	var errb bytes.Buffer
	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
		_ = r.Close()
	}()

	code := Run([]string{"linuxio-webserver", "help"})
	if code != 0 {
		t.Fatalf("Run exit code = %d, want 0", code)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close write pipe: %v", err)
	}
	os.Stderr = oldStderr
	if _, err := errb.ReadFrom(r); err != nil {
		t.Fatalf("read stderr: %v", err)
	}

	output := errb.String()
	if !strings.Contains(output, "linuxio-webserver run") {
		t.Fatalf("expected webserver binary name in help, got: %q", output)
	}
	if strings.Contains(output, "  linuxio run") {
		t.Fatalf("help still contains old binary name: %q", output)
	}
}
