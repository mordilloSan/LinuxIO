package cmd

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/config"
)

func withArgs(args []string, fn func()) {
	old := os.Args
	os.Args = args
	defer func() { os.Args = old }()
	fn()
}

func TestStartLinuxIO_Help(t *testing.T) {
	var out, errb bytes.Buffer
	oldStdout, oldStderr := os.Stdout, os.Stderr
	r1, w1, _ := os.Pipe()
	r2, w2, _ := os.Pipe()
	os.Stdout, os.Stderr = w1, w2

	defer func() {
		_ = w1.Close()
		_ = w2.Close()
		os.Stdout, os.Stderr = oldStdout, oldStderr
		_ = r1.Close()
		_ = r2.Close()
	}()

	withArgs([]string{"linuxio", "help"}, func() { StartLinuxIO() })
	_ = w1.Close()
	_ = w2.Close()

	if _, err := out.ReadFrom(r1); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if _, err := errb.ReadFrom(r2); err != nil {
		t.Fatalf("read stderr: %v", err)
	}

	all := out.String() + errb.String()
	if !strings.Contains(all, "LinuxIO Server") {
		t.Fatalf("expected general help in output, got: %q", all)
	}
}

func TestStartLinuxIO_Version(t *testing.T) {
	var out bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() {
		os.Stdout = oldStdout
		_ = r.Close()
	}()

	withArgs([]string{"linuxio", "version"}, func() { StartLinuxIO() })
	_ = w.Close()
	if _, err := out.ReadFrom(r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	got := out.String()
	if !strings.Contains(got, "linuxio ") {
		t.Fatalf("expected 'linuxio ' prefix, got: %q", got)
	}
	if config.Version != "" && !strings.Contains(got, config.Version) {
		t.Fatalf("expected version %q in output, got %q", config.Version, got)
	}
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
