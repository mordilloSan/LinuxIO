package memory

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// madeSwapFile makes a swap file of testSwapSize at path with the blocks and
// forgets the commands that ran.
func (b *blocksHost) madeSwapFile(path string) fileID {
	b.t.Helper()
	id, err := b.h.makeSwapFile(b.t.Context(), path, testSwapSize)
	if err != nil {
		b.t.Fatal(err)
	}
	if err := os.Remove(b.tools + "/log"); err != nil {
		b.t.Fatal(err)
	}
	if err := os.Remove(b.tools + "/lengths"); err != nil && !errors.Is(err, os.ErrNotExist) {
		b.t.Fatal(err)
	}
	return id
}

func TestResizeSwapFileGrowsAndShrinksInPlace(t *testing.T) {
	for _, kind := range []string{"ext4", "btrfs"} {
		t.Run(kind, func(t *testing.T) {
			b := newBlocksHost(t, kind)
			id := b.madeSwapFile(ownSwapFilePath)
			if err := os.Chmod(b.path(ownSwapFilePath), 0o644); err != nil {
				t.Fatal(err)
			}
			for _, size := range []int64{2 * testSwapSize, testSwapSize / 2} {
				b.resizeInPlace(id, size)
			}
			// The shrink writes the smaller header, with the size in KiB,
			// while the file still has its old length, and cuts it after.
			want := []string{"mkswap -- " + b.path(ownSwapFilePath), fmt.Sprintf("mkswap -- %s %d", b.path(ownSwapFilePath), testSwapSize/2>>10)}
			if !slices.Equal(b.ran(), want) {
				t.Fatalf("ran %q, want %q", b.ran(), want)
			}
			if got, want := b.mkswapLengths(), []string{strconv.Itoa(2 * testSwapSize), strconv.Itoa(2 * testSwapSize)}; !slices.Equal(got, want) {
				t.Fatalf("mkswap saw lengths %q, want %q: the shrink's header before the cut", got, want)
			}
		})
	}
}

func TestResizeSwapFileRefuses(t *testing.T) {
	for name, c := range map[string]struct {
		kind   string
		setup  func(b *blocksHost, id *fileID)
		reason error
	}{
		"while it swaps":           {"ext4", func(b *blocksHost, _ *fileID) { b.swapping(ownSwapFilePath) }, errSwapFileActive},
		"another file at the name": {"ext4", func(b *blocksHost, id *fileID) { id.ino++ }, errSwapFileChanged},
		"a link at the name": {"ext4", func(b *blocksHost, _ *fileID) {
			if err := os.Rename(b.path(ownSwapFilePath), b.path("/var/lib/linuxio/real")); err != nil {
				b.t.Fatal(err)
			}
			if err := os.Symlink(b.path("/var/lib/linuxio/real"), b.path(ownSwapFilePath)); err != nil {
				b.t.Fatal(err)
			}
		}, errNotRootFile},
		"a folder others can write": {"ext4", func(b *blocksHost, _ *fileID) { b.mkdir(ownSwapDir, 0o777) }, errOthersCanRedirect},
		"btrfs without NOCOW": {"btrfs", func(b *blocksHost, _ *fileID) {
			b.flags[b.stat(ownSwapFilePath).Ino] &^= fsNoCowFl
		}, errNoCow},
		"a read-only filesystem": {"ext4", func(b *blocksHost, _ *fileID) { b.readOnly = true }, errReadOnlyFilesystem},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBlocksHost(t, c.kind)
			id := b.madeSwapFile(ownSwapFilePath)
			c.setup(b, &id)
			err := b.h.resizeSwapFile(t.Context(), ownSwapFilePath, id, 2*testSwapSize)
			isRefusal(t, err, c.reason)
			if len(b.ran()) != 0 {
				t.Fatalf("ran %q", b.ran())
			}
			var st unix.Stat_t
			if unix.Stat(b.path(ownSwapFilePath), &st) == nil && st.Size == 2*testSwapSize {
				t.Fatal("the file was resized")
			}
		})
	}
}

// A grow the disk has no room for leaves the file at its old length, whose
// old header fits it, and runs no mkswap.
func TestResizeSwapFileWithoutRoomKeepsTheOldLength(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	id := b.madeSwapFile(ownSwapFilePath)
	b.noSpace = true
	err := b.h.resizeSwapFile(t.Context(), ownSwapFilePath, id, 4*testSwapSize)
	isRefusal(t, err, errNoSpaceForSwap)
	if errors.Is(err, errNoSwapHeader) || !errors.Is(err, unix.ENOSPC) {
		t.Fatalf("err = %v, want ENOSPC without errNoSwapHeader", err)
	}
	if st := b.stat(ownSwapFilePath); st.Size != testSwapSize || len(b.ran()) != 0 {
		t.Fatalf("size %d, ran %q; want the old size and no mkswap", st.Size, b.ran())
	}
}

func TestResizeSwapFileRunsMkswapAgainOnce(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	id := b.madeSwapFile(ownSwapFilePath)
	b.fail(mkswapCommand, 1)
	if err := b.h.resizeSwapFile(t.Context(), ownSwapFilePath, id, 2*testSwapSize); err != nil {
		t.Fatalf("mkswap failing once: %v", err)
	}
	if len(b.ran()) != 2 {
		t.Fatalf("ran %q, want mkswap twice", b.ran())
	}

	// A shrink whose mkswap fails twice is never cut: a header the failed
	// mkswap left can only be smaller than the file.
	b.fail(mkswapCommand, 2)
	err := b.h.resizeSwapFile(t.Context(), ownSwapFilePath, id, testSwapSize)
	if !errors.Is(err, errNoSwapHeader) || !strings.Contains(err.Error(), "mkswap: something went wrong") {
		t.Fatalf("mkswap failing twice: err = %v, want errNoSwapHeader with what mkswap printed", err)
	}
	if st := b.stat(ownSwapFilePath); st.Size != 2*testSwapSize {
		t.Fatalf("size %d, want the old length", st.Size)
	}
}

// resizeInPlace resizes LinuxIO's swap file to size and checks that it is
// the same file, 0600, allocated to that length.
func (b *blocksHost) resizeInPlace(id fileID, size int64) {
	b.t.Helper()
	if err := b.live().resizeSwapFile(b.t.Context(), ownSwapFilePath, id, uint64(size)); err != nil {
		b.t.Fatalf("to %d: %v", size, err)
	}
	st := b.stat(ownSwapFilePath)
	if st.Size != size || st.Mode&0o7777 != 0o600 || fileIDOf(&st) != id || st.Blocks*512 < size {
		b.t.Fatalf("to %d: size %d, mode %#o, id %+v; want the same file, 0600, allocated", size, st.Size, st.Mode&0o7777, fileIDOf(&st))
	}
}

// mkswapLengths are the lengths the files mkswap was run on had, in turn.
func (b *blocksHost) mkswapLengths() []string {
	b.t.Helper()
	data, err := os.ReadFile(b.tools + "/lengths")
	if err != nil {
		b.t.Fatal(err)
	}
	return strings.Fields(string(data))
}

func TestResizeSwapFileReportsAMissingFile(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	id := b.madeSwapFile(ownSwapFilePath)
	if err := os.Remove(b.path(ownSwapFilePath)); err != nil {
		t.Fatal(err)
	}
	if err := b.h.resizeSwapFile(t.Context(), ownSwapFilePath, id, testSwapSize); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("err = %v, want os.ErrNotExist", err)
	}
}
