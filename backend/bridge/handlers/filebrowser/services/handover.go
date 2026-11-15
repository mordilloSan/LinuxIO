package services

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/linksafe"
)

// A root bridge keeps what it creates root-owned (backend audit item 110).
// Acting for the session user it may hand the entries it creates to that
// user, but only inside a folder that user already owns, so the user never
// gets an entry they could not have created themselves. Every chown and chmod
// here acts on an open handle, never on a path, because the folders involved
// are writable by the user's own processes.

// Owner is whom a root bridge hands the entries it creates to, and whom it
// acts for when it reaches a folder (see linksafe.Owner).
type Owner = linksafe.Owner

// HandsOverIn reports whether an entry created in a folder goes to owner:
// only when owner is set and the folder, as fstat saw its open handle,
// already belongs to them.
func HandsOverIn(folder os.FileInfo, owner Owner) bool {
	if !owner.Set {
		return false
	}
	st, ok := folder.Sys().(*syscall.Stat_t)
	return ok && st.Uid == owner.UID
}

// entryFollowingNoUserLinks returns the folder the entry at path resolves
// into, open, and the entry's name in it, following a symlink at path as
// chmod(1) does. On a root bridge acting for owner the folders are reached
// as linksafe.OpenDirNoUserLinks reaches them and a symlink at the entry is followed
// under the same rule; without owner set path resolves as the user's own
// programs would resolve it.
func entryFollowingNoUserLinks(root *fsroot.FSRoot, path string, owner Owner) (entry, error) {
	path = fsroot.CleanAbsPath(path)
	if !owner.Set {
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if path, err = filepath.EvalSymlinks(path); err != nil {
				return entry{}, err
			}
		}
		return openEntry(root, path, owner)
	}
	for hops := 0; ; hops++ {
		at, err := openEntry(root, path, owner)
		if err != nil {
			return entry{}, err
		}
		target, isLink := linksafe.LinkTarget(at.dir, at.name)
		if !isLink {
			return at, nil
		}
		if hops == linksafe.MaxLinkHops {
			_ = at.dir.Close()
			return entry{}, &fs.PathError{Op: "open", Path: path, Err: unix.ELOOP}
		}
		dir, err := linksafe.FollowLink(at.dir, at.name, target)
		if err != nil {
			return entry{}, err
		}
		// dir is reached one name at a time from /, so its name is its
		// real path and a relative target resolves against it.
		path = filepath.Join(dir.Name(), target)
		_ = dir.Close()
	}
}

// ResolveNoUserLinks returns the real path of the entry at path, following a
// symlink at path, for a root bridge acting for owner: as
// entryFollowingNoUserLinks reaches it, so a link the session user could have
// planted, on the way or at the entry, is refused.
func ResolveNoUserLinks(root *fsroot.FSRoot, path string, owner Owner) (string, error) {
	at, err := entryFollowingNoUserLinks(root, path, owner)
	if err != nil {
		return "", err
	}
	defer at.dir.Close()
	return filepath.Join(at.dir.Name(), at.name), nil
}

// openEntry opens the folder path sits in as linksafe.OpenDirNoUserLinks does and
// returns it with path's name in it ("." for /).
func openEntry(root *fsroot.FSRoot, path string, owner Owner) (entry, error) {
	path = fsroot.CleanAbsPath(path)
	dir, err := linksafe.OpenDirNoUserLinks(root, filepath.Dir(path), owner)
	if err != nil {
		return entry{}, err
	}
	name := filepath.Base(path)
	if path == "/" {
		name = "."
	}
	return entry{dir: dir, name: name, path: path}, nil
}

// MkdirAllAt makes names below dir, each inside the one before, through
// MkdirAt and returns the last one open, so every folder is made and handed
// over relative to the handle of its parent, never by path. It closes dir.
func MkdirAllAt(dir *os.File, names []string, perm os.FileMode, owner Owner) (*os.File, error) {
	for _, name := range names {
		next, err := MkdirAt(dir, name, perm, owner)
		_ = dir.Close()
		if err != nil {
			return nil, err
		}
		dir = next
	}
	return dir, nil
}

// MkdirAt makes name in parent with perm and returns it open, refusing a
// symlink at name. Only a folder this call made (mkdirat succeeded, so not
// EEXIST) inside a folder owner owns is handed over (see handOverDir); any
// other folder keeps the bridge's ownership and the service umask, as
// MkdirAll leaves it.
func MkdirAt(parent *os.File, name string, perm os.FileMode, owner Owner) (*os.File, error) {
	parentInfo, err := parent.Stat()
	if err != nil {
		return nil, err
	}
	return mkdirAt(parent, name, perm, owner, HandsOverIn(parentInfo, owner))
}

// mkdirAt is MkdirAt with the parent already judged: handOver says whether a
// folder this call makes goes to owner.
func mkdirAt(parent *os.File, name string, perm os.FileMode, owner Owner, handOver bool) (*os.File, error) {
	err := unix.Mkdirat(int(parent.Fd()), name, uint32(perm))
	created := err == nil
	if err != nil && !errors.Is(err, unix.EEXIST) {
		return nil, &fs.PathError{Op: "mkdirat", Path: filepath.Join(parent.Name(), name), Err: err}
	}
	dir, err := linksafe.OpenDirAt(parent, name)
	if err != nil {
		return nil, err
	}
	if created && handOver {
		if err := handOverDir(dir, owner, perm); err != nil {
			_ = dir.Close()
			return nil, err
		}
	}
	return dir, nil
}

// handOverDir hands the folder MkdirAt just made to owner, with perm less
// group and other write (0775 becomes 0755). Between mkdirat and openat a
// process that can write the parent could have put another folder under the
// name, so only a folder the bridge owns that is still empty, as a new one
// is, is handed over. A folder made in a setgid folder keeps the setgid bit
// and the group it inherited, as if the user had made it there.
func handOverDir(dir *os.File, owner Owner, perm os.FileMode) error {
	info, err := dir.Stat()
	if err != nil {
		return err
	}
	fresh, err := IsFreshDir(dir, info)
	if err != nil || !fresh {
		return err
	}
	setgid := info.Mode() & os.ModeSetgid
	return HandOver(dir, owner, perm&^0o022|setgid, setgid != 0)
}

// IsFreshDir reports whether dir, described by info, looks like the folder
// the bridge has just made: owned by the bridge's euid and empty.
func IsFreshDir(dir *os.File, info os.FileInfo) (bool, error) {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Geteuid() {
		return false, nil
	}
	_, err := dir.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	return false, err // nil when the folder has entries
}

// HandOver gives an entry the bridge created, held open, to owner and sets
// its mode. In a setgid folder the entry keeps the group it inherited.
// Ownership goes first because a chown clears setuid and setgid.
func HandOver(f *os.File, owner Owner, mode os.FileMode, inSetgidFolder bool) error {
	gid := int(owner.GID)
	if inSetgidFolder {
		gid = -1
	}
	if err := f.Chown(int(owner.UID), gid); err != nil {
		return fmt.Errorf("hand %s to session user: %w", f.Name(), err)
	}
	if err := f.Chmod(mode); err != nil {
		return fmt.Errorf("set permissions of %s: %w", f.Name(), err)
	}
	return nil
}
