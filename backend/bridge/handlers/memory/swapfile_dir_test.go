package memory

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

func TestOpenRootOnlyFolderReturnsTheFolder(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	for _, dir := range []string{"/", "/var", "/var/lib/linuxio"} {
		f, err := b.h.openRootOnlyFolder(dir)
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
		if got := fdPath(t, int(f.Fd())); got != filepath.Clean(b.path(dir)) {
			t.Errorf("%s: handle is on %s", dir, got)
		}
		_ = f.Close()
	}
}

func TestOpenRootOnlyFolderRefusesWhatOthersCouldChange(t *testing.T) {
	for name, setup := range map[string]func(b *blocksHost){
		"a folder its group may write": func(b *blocksHost) { b.mkdir("/var/lib", 0o775) },
		"a folder everyone may write":  func(b *blocksHost) { b.mkdir("/var", 0o757) },
		"a sticky folder others write": func(b *blocksHost) { b.mkdir("/var/lib", 0o777|os.ModeSticky) },
		"the root others may write":    func(b *blocksHost) { b.mkdir("/", 0o777) },
		"a link to a root-only folder": func(b *blocksHost) {
			b.mkdir("/elsewhere/lib", 0o755)
			if err := os.RemoveAll(b.path("/var/lib")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(b.path("/elsewhere/lib"), b.path("/var/lib")); err != nil {
				t.Fatal(err)
			}
			b.mkdir("/elsewhere/lib/linuxio", 0o755)
		},
		"a relative link": func(b *blocksHost) {
			b.mkdir("/var/lib.real/linuxio", 0o755)
			if err := os.RemoveAll(b.path("/var/lib")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("lib.real", b.path("/var/lib")); err != nil {
				t.Fatal(err)
			}
		},
		"a file at a folder's name": func(b *blocksHost) {
			if err := os.RemoveAll(b.path("/var/lib")); err != nil {
				t.Fatal(err)
			}
			b.write("/var/lib", "")
		},
	} {
		t.Run(name, func(t *testing.T) {
			b := newBlocksHost(t, "ext4")
			setup(b)
			f, err := b.h.openRootOnlyFolder("/var/lib/linuxio")
			if err == nil {
				_ = f.Close()
			}
			isRefusal(t, err, errOthersCanRedirect)
		})
	}
}

// Every folder must be the owner's: as root, a folder another user owns is
// refused, here the test user's own root for an owner it is not.
func TestOpenRootOnlyFolderRefusesAFolderOfAnotherOwner(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	other := hostRoot{root: b.root, owner: b.h.owner + 1}
	if _, err := other.openRootOnlyFolder("/var"); !errors.Is(err, errOthersCanRedirect) {
		t.Fatalf("err = %v, want errOthersCanRedirect", err)
	}
}

func TestOpenRootOnlyFolderReportsAMissingFolder(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	if _, err := b.h.openRootOnlyFolder("/var/lib/missing/swap"); !errors.Is(err, fs.ErrNotExist) || errors.Is(err, bridgeipc.ErrConflict) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestOpenRootOnlyFolderRefusesAPathThatIsNotClean(t *testing.T) {
	b := newBlocksHost(t, "ext4")
	for _, dir := range []string{"", "var/lib", "/var/../etc", "/var/lib/"} {
		if f, err := b.h.openRootOnlyFolder(dir); err == nil {
			_ = f.Close()
			t.Errorf("%q opened, want it refused", dir)
		}
	}
}
