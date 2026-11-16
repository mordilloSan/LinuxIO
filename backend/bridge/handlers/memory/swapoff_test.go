package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

// fakeSwapoff puts a swapoff script alone on the command search path.
func fakeSwapoff(t *testing.T, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, swapoffCommand), []byte("#!/bin/sh\n"+script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	saved := hostcmd.SearchDirs
	hostcmd.SearchDirs = nil
	t.Cleanup(func() { hostcmd.SearchDirs = saved })
}

func TestSwapOffRunsSwapoffOnThePath(t *testing.T) {
	out := filepath.Join(t.TempDir(), "args")
	t.Setenv("SWAPOFF_ARGS", out)
	fakeSwapoff(t, `printf '%s\n' "$@" > "$SWAPOFF_ARGS"`+"\n")

	if err := swapOff(t.Context(), "/data/swap file"); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(out)
	if err != nil || string(args) != "--\n/data/swap file\n" {
		t.Fatalf("args = %q, %v; want -- and the path as one argument", args, err)
	}
}

func TestSwapOffReportsWhatSwapoffPrinted(t *testing.T) {
	fakeSwapoff(t, "echo 'swapoff: /swap.img: swapoff failed: Cannot allocate memory' >&2\nexit 255\n")

	err := swapOff(t.Context(), "/swap.img")
	if err == nil || !strings.Contains(err.Error(), "Cannot allocate memory") || !strings.Contains(err.Error(), "swapoff -- /swap.img") {
		t.Fatalf("err = %v, want the command and what it printed", err)
	}
}

// A cancelled request ends the swapoff process, which makes the kernel
// leave the device swapping.
func TestSwapOffEndsTheProcessWhenCancelled(t *testing.T) {
	fakeSwapoff(t, "exec sleep 60\n")
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	if err := swapOff(ctx, "/swap.img"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("returned after %s, want as soon as the request ended", elapsed)
	}
}

// swapoff reads LABEL= and UUID= arguments as tags, and /proc/swaps names
// every device by an absolute path.
func TestSwapOffRefusesAPathThatIsNotAbsolute(t *testing.T) {
	fakeSwapoff(t, "exit 0\n")
	for _, path := range []string{"", "swap.img", "UUID=0a1b", "-a"} {
		if err := swapOff(t.Context(), path); err == nil {
			t.Errorf("swapOff(%q) ran, want it refused", path)
		}
	}
}

func TestSwapOffFailsWithoutTheCommand(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	saved := hostcmd.SearchDirs
	hostcmd.SearchDirs = nil
	t.Cleanup(func() { hostcmd.SearchDirs = saved })

	if err := swapOff(t.Context(), "/swap.img"); err == nil {
		t.Fatal("want an error")
	}
}
