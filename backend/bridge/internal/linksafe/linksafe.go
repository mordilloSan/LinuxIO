// Package linksafe reaches folders for a root bridge acting for a session
// user, without following a symlink that anyone but root could have planted
// on the way. See docs/privilege_pattern.md, "Root acting in the session
// user's folders".
package linksafe

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
)

// Owner is the session user a root bridge acts for: whose planted links it
// refuses to follow, and whom the file browser hands the entries it creates
// to. The zero value is nobody: no link rule and nothing handed over.
type Owner struct {
	UID, GID uint32
	Set      bool
}

// ActingFor is whom a bridge acts for when it changes files for the session
// user uid: that user on a root bridge, which then never follows a link that
// user could have planted on the way (see OpenNearestDirNoUserLinks); nobody
// on a bridge that is not root, which already acts as that user, and nobody
// for a root session, whose user is no less trusted than the bridge and would
// otherwise be refused system links such as /bin -> usr/bin.
func ActingFor(uid, gid uint32) Owner {
	if os.Geteuid() != 0 || uid == 0 {
		return Owner{}
	}
	return Owner{UID: uid, GID: gid, Set: true}
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

// OpenNearestDirNoUserLinks is OpenNearestDir for a bridge acting for owner:
// each folder is reached as OpenDirNoUserLinks reaches it, so on a root
// bridge a symlink in a folder anyone but root can write is refused, not
// followed, while one such as /home -> /var/home still is. Without owner set
// it is OpenNearestDir.
func OpenNearestDirNoUserLinks(root *fsroot.FSRoot, path string, owner Owner) (*os.File, []string, error) {
	path = fsroot.CleanAbsPath(path)
	if !owner.Set {
		return OpenNearestDir(root, fsroot.ToRel(path))
	}
	var missing []string
	for existing := path; ; existing = filepath.Dir(existing) {
		dir, err := OpenDirNoUserLinks(root, existing, owner)
		if err == nil {
			slices.Reverse(missing)
			return dir, missing, nil
		}
		if !errors.Is(err, fs.ErrNotExist) || existing == "/" {
			return nil, nil, err
		}
		missing = append(missing, filepath.Base(existing))
	}
}

// OpenDirAt opens the folder name in parent, refusing a symlink at name.
func OpenDirAt(parent *os.File, name string) (*os.File, error) {
	path := filepath.Join(parent.Name(), name)
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "openat", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

// OpenDirNoUserLinks opens the folder at path for a root bridge acting for
// owner, walking from / one name at a time. A symlink is followed only where
// nobody but root can have planted it: in a folder root owns that no group
// or everyone may write. So /home -> /var/home works, while a link a process
// of the session user put in a folder of theirs, say after a check ran on the
// path, is refused, and so is one another local user or a service account
// put in a folder of its own, which would send an admin working there
// wherever that account chose. Without owner set, path is opened as it
// resolves.
func OpenDirNoUserLinks(root *fsroot.FSRoot, path string, owner Owner) (*os.File, error) {
	if !owner.Set {
		return root.Root.OpenFile(fsroot.ToRel(path), os.O_RDONLY|syscall.O_DIRECTORY, 0)
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
		next, err := OpenDirAt(dir, name)
		if err != nil && links < MaxLinkHops {
			if target, isLink := LinkTarget(dir, name); isLink {
				links++
				if dir, err = FollowLink(dir, name, target); err != nil {
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

// MaxLinkHops is how many symlinks a walk follows before giving up, as the
// kernel's own limit for a path.
const MaxLinkHops = 40

// LinkTarget returns the target of name in dir when it is a symlink.
func LinkTarget(dir *os.File, name string) (string, bool) {
	buf := make([]byte, unix.PathMax)
	n, err := unix.Readlinkat(int(dir.Fd()), name, buf)
	if err != nil {
		return "", false
	}
	return string(buf[:n]), true
}

// FollowLink checks that nobody but root can have put the symlink name in
// dir, where target is what it points at (see Plantable). It returns the
// folder target resolves from, dir for a relative target and / for an
// absolute one, and takes over dir.
func FollowLink(dir *os.File, name, target string) (*os.File, error) {
	planted, err := Plantable(dir)
	if err == nil && planted {
		err = plantedLinkError(dir, name, target)
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

// Plantable reports whether anyone but root could have put an entry in dir,
// as fstat sees its handle: dir is not root's, or a group or everyone may
// write it. The owner of a folder can put a link in it whatever its mode, so
// only root's own folders vouch for the links they hold.
func Plantable(dir *os.File) (bool, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(dir.Fd()), &st); err != nil {
		return false, &fs.PathError{Op: "fstat", Path: dir.Name(), Err: err}
	}
	return st.Uid != 0 || st.Mode&0o022 != 0, nil
}

// plantedLinkError refuses the symlink name in dir, which points at target,
// because someone other than root could have planted it. It names what the
// link points to, which readlink tells anyone who can list dir anyway, so
// whoever meant the link knows which path to use instead. The page offers to
// open that path: it finds it by the wording here (linkRefusal.ts, pinned by
// a test).
func plantedLinkError(dir *os.File, name, target string) error {
	return fmt.Errorf("%w: %s is a link to %s. Open that folder to make this change",
		bridgeipc.ErrForbidden, filepath.Join(dir.Name(), name), LinkDestination(dir, target))
}

// LinkDestination is target, read from a symlink in dir, as an absolute
// path: a relative target resolves against dir, whose name is its real path
// when it was reached one name at a time from /.
func LinkDestination(dir *os.File, target string) string {
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Join(dir.Name(), target)
}
