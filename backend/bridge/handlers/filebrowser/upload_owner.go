package filebrowser

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

// A root bridge keeps what it creates root-owned (backend audit item 110). An
// upload may opt out with owner "session": the entries it creates are handed
// to the session user, but only inside a folder that user already owns, so
// the user never gets an entry they could not have created themselves. Every
// chown and chmod here acts on an open handle, never on a path, because the
// folders involved are writable by the user's own processes.

const uploadOwnerSession apischema.FileUploadOwner = "session"

// Modes of what an upload hands to the session user. Docker runs compose files
// as root, so they must not be writable by a primary group others may share.
// A private file (private set) is owner-only either way.
const (
	handedDirMode     os.FileMode = 0o755
	handedFileMode    os.FileMode = 0o644
	privateUploadMode os.FileMode = 0o600
)

var (
	errUploadBufferReplaced = errors.New("upload buffer was replaced")
	errUploadFolderReplaced = errors.New("upload folder was replaced")
)

// uploadNewOwner returns whom the entries an upload creates are handed to.
// The ids are always the authenticated session's: the request can only ask
// for "session". A bridge that is not root already writes as the session
// user, and without a session owner there is nobody to hand them to, so both
// hand nothing over.
func uploadNewOwner(requested apischema.FileUploadOwner, session sessionOwner, euid int) (sessionOwner, error) {
	switch requested {
	case "":
		return sessionOwner{}, nil
	case uploadOwnerSession:
		if euid != 0 || !session.set {
			return sessionOwner{}, nil
		}
		return session, nil
	default:
		return sessionOwner{}, fmt.Errorf("%w: unknown upload owner %q", bridgeipc.ErrInvalidArgs, requested)
	}
}

// handsOverIn reports whether an entry created in a folder goes to owner:
// only when owner is set and the folder, as fstat saw its open handle,
// already belongs to them.
func handsOverIn(folder os.FileInfo, owner sessionOwner) bool {
	if !owner.set {
		return false
	}
	st, ok := folder.Sys().(*syscall.Stat_t)
	return ok && st.Uid == owner.uid
}

// isSetgid reports whether entries created in folder inherit its group.
func isSetgid(folder os.FileInfo) bool {
	return folder.Mode()&os.ModeSetgid != 0
}

// openUploadDir opens dirRel, the folder the upload lands in, creating any
// missing folders on the way. It walks down from the deepest folder that
// already exists through open handles, so the folders it creates are made and
// handed over relative to the handle of their parent, never by path. Folders
// that already exist are only opened.
func openUploadDir(root *fsroot.FSRoot, dirRel string, owner sessionOwner) (*os.File, error) {
	var missing []string
	existing := dirRel
	for {
		_, err := root.Root.Stat(existing)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrNotExist) || existing == "." {
			return nil, err
		}
		missing = append(missing, filepath.Base(existing))
		existing = filepath.Dir(existing)
	}
	dir, err := root.Root.OpenFile(existing, os.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	for _, name := range slices.Backward(missing) {
		next, err := mkdirUploadDir(dir, name, owner)
		_ = dir.Close()
		if err != nil {
			return nil, err
		}
		dir = next
	}
	return dir, nil
}

// mkdirUploadDir makes name in parent and returns it open. Only a folder this
// call made (mkdirat succeeded, so not EEXIST) inside a folder owner owns is
// handed over (see handOverDir); any other folder keeps the bridge's
// ownership and the service umask, as MkdirAll leaves it.
func mkdirUploadDir(parent *os.File, name string, owner sessionOwner) (*os.File, error) {
	parentInfo, err := parent.Stat()
	if err != nil {
		return nil, err
	}
	err = unix.Mkdirat(int(parent.Fd()), name, uint32(services.PermDir))
	created := err == nil
	if err != nil && !errors.Is(err, unix.EEXIST) {
		return nil, &fs.PathError{Op: "mkdirat", Path: name, Err: err}
	}
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "openat", Path: name, Err: err}
	}
	dir := os.NewFile(uintptr(fd), filepath.Join(parent.Name(), name))
	if created && handsOverIn(parentInfo, owner) {
		if err := handOverDir(dir, owner); err != nil {
			_ = dir.Close()
			return nil, err
		}
	}
	return dir, nil
}

