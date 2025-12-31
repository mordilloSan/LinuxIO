package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func withArgs(args []string, fn func()) {
	old := os.Args
	os.Args = args
	defer func() { os.Args = old }()
	fn()
}

func TestStartLinuxIO_Run_InvokesRunServer(t *testing.T) {
	called := false
	var gotCfg ServerConfig

	old := runServerFunc
	runServerFunc = func(cfg ServerConfig) {
		called = true
		gotCfg = cfg
	}
	defer func() { runServerFunc = old }()

	withArgs([]string{"linuxio", "run", "-port", "18090", "-verbose"}, func() {
		StartLinuxIO()
	})

	if !called {
		t.Fatal("expected runServerFunc to be called")
	}
	if gotCfg.Port != 18090 || !gotCfg.Verbose {
		t.Fatalf("unexpected cfg: %+v", gotCfg)
	}
}

func TestStartLinuxIO_UnknownCommand_ShowsHelp(t *testing.T) {
	var errb bytes.Buffer
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
		_ = r.Close()
	}()

	withArgs([]string{"linuxio", "wat"}, func() { StartLinuxIO() })
	_ = w.Close()
	if _, err := errb.ReadFrom(r); err != nil {
		t.Fatalf("read stderr: %v", err)
	}

	if !strings.Contains(errb.String(), "unknown command") {
		t.Fatalf("expected 'unknown command' in stderr, got: %q", errb.String())
	}
}
