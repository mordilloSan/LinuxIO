package memory

import (
	"os"
	"testing"
)

func TestDeleteSwapFile(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	id := b.madeSwapFile(ownSwapFilePath)
	if err := b.live().deleteSwapFile(t.Context(), ownSwapFilePath, id); err != nil {
		t.Fatal(err)
	}
	if b.exists(ownSwapFilePath) || !b.exists(ownSwapDir) {
		t.Fatal("want the file gone and its folder kept")
	}
	// Gone already, and with its folder gone too, is done.
	if err := b.h.deleteSwapFile(t.Context(), ownSwapFilePath, id); err != nil {
		t.Fatalf("file gone: %v", err)
	}
	if err := os.Remove(b.path(ownSwapDir)); err != nil {
		t.Fatal(err)
	}
	if err := b.h.deleteSwapFile(t.Context(), ownSwapFilePath, id); err != nil {
		t.Fatalf("folder gone: %v", err)
	}
}

func TestDeleteSwapFileRefuses(t *testing.T) {
	for name, c := range map[string]struct {
		setup  func(b *blocksHost, id *fileID)
		reason error
	}{
		"while it swaps":           {func(b *blocksHost, _ *fileID) { b.swapping(ownSwapFilePath) }, errSwapFileActive},
		"another file at the name": {func(_ *blocksHost, id *fileID) { id.ino++ }, errSwapFileChanged},
		"a file put at the name since": {func(b *blocksHost, _ *fileID) {
			if err := os.Remove(b.path(ownSwapFilePath)); err != nil {
				b.t.Fatal(err)
			}
			b.write(ownSwapFilePath, "new")
		}, errSwapFileChanged},
		"a folder others can write": {func(b *blocksHost, _ *fileID) { b.mkdir(ownSwapDir, 0o777) }, errOthersCanRedirect},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBlocksHost(t, "ext4")
			id := b.madeSwapFile(ownSwapFilePath)
			c.setup(b, &id)
			isRefusal(t, b.h.deleteSwapFile(t.Context(), ownSwapFilePath, id), c.reason)
			if !b.exists(ownSwapFilePath) {
				t.Fatal("the file was deleted")
			}
		})
	}
}
