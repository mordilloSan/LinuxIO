package services

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// A root bridge keeps what it creates root-owned (backend audit item 110).
// Acting for the session user it may hand the entries it creates to that
// user, but only inside a folder that user already owns, so the user never
// gets an entry they could not have created themselves. Every chown and chmod
// here acts on an open handle, never on a path, because the folders involved
// are writable by the user's own processes.

// Owner is whom a root bridge hands the entries it creates to. The zero value
// hands nothing over.
type Owner struct {
	UID, GID uint32
	Set      bool
}

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

// OpenNearestDir opens the deepest folder of dirRel that exists and returns
// the names below it that are missing, outermost first, for MkdirAllAt.
func OpenNearestDir(root *fsroot.FSRoot, dirRel string) (*os.File, []string, error) {
	var missing []string
	existing := dirRel
	for {
		_, err := root.Root.Stat(existing)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) || existing == "." {
			return nil, nil, err
		}
		missing = append(missing, filepath.Base(existing))
		existing = filepath.Dir(existing)
	}
	dir, err := root.Root.OpenFile(existing, os.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, nil, err
	}
	slices.Reverse(missing)
	return dir, missing, nil
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
	dir, err := openDirAt(parent, name)
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

// openDirAt opens the folder name in parent, refusing a symlink at name.
func openDirAt(parent *os.File, name string) (*os.File, error) {
	path := filepath.Join(parent.Name(), name)
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "openat", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

// openDirNoUserLinks opens the folder at path for a root bridge acting for
// owner, walking from / one name at a time. A symlink is followed only where
// the session user cannot have planted it: in a folder that is not theirs and
// that no group or everyone may write (a group the user may be in). So
// /home -> /var/home works, while a link a process of the user put in a
// folder of theirs, say after a check ran on the path, is refused. Without
// owner set, path is opened as it resolves.
func openDirNoUserLinks(root *fsroot.FSRoot, path string, owner Owner) (*os.File, error) {
	if !owner.Set {
		return root.Root.OpenFile(relPath(path), os.O_RDONLY|syscall.O_DIRECTORY, 0)
	}
	dir, err := os.Open("/")
	if err != nil {
		return nil, err
	}
	names := strings.Split(path, "/")
	for links := 0; len(names) > 0; {
		name := names[0]
		names = names[1:]
		if name == "" || name == "." {
			continue
		}
		next, err := openDirAt(dir, name)
		if err != nil && links < maxLinkHops {
			if target, isLink := linkTarget(dir, name); isLink {
				links++
				if dir, err = followLink(dir, name, target, owner); err != nil {
					return nil, err
				}
				names = append(strings.Split(target, "/"), names...)
				continue
			}
		}
		_ = dir.Close()
		if err != nil {
			return nil, err
		}
		dir = next
	}
	return dir, nil
}

// maxLinkHops is how many symlinks a walk follows before giving up, as the
// kernel's own limit for a path.
const maxLinkHops = 40

// linkTarget returns the target of name in dir when it is a symlink.
func linkTarget(dir *os.File, name string) (string, bool) {
	buf := make([]byte, unix.PathMax)
	n, err := unix.Readlinkat(int(dir.Fd()), name, buf)
	if err != nil {
		return "", false
	}
	return string(buf[:n]), true
}

// followLink checks that the session user cannot have put the symlink name
// in dir, where target is what it points at: dir is not theirs and no group
// or everyone may write it. It returns the folder target resolves from, dir
// for a relative target and / for an absolute one, and takes over dir.
func followLink(dir *os.File, name, target string, owner Owner) (*os.File, error) {
	var st unix.Stat_t
	err := unix.Fstat(int(dir.Fd()), &st)
	if err == nil && (st.Uid == owner.UID || st.Mode&0o022 != 0) {
		err = fmt.Errorf("%w: %s is a link in a folder you can write", bridgeipc.ErrForbidden, filepath.Join(dir.Name(), name))
	}
	if err != nil || filepath.IsAbs(target) {
		_ = dir.Close()
	}
	if err != nil {
		return nil, err
	}
	if filepath.IsAbs(target) {
		return os.Open("/")
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
