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

	code := Run([]string{"linuxio", "run", "-port", "18090", "-verbose"})
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

func TestRun_UnknownCommand_ShowsHelp(t *testing.T) {
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

	code := Run([]string{"linuxio", "wat"})
	if code != 0 {
		t.Fatalf("Run exit code = %d, want 0", code)
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
