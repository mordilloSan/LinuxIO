package memory

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func TestSwapOnFlags(t *testing.T) {
	for options, want := range map[string][]string{
		"":                           nil,
		"sw":                         nil,
		"sw,nofail":                  nil,
		"defaults,x-systemd.makefs":  nil,
		"pri=5":                      {"-p", "5"},
		"sw,pri=-1":                  {"-p", "-1"},
		"pri=32767,nofail":           {"-p", "32767"},
		"pri=+7":                     {"-p", "7"},
		"discard":                    {"--discard"},
		"discard=once":               {"--discard=once"},
		"sw,discard=pages,pri=10,ro": {"-p", "10", "--discard=pages"},
	} {
		got, err := swapOnFlags("/swap.img", options)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("swapOnFlags(%q) = %q, %v; want %q", options, got, err, want)
		}
	}
}

func TestSwapOnFlagsRefusesWhatSwaponCouldMisread(t *testing.T) {
	for _, options := range []string{
		"pri", "pri=", "pri=high", "pri=-2", "pri=32768", "pri=1,pri=2", "pri=5 -a",
		"discard=", "discard=all", "discard=once,discard", "discard=once -a",
	} {
		if got, err := swapOnFlags("/swap.img", options); !errors.Is(err, bridgeipc.ErrConflict) {
			t.Errorf("swapOnFlags(%q) = %q, %v; want it refused", options, got, err)
		}
	}
}

func TestSwapOnRunsSwaponOnThePath(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	b.madeSwapFile(ownSwapFilePath)
	if err := b.live().swapOn(t.Context(), ownSwapFilePath, "sw,pri=3,discard"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"swapon -p 3 --discard -- " + b.path(ownSwapFilePath)}; !slices.Equal(b.ran(), want) {
		t.Fatalf("ran %q, want %q", b.ran(), want)
	}
}

func TestSwapOnReportsWhatSwaponPrinted(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	b.madeSwapFile(ownSwapFilePath)
	b.fail(swaponCommand, 1)
	err := b.h.swapOn(t.Context(), ownSwapFilePath, "")
	if err == nil || !strings.Contains(err.Error(), "swapon: something went wrong") || !strings.Contains(err.Error(), "swapon -- "+b.path(ownSwapFilePath)) {
		t.Fatalf("err = %v, want the command and what it printed", err)
	}
}

func TestSwapOnRefusesAPathOthersCouldRedirect(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	b.madeSwapFile(ownSwapFilePath)
	b.mkdir("/var/lib", 0o777)
	isRefusal(t, b.h.swapOn(t.Context(), ownSwapFilePath, ""), errOthersCanRedirect)

	b = newBlocksHost(t, "ext4")
	b.madeSwapFile(ownSwapFilePath)
	if err := os.Rename(b.path(ownSwapFilePath), b.path(ownSwapDir+"/real")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", b.path(ownSwapFilePath)); err != nil {
		t.Fatal(err)
	}
	isRefusal(t, b.h.swapOn(t.Context(), ownSwapFilePath, ""), errNotRootFile)
	if len(b.ran()) != 0 {
		t.Fatalf("ran %q", b.ran())
	}
}

func TestSwapOnEndsTheProcessWhenCancelled(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	b.madeSwapFile(ownSwapFilePath)
	if err := os.WriteFile(b.tools+"/"+swaponCommand, []byte("#!/bin/sh\nexec /bin/sleep 60\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := b.h.swapOn(ctx, ownSwapFilePath, ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the deadline", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("returned after %s", elapsed)
	}
}

func TestSwapOnFailsWithoutTheCommand(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	b.madeSwapFile(ownSwapFilePath)
	if err := os.Remove(b.tools + "/" + swaponCommand); err != nil {
		t.Fatal(err)
	}
	if err := b.h.swapOn(t.Context(), ownSwapFilePath, ""); err == nil {
		t.Fatal("swapOn without swapon succeeded")
	}
}
