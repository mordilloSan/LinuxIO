package filebrowser

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
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

// asOwner is whom the services hand the entries they create to.
func (o sessionOwner) asOwner() services.Owner {
	return services.Owner{UID: o.uid, GID: o.gid, Set: o.set}
}

// actingFor is whom a bridge acts for when it changes the session user's
// files: that user on a root bridge, which then never follows a link that
// user could have planted on the way (see services.OpenNearestDirNoUserLinks);
// nobody on a bridge that is not root, which already acts as that user, and
// nobody for a root session, whose user is no less trusted than the bridge
// and would otherwise be refused system links such as /bin -> usr/bin.
func actingFor(session sessionOwner) services.Owner {
	if os.Geteuid() != 0 || session.uid == 0 {
		return services.Owner{}
	}
	return session.asOwner()
}

// handsOverIn reports whether an entry created in a folder goes to owner
// (see services.HandsOverIn).
func handsOverIn(folder os.FileInfo, owner sessionOwner) bool {
	return services.HandsOverIn(folder, owner.asOwner())
}

// isSetgid reports whether entries created in folder inherit its group.
func isSetgid(folder os.FileInfo) bool {
	return folder.Mode()&os.ModeSetgid != 0
}

// openUploadDir opens dirRel, the folder the upload lands in, creating any
// missing folders on the way. It walks down from the deepest folder that
// already exists through open handles, so the folders it creates are made and
// handed over relative to the handle of their parent, never by path. Folders
// that already exist are only opened. A root bridge reaches that folder for
// linkRule, the session user it acts for (see actingFor), whether or not the
// upload sets an owner, without following a link that user could have
// planted (see services.OpenNearestDirNoUserLinks), so its save cannot be
// sent elsewhere. New folders go to owner, which is set only when the upload
// asked for it.
func openUploadDir(root *fsroot.FSRoot, dirRel string, linkRule services.Owner, owner sessionOwner) (*os.File, error) {
	dir, missing, err := services.OpenNearestDirNoUserLinks(root, dirRel, linkRule)
	if err != nil {
		return nil, err
	}
	return services.MkdirAllAt(dir, missing, services.PermDir, owner.asOwner())
}

// handOver gives an entry this upload created, held open, to owner and sets
// its mode (see services.HandOver).
func handOver(f *os.File, owner sessionOwner, mode os.FileMode, inSetgidFolder bool) error {
	return services.HandOver(f, owner.asOwner(), mode, inSetgidFolder)
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
