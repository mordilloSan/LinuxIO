// Package linksafe walks paths from / one name at a time, by handles, for
// every file browser path: the folders a request names are resolved here
// where it enters, and folders are reached here when files change. For a
// root bridge acting for a session user it follows a symlink only where
// nobody but root could have planted it; otherwise it follows every link as
// the session user's own programs would. See docs/privilege_pattern.md, "Root
// acting in the session user's folders" and "Paths through links".
package linksafe

import (
	"context"
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
// followed, while one such as /home -> /var/home still is. A name inside a
// link's target that does not exist is an error, not a missing name, so
// nothing is ever made where a dangling link points. root is not used: the
// walk starts at /.
func OpenNearestDirNoUserLinks(_ *fsroot.FSRoot, path string, owner Owner) (*os.File, []string, error) {
	w := walk{ctx: context.Background(), owner: owner}
	return w.folders(fsroot.CleanAbsPath(path), true)
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

// OpenDirNoUserLinks opens the folder at path, walking from / one name at a
// time and following the symlinks on the way as the kernel would, a link with
// an absolute target included (os.Root refuses one). The folder it returns is
// named by its real path. For a root bridge acting for owner a symlink is
// followed only where nobody but root can have planted it: in a folder root
// owns that no group or everyone may write. So /home -> /var/home works,
// while a link a process of the session user put in a folder of theirs, say
// after a check ran on the path, is refused, and so is one another local user
// or a service account put in a folder of its own, which would send an admin
// working there wherever that account chose. Without owner set every link is
// followed: the bridge then acts as the session user, whose permissions the
// kernel checks, or reads. root is not used: the walk starts at /.
func OpenDirNoUserLinks(_ *fsroot.FSRoot, path string, owner Owner) (*os.File, error) {
	w := walk{ctx: context.Background(), owner: owner}
	dir, _, err := w.folders(path, false)
	return dir, err
}

// OpenEntryFollowing returns the folder the entry at path resolves into, open
// and named by its real path, and the entry's name in it ("." for /). A
// symlink at path, and one it leads to, is followed as OpenDirNoUserLinks
// follows one on the way: for a root bridge acting for owner only one nobody
// but root can have planted. The entry itself need not exist.
func OpenEntryFollowing(ctx context.Context, path string, owner Owner) (*os.File, string, error) {
	w := walk{ctx: ctx, owner: owner}
	return w.entry(fsroot.CleanAbsPath(path))
}

// Resolve returns the real path of the entry at path, following a symlink at
// path as OpenEntryFollowing does: path whole, its last name included,
// resolved under the rule ResolveFolders applies to the folders above it.
func Resolve(ctx context.Context, path string, owner Owner) (string, error) {
	dir, name, err := OpenEntryFollowing(ctx, path, owner)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	return filepath.Join(dir.Name(), name), nil
}

// ResolveFolders is where a path the file browser was asked about enters: it
// returns path with the folders above its last name resolved. That is the
// real path of the deepest of those folders that exists, reached as
// OpenNearestDirNoUserLinks reaches it, then the names below it that do not
// exist yet, then the last name, which is not followed. The path returned
// names the same entry through folders that held no symlink when it was
// resolved, so os.Root, which refuses a link with an absolute target, can
// reach it, and a check such as CheckNotProtected judges the entry where it
// really is rather than by the name a link gave it.
//
// For a root bridge acting for owner it follows a link only where nobody but
// root can have planted it and refuses any other with the error that names
// where it points, exactly as the change's own walk would. The path is a name
// only, stale as soon as it is returned: a change made for owner reaches it
// again from / by handles under the same rule (OpenDirNoUserLinks), so a
// folder on it swapped for a link meanwhile is refused then, never followed.
// Without owner set every link is followed, as the session user's own
// programs would follow it. Either way the resolution is bounded as a path
// lookup is (see walk) and stops when ctx is done.
func ResolveFolders(ctx context.Context, path string, owner Owner) (string, error) {
	path = fsroot.CleanAbsPath(path)
	if path == "/" {
		return path, nil
	}
	w := walk{ctx: ctx, owner: owner}
	dir, missing, err := w.folders(filepath.Dir(path), true)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	return filepath.Join(dir.Name(), filepath.Join(missing...), filepath.Base(path)), nil
}

// MaxLinkHops is how many symlinks one resolution follows before giving up,
// the kernel's own limit for a path.
const MaxLinkHops = 40

// maxWalkSteps is how many names one resolution opens before giving up. A
// link's target can name some two thousand folders, so forty links could
// send a walk by handles through tens of thousands of opens where the kernel
// takes milliseconds; os.Root bounds its walks the same way. No path a person
// browses comes near it.
const maxWalkSteps = 4096

// walk is one resolution. Every folder it opens and every link it follows
// count against one budget, whatever part of the path they belong to (a
// folder on the way, the last name or a link's target), as in one path
// lookup by the kernel, and ctx stops it between two names.
type walk struct {
	ctx   context.Context
	owner Owner
	links int
	steps int
}

// step counts one more name opened in dir and fails once the walk has opened
// too many or its ctx is done.
func (w *walk) step(dir *os.File, name string) error {
	w.steps++
	if w.steps > maxWalkSteps {
		return &fs.PathError{Op: "open", Path: filepath.Join(dir.Name(), name), Err: unix.ENAMETOOLONG}
	}
	return w.ctx.Err()
}

// follow follows the symlink name in dir, which points at target, as
// FollowLink does, once the walk has links left. It takes over dir.
func (w *walk) follow(dir *os.File, name, target string) (*os.File, error) {
	if w.links == MaxLinkHops {
		err := &fs.PathError{Op: "open", Path: filepath.Join(dir.Name(), name), Err: unix.ELOOP}
		_ = dir.Close()
		return nil, err
	}
	w.links++
	return FollowLink(dir, name, target, w.owner)
}

// folders opens the folder at path, from /. With nearest, a name of path
// itself that does not exist ends the walk: the deepest folder reached comes
// back with that name and those after it as missing. A name of a link's
// target that does not exist is an error either way.
func (w *walk) folders(path string, nearest bool) (*os.File, []string, error) {
	dir, err := os.Open("/")
	if err != nil {
		return nil, nil, err
	}
	return w.below(dir, strings.Split(path, "/"), nearest)
}

// below walks names from dir, which it takes over, following every symlink
// on the way under the walk's rule and taking .. where the folder reached
// really is, and returns the folder the names lead to. With nearest it stops
// at the first of names that does not exist, as folders does.
func (w *walk) below(dir *os.File, names []string, nearest bool) (*os.File, []string, error) {
	pending := 0 // how many names at the front of names come from a link's target
	for len(names) > 0 {
		name := names[0]
		names = names[1:]
		fromTarget := pending > 0
		if fromTarget {
			pending--
		}
		if name == "" || name == "." {
			continue
		}
		if err := w.step(dir, name); err != nil {
			_ = dir.Close()
			return nil, nil, err
		}
		next, err := OpenDirAt(dir, name)
		if err == nil {
			_ = dir.Close()
			dir = next
			continue
		}
		if target, isLink := LinkTarget(dir, name); isLink {
			if dir, err = w.follow(dir, name, target); err != nil {
				return nil, nil, err
			}
			parts := strings.Split(target, "/")
			names = append(parts, names...)
			pending += len(parts)
			continue
		}
		if nearest && !fromTarget && errors.Is(err, fs.ErrNotExist) {
			return dir, append([]string{name}, names...), nil
		}
		_ = dir.Close()
		return nil, nil, err
	}
	return dir, nil, nil
}

// entry returns the folder the entry at path resolves into and the entry's
// name in it, following a symlink at the entry and the ones it leads to. A
// target is walked as the folders on the way are, so a .. in it is taken
// where the kernel takes it; a target that ends in ., .. or a slash names the
// folder it leads to as the entry, and fails with ENOTDIR when that is no
// folder.
func (w *walk) entry(path string) (*os.File, string, error) {
	if path == "/" {
		dir, err := os.Open("/")
		return dir, ".", err
	}
	dir, _, err := w.folders(filepath.Dir(path), false)
	if err != nil {
		return nil, "", err
	}
	name := filepath.Base(path)
	for {
		if err = w.step(dir, name); err != nil {
			_ = dir.Close()
			return nil, "", err
		}
		target, isLink := LinkTarget(dir, name)
		if !isLink {
			return dir, name, nil
		}
		if dir, err = w.follow(dir, name, target); err != nil {
			return nil, "", err
		}
		parts := strings.Split(target, "/")
		name = parts[len(parts)-1]
		if name == "" || name == "." || name == ".." {
			// The target names a folder as the entry itself: /, ., .. or
			// one that ends in a slash, which must then be a folder, as the
			// kernel has it.
			if dir, _, err = w.below(dir, parts, false); err != nil {
				return nil, "", err
			}
			return entryOfFolder(dir)
		}
		if dir, _, err = w.below(dir, parts[:len(parts)-1], false); err != nil {
			return nil, "", err
		}
	}
}

// entryOfFolder returns the folder dir sits in, opened by its handle, and
// dir's name there; for / it returns / and ".". It takes over dir.
func entryOfFolder(dir *os.File) (*os.File, string, error) {
	if dir.Name() == "/" {
		return dir, ".", nil
	}
	parent, err := OpenDirAt(dir, "..")
	name := filepath.Base(dir.Name())
	_ = dir.Close()
	if err != nil {
		return nil, "", err
	}
	return parent, name, nil
}

// LinkTarget returns the target of name in dir when it is a symlink.
func LinkTarget(dir *os.File, name string) (string, bool) {
	buf := make([]byte, unix.PathMax)
	n, err := unix.Readlinkat(int(dir.Fd()), name, buf)
	if err != nil {
		return "", false
	}
	return string(buf[:n]), true
}

// FollowLink returns the folder target, what the symlink name in dir points
// at, resolves from: dir for a relative target and / for an absolute one. It
// takes over dir. For a bridge acting for owner it first checks that nobody
// but root can have put the link there (see Plantable).
func FollowLink(dir *os.File, name, target string, owner Owner) (*os.File, error) {
	var err error
	if owner.Set {
		var planted bool
		if planted, err = Plantable(dir); err == nil && planted {
			err = plantedLinkError(dir, name, target)
		}
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
