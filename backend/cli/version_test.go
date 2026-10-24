package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestShowVersionFailedProbesAreIndentedAndHaveDeadline(t *testing.T) {
	orig := versionExecCommand
	t.Cleanup(func() { versionExecCommand = orig })
	versionExecCommand = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		if _, ok := ctx.Deadline(); !ok {
			t.Error("probe context has no deadline")
		}
		return exec.CommandContext(ctx, "/nonexistent/linuxio-probe")
	}

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = w
	showVersion(context.Background(), nil)
	os.Stdout = stdout
	_ = w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range versionProbes {
		want := "\n  " + c.binary + ": not found or error\n"
		if !strings.Contains(string(out), want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}