// handOverDir hands the folder mkdirUploadDir just made to owner with mode
// 0755. Between mkdirat and openat a process that can write the parent could
// have put another folder under the name, so only a folder the bridge owns
// that is still empty, as a new one is, is handed over. A folder made in a
// setgid folder keeps the setgid bit and the group it inherited, as if the
// user had made it there.
func handOverDir(dir *os.File, owner sessionOwner) error {
	info, err := dir.Stat()
	if err != nil {
		return err
	}
	fresh, err := isFreshUploadDir(dir, info)
	if err != nil || !fresh {
		return err
	}
	return handOver(dir, owner, handedDirMode|info.Mode()&os.ModeSetgid, isSetgid(info))
}

// isFreshUploadDir reports whether dir, described by info, looks like the
// folder the bridge has just made: owned by the bridge's euid and empty.
func isFreshUploadDir(dir *os.File, info os.FileInfo) (bool, error) {
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

// handOver gives an entry this upload created, held open, to owner and sets
// its mode. In a setgid folder the entry keeps the group it inherited.
// Ownership goes first because a chown clears setuid and setgid.
func handOver(f *os.File, owner sessionOwner, mode os.FileMode, inSetgidFolder bool) error {
	gid := int(owner.gid)
	if inSetgidFolder {
		gid = -1
	}
	if err := f.Chown(int(owner.uid), gid); err != nil {
		return fmt.Errorf("hand %s to session user: %w", f.Name(), err)
	}
	if err := f.Chmod(mode); err != nil {
		return fmt.Errorf("set permissions of %s: %w", f.Name(), err)
	}
	return nil
}

// createUploadBuffer creates the buffer exclusively in dir, so a name planted
// there beforehand, a symlink included, is refused rather than reused. It
// returns the new file's info for openUploadBuffer to check against.
func createUploadBuffer(dir *os.File, name string) (os.FileInfo, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(privateUploadMode)) // owner-only until commit sets the target mode
	if err != nil {
		return nil, &fs.PathError{Op: "create", Path: name, Err: err}
	}
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return info, err
}

// openUploadFolder opens dirRel, the folder an upload's buffer and target
// share. A non-nil want is the folder prepare used: any other folder now at
// that path, such as one swapped for a symlink, is refused.
func openUploadFolder(root *fsroot.FSRoot, dirRel string, want os.FileInfo) (*os.File, error) {
	dir, err := root.Root.OpenFile(dirRel, os.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil || want == nil {
		return dir, err
	}
	info, err := dir.Stat()
	if err == nil && !os.SameFile(info, want) {
		err = errUploadFolderReplaced
	}
	if err != nil {
		_ = dir.Close()
		return nil, err
	}
	return dir, nil
}

// openUploadBuffer reopens the buffer createUploadBuffer made, without
// following a symlink at its name and only if it is still that same file, so
// a buffer swapped by a process that can write the folder is refused instead
// of written to or handed over.
func openUploadBuffer(root *fsroot.FSRoot, tempRel string, created os.FileInfo) (*os.File, error) {
	dir, err := openUploadFolder(root, filepath.Dir(tempRel), nil)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	fd, err := unix.Openat(int(dir.Fd()), filepath.Base(tempRel), unix.O_RDWR|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: tempRel, Err: err}
	}
	file := os.NewFile(uintptr(fd), tempRel)
	info, err := file.Stat()
	if err == nil && !os.SameFile(info, created) {
		err = errUploadBufferReplaced
	}
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

// settleUploadBuffer gives the still-open buffer its final owner and mode
// before it is closed and committed, through the handle, so a name swapped
// under the buffer is never touched. An overwrite keeps the replaced file's
// owner and mode; a new file gets mode and, when owner is set, goes to the
// session user, keeping the group it inherited in a setgid folder.
func settleUploadBuffer(file *os.File, attrs uploadAttributes, owner sessionOwner, mode os.FileMode, inSetgidFolder bool) error {
	if attrs.hasExisting {
		// Only root may hand files to other users or groups; an unprivileged
		// session keeps its own ownership, as cp -p does (see chownLike).
		if err := file.Chown(attrs.uid, attrs.gid); err != nil && !errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("restore upload ownership: %w", err)
		}
		if err := file.Chmod(attrs.mode); err != nil {
			return fmt.Errorf("restore upload permissions: %w", err)
		}
		return nil
	}
	if owner.set {
		return handOver(file, owner, mode, inSetgidFolder)
	}
	if err := file.Chmod(mode); err != nil {
		return fmt.Errorf("set upload permissions: %w", err)
	}
	return nil
}
