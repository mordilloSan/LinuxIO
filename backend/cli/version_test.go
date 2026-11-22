package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
)

// runCaptured runs the CLI with args and returns its exit code and what it
// wrote to standard output and standard error.
func runCaptured(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	dir := t.TempDir()
	outFile, err := os.Create(filepath.Join(dir, "stdout"))
	if err != nil {
		t.Fatal(err)
	}
	errFile, err := os.Create(filepath.Join(dir, "stderr"))
	if err != nil {
		t.Fatal(err)
	}
	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outFile, errFile
	code = runCLI(append([]string{"linuxio"}, args...))
	os.Stdout, os.Stderr = origOut, origErr
	_ = outFile.Close()
	_ = errFile.Close()

	out, err := os.ReadFile(outFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	errOut, err := os.ReadFile(errFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	return code, string(out), string(errOut)
}

// linuxio version and linuxio --version print the one version of this build,
// which is the package's, and nothing else.
func TestVersionPrintsTheBuildVersion(t *testing.T) {
	orig := version.Version
	t.Cleanup(func() { version.Version = orig })
	version.Version = "v1.2.3"

	for _, arg := range []string{"version", "--version"} {
		code, out, errOut := runCaptured(t, arg)
		if code != 0 || out != "LinuxIO v1.2.3\n" || errOut != "" {
			t.Errorf("linuxio %s = %d, stdout %q, stderr %q; want 0, %q", arg, code, out, errOut, "LinuxIO v1.2.3\n")
		}
	}
}

func TestHelpPrintsTheCommandSummary(t *testing.T) {
	const want = `LinuxIO system administration

linuxio status
linuxio logs [component] [lines]
linuxio start
linuxio stop
linuxio restart [--full]
linuxio verbose enable|disable|status
linuxio version
`
	for _, args := range [][]string{nil, {"help"}, {"-h"}, {"--help"}} {
		code, out, errOut := runCaptured(t, args...)
		if code != 0 || out != want || errOut != "" {
			t.Errorf("linuxio %q = %d, stdout %q, stderr %q; want 0 and the summary", args, code, out, errOut)
		}
	}
}

func TestUnknownCommandPrintsTheSummaryToStandardError(t *testing.T) {
	code, out, errOut := runCaptured(t, "frobnicate")
	if want := "Unknown command: frobnicate\n\n" + helpText; code != 2 || out != "" || errOut != want {
		t.Fatalf("linuxio frobnicate = %d, stdout %q, stderr %q; want 2 and %q on stderr", code, out, errOut, want)
	}
}
