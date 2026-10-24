package hostcmd

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLookPathInFallsBackToDirs(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	dir := t.TempDir()
	tool := filepath.Join(dir, "faketool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notexec"), nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if got, err := LookPathIn("faketool", []string{dir}); err != nil || got != tool {
		t.Fatalf("LookPathIn = %q, %v; want %q", got, err, tool)
	}
	if _, err := LookPathIn("notexec", []string{dir}); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("non-executable err = %v, want exec.ErrNotFound", err)
	}
	if _, err := LookPathIn("missing", []string{dir}); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("missing err = %v, want exec.ErrNotFound", err)
	}
}
