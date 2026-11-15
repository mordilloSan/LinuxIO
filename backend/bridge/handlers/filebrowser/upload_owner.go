package filebrowser

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/linksafe"
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
	errUploadEnded          = errors.New("upload has ended")
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
// files (see linksafe.ActingFor).
func actingFor(session sessionOwner) services.Owner {
	if !session.set {
		return services.Owner{}
	}
	return linksafe.ActingFor(session.uid, session.gid)
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
// planted (see linksafe.OpenNearestDirNoUserLinks), so its save cannot be
// sent elsewhere. New folders go to owner, which is set only when the upload
// asked for it.
func openUploadDir(root *fsroot.FSRoot, dirRel string, linkRule services.Owner, owner sessionOwner) (*os.File, error) {
	dir, missing, err := linksafe.OpenNearestDirNoUserLinks(root, dirRel, linkRule)
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
// there beforehand, a symlink included, is refused rather than reused, and
// returns it open. The upload holds it open until it commits or ends and
// writes every chunk through it, across reconnects of the data stream, so a
// file that a process able to write the folder puts at the buffer's name
// never receives a byte: reopening by name would have to tell the two apart,
// and ext4 gives a deleted file's inode number to the next file created.
func createUploadBuffer(dir *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(privateUploadMode)) // owner-only until commit sets the target mode
	if err != nil {
		return nil, &fs.PathError{Op: "create", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}

// checkUploadBuffer refuses a commit unless name in dir is still buffer, the
// file the upload created and wrote, which it still holds open. An open
// file's inode is not freed, so its number is not given to another file
// while the upload holds it. It is checked before the buffer is handed over,
// so a refused buffer is still the bridge's and private, and again right
// before the commit, which goes by name as rename has no form that takes an
// open file.
func checkUploadBuffer(dir *os.File, name string, buffer *os.File) error {
	var held, named unix.Stat_t
	if err := unix.Fstat(int(buffer.Fd()), &held); err != nil {
		return &fs.PathError{Op: "fstat", Path: buffer.Name(), Err: err}
	}
	if err := unix.Fstatat(int(dir.Fd()), name, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return &fs.PathError{Op: "fstatat", Path: name, Err: err}
	}
	if held.Dev != named.Dev || held.Ino != named.Ino {
		return errUploadBufferReplaced
	}
	return nil
}

// openUploadFolder opens dirRel, the folder an upload's buffer and target
// share, only while it is still want, the folder prepare used: any other
// folder now at that path, such as one swapped for a symlink, is refused.
func openUploadFolder(root *fsroot.FSRoot, dirRel string, want os.FileInfo) (*os.File, error) {
	dir, err := root.Root.OpenFile(dirRel, os.O_RDONLY|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
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
