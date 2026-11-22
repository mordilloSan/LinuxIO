package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/filelock"
)

const ownSwapLine = ownSwapFilePath + " none swap sw,nofail 0 0\n"

func TestAddFstabSwap(t *testing.T) {
	for name, c := range map[string]struct{ before, after string }{
		"empty":   {"", ownSwapLine},
		"missing": {"\x00", ownSwapLine},
		"entries and comments": {
			"# /etc/fstab: static file system information.\nUUID=1234\t/\text4\terrors=remount-ro  0  1\n\n/swap.img   none swap sw 0 0\n",
			"# /etc/fstab: static file system information.\nUUID=1234\t/\text4\terrors=remount-ro  0  1\n\n/swap.img   none swap sw 0 0\n" + ownSwapLine,
		},
		"no newline at the end": {"/swap.img none swap sw 0 0", "/swap.img none swap sw 0 0\n" + ownSwapLine},
		// Only a swap line counts as naming the file.
		"a comment and a mount naming the path": {
			"#" + ownSwapLine + ownSwapFilePath + " /mnt none bind 0 0\n",
			"#" + ownSwapLine + ownSwapFilePath + " /mnt none bind 0 0\n" + ownSwapLine,
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBlocksHost(t, "ext4")
			if c.before == "\x00" {
				if err := os.Remove(b.path(etcFstab)); err != nil {
					t.Fatal(err)
				}
			} else {
				b.write(etcFstab, c.before)
			}
			if err := b.live().addFstabSwap(t.Context(), ownSwapFilePath); err != nil {
				t.Fatal(err)
			}
			if got := b.read(etcFstab); got != c.after {
				t.Fatalf("fstab =\n%q\nwant\n%q", got, c.after)
			}
		})
	}
}

// A swap line that names the path already, as written or escaped, leaves
// the file as it was, not even rewritten.
func TestAddFstabSwapKeepsALineThatIsThere(t *testing.T) {
	for _, before := range []string{
		"/swap.img none swap sw 0 0\n/var/lib/linuxio/swap/swapfile\tnone\tswap\tdefaults,pri=2 0 0",
		`/var/lib/linuxio/swap\057swapfile none swap sw 0 0` + "\n",
	} {
		b := newBlocksHost(t, "ext4")
		b.write(etcFstab, before)
		ino := b.stat(etcFstab).Ino
		if err := b.h.addFstabSwap(t.Context(), ownSwapFilePath); err != nil {
			t.Fatal(err)
		}
		if b.read(etcFstab) != before || b.stat(etcFstab).Ino != ino {
			t.Fatalf("fstab with %q was changed", before)
		}
	}
}

func TestAddFstabSwapRefusesAPathThatNeedsEscaping(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	for _, path := range []string{"/swap file", "/swap\tfile", "/swap\nfile", `/swap\file`, "swapfile", "/a/../swapfile"} {
		if err := b.h.addFstabSwap(t.Context(), path); !errors.Is(err, bridgeipc.ErrInvalidArgs) {
			t.Errorf("addFstabSwap(%q) = %v, want ErrInvalidArgs", path, err)
		}
	}
	if b.read(etcFstab) != "" {
		t.Fatal("fstab was written")
	}
}

func TestRemoveFstabSwap(t *testing.T) {
	keep := "# " + ownSwapLine +
		"UUID=1234 / ext4 defaults 0 1\n" +
		ownSwapFilePath + " /mnt none bind 0 0\n" +
		"/swap.img none swap sw 0 0\n" +
		ownSwapFilePath + ".linuxio-resize none swap sw 0 0\n" +
		"\n"
	for name, c := range map[string]struct{ before, after string }{
		"one line": {keep + ownSwapLine, keep},
		"two lines, one escaped, among others": {
			ownSwapLine + keep + `/var/lib/linuxio/swap\057swapfile none swap sw 0 0` + "\n" + "tmpfs /tmp tmpfs defaults 0 0",
			keep + "tmpfs /tmp tmpfs defaults 0 0",
		},
		"the last line without its newline": {keep + strings.TrimSuffix(ownSwapLine, "\n"), keep},
		"none":                              {keep, keep},
		"spaces and tabs kept":              {"  " + ownSwapFilePath + "\tnone  swap sw 0 0\n\t/swap.img  none\tswap  sw  0 0\n", "\t/swap.img  none\tswap  sw  0 0\n"},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBlocksHost(t, "ext4")
			b.write(etcFstab, c.before)
			if err := b.live().removeFstabSwap(t.Context(), ownSwapFilePath); err != nil {
				t.Fatal(err)
			}
			if got := b.read(etcFstab); got != c.after {
				t.Fatalf("fstab =\n%q\nwant\n%q", got, c.after)
			}
		})
	}
}

func TestRemoveFstabSwapWithoutAnFstab(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	if err := os.Remove(b.path(etcFstab)); err != nil {
		t.Fatal(err)
	}
	if err := b.h.removeFstabSwap(t.Context(), ownSwapFilePath); err != nil {
		t.Fatal(err)
	}
	if b.exists(etcFstab) {
		t.Fatal("an fstab was made")
	}
}

// Edits wait for the fstab lock, so none is lost to another's write.
func TestFstabEditsTakeTheLock(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	release, err := filelock.AcquireExclusive(t.Context(), fstabEditLock)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- b.h.addFstabSwap(t.Context(), ownSwapFilePath) }()
	select {
	case err := <-done:
		t.Fatalf("the edit ran while another held the lock: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	if b.read(etcFstab) != "" {
		t.Fatal("fstab was written under another's lock")
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := range 8 {
		wg.Go(func() { errs <- b.h.addFstabSwap(t.Context(), fmt.Sprintf("/swap%d.img", i)) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	for i := range 8 {
		if !strings.Contains(b.read(etcFstab), fmt.Sprintf("/swap%d.img none swap", i)) {
			t.Fatalf("line %d was lost:\n%s", i, b.read(etcFstab))
		}
	}
}

func TestFstabEditGivesUpWithItsRequest(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	release, err := filelock.AcquireExclusive(t.Context(), fstabEditLock)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = release() }()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	if err := b.h.removeFstabSwap(ctx, ownSwapFilePath); err == nil {
		t.Fatal("the edit ran without the lock")
	}
}
