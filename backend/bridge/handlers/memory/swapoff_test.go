package memory

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
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

// swapoff resolves the path again in the kernel, so in a folder someone
// other than root can write, a link put at the path sends it to another
// swap device, which leaves without the room check. Such a path is refused
// before the command runs.
func TestSwapOffRefusesAPathOthersCanRedirect(t *testing.T) {
	ran := filepath.Join(t.TempDir(), "ran")
	t.Setenv("SWAPOFF_RAN", ran)
	fakeSwapoff(t, `: > "$SWAPOFF_RAN"`+"\n")
	shared := t.TempDir()
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}

	err := swapOff(t.Context(), filepath.Join(shared, "swap.img"))
	if !errors.Is(err, bridgeipc.ErrConflict) || !errors.Is(err, errOthersCanRedirect) {
		t.Errorf("err = %v, want a conflict that says others can redirect the path", err)
	}
	if _, err := os.Stat(ran); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("swapoff ran (%v), want it never started", err)
	}
}

// The folders a swap device is normally in belong to root alone.
func TestRootOnlyPathAcceptsRootsFolders(t *testing.T) {
	for _, path := range []string{"/swap.img", "/dev/sda2", "/no-such-folder/swap.img"} {
		if err := rootOnlyPath(path); err != nil {
			t.Errorf("rootOnlyPath(%q) = %v, want nil", path, err)
		}
	}
}

// foldersOnlyOf, the rule behind rootOnlyPath, on folders the test owns:
// every folder from the base down to the one the path sits in must be a
// folder, not a link, that the owner alone may write.
func TestFoldersOnlyOf(t *testing.T) {
	me := uint32(os.Getuid())
	base := t.TempDir()
	if err := os.Chmod(base, 0o755); err != nil {
		t.Fatal(err)
	}
	mkdir := func(name string, mode os.FileMode) string {
		t.Helper()
		dir := filepath.Join(base, name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	private := mkdir("private", 0o755)
	nested := filepath.Join(private, "nested")
	if err := os.Mkdir(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	groupWritable := mkdir("group", 0o775)
	worldWritable := mkdir("world", 0o757)
	sticky := mkdir("sticky", 0o1777)
	below := filepath.Join(worldWritable, "below")
	if err := os.Mkdir(below, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(private, filepath.Join(base, "link")); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name    string
		uid     uint32
		path    string
		refused string
	}{
		{"the owner's folders", me, filepath.Join(nested, "swap.img"), ""},
		{"a missing folder ends the walk", me, filepath.Join(private, "gone", "swap.img"), ""},
		{"the base itself", me, filepath.Join(base, "swap.img"), ""},
		{"another owner", me + 1, filepath.Join(private, "swap.img"), base},
		{"group may write", me, filepath.Join(groupWritable, "swap.img"), groupWritable},
		{"everyone may write", me, filepath.Join(worldWritable, "swap.img"), worldWritable},
		{"a sticky folder", me, filepath.Join(sticky, "swap.img"), sticky},
		{"below a folder others may write", me, filepath.Join(below, "swap.img"), worldWritable},
		{"the name of a deleted file", me, filepath.Join(worldWritable, "swap.img (deleted)"), worldWritable},
		{"a folder that is a link", me, filepath.Join(base, "link", "swap.img"), filepath.Join(base, "link")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := foldersOnlyOf(tc.uid, base, tc.path)
			if tc.refused == "" {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, errOthersCanRedirect) || !errors.Is(err, bridgeipc.ErrConflict) || !strings.Contains(err.Error(), "("+tc.refused+")") {
				t.Fatalf("err = %v, want a conflict that names %s", err, tc.refused)
			}
		})
	}
}
