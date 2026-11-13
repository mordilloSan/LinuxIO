package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
)

type uploadAttributes struct {
	mode        os.FileMode
	uid         int
	gid         int
	hasExisting bool
	modTime     time.Time // zero keeps the write time
}

type uploadTransferTask struct {
	*transferLifecycle
	ctx             context.Context
	path            string
	expectedSize    int64
	expectedVersion string
	overwrite       bool
	newOwner        sessionOwner   // set when a root bridge hands what this upload creates to the session user
	linkRule        services.Owner // the session user a root bridge acts for: no link they could plant is followed (see actingFor)
	private         bool           // a new file is owner-only (0600)
	refuseSymlink   bool           // the request set owner: a symlink at the target is refused, never followed

	// finalRel, tempRel, attrs, folderInfo, bufferID and fileOwner are
	// guarded by mu.
	finalRel   string
	tempRel    string
	attrs      uploadAttributes
	folderInfo os.FileInfo    // the folder prepare put the buffer in; the commit must find this folder
	bufferID   uploadBufferID // the buffer prepare created; every reopen must find this file
	fileOwner  sessionOwner   // newOwner when the buffer's folder is already theirs
}

var errUploadVersionConflict = errors.New("file changed since it was opened")

var uploadCommitMu sync.Mutex // ponytail: global CAS lock; use per-path locks if upload throughput requires it.

func parseUploadRequest(req apischema.FileUploadRequest) (string, int64, bool, string, error) {
	if req.TargetPath == "" || req.Size == "" {
		return "", 0, false, "", fmt.Errorf("missing path or size")
	}

	expectedSize, err := strconv.ParseInt(req.Size, 10, 64)
	if err != nil {
		return "", 0, false, "", fmt.Errorf("invalid size: %w", err)
	}

	expectedVersion := ""
	if req.ExpectedVersion != nil {
		expectedVersion = *req.ExpectedVersion
	}
	return req.TargetPath, expectedSize, req.Overwrite != nil && *req.Overwrite, expectedVersion, nil
}

// refuseFolderTarget refuses an upload onto a folder, or onto a link to one,
// before any missing folder is made. It goes by path and decides nothing
// else: the owner and mode come from uploadAttributesAt.
func refuseFolderTarget(root *fsroot.FSRoot, realRel string) error {
	if info, err := root.Root.Stat(realRel); err == nil && info.IsDir() {
		return fmt.Errorf("destination is a directory")
	}
	return nil
}

// uploadAttributesAt reads the owner and mode of the file the commit
// replaces: the entry name in dir itself, through the handle of the folder
// the commit works in, so a folder swapped for a link after it was opened
// lends no other file's owner or mode. The commit's renameat replaces a
// symlink there, not its target, so the target lends none either (a setuid
// bit included).
func uploadAttributesAt(dir *os.File, name string) (uploadAttributes, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return uploadAttributes{}, nil
	}
	entry := os.NewFile(uintptr(fd), name)
	defer entry.Close()
	existingStat, err := entry.Stat()
	switch {
	case err != nil, existingStat.Mode()&os.ModeSymlink != 0:
		return uploadAttributes{}, nil
	case existingStat.IsDir():
		return uploadAttributes{}, fmt.Errorf("destination is a directory")
	}

	attrs := uploadAttributes{mode: existingStat.Mode()}
	if st, ok := existingStat.Sys().(*syscall.Stat_t); ok {
		attrs.uid = int(st.Uid)
		attrs.gid = int(st.Gid)
		attrs.hasExisting = true
	}
	return attrs, nil
}

// removeUploadBuffer removes the buffer at tempRel through the handle of its
// folder, and only while that is still the folder the buffer was created in,
// so a folder swapped for a link names no other file to remove.
func removeUploadBuffer(root *fsroot.FSRoot, tempRel string, folder os.FileInfo) error {
	dir, err := openUploadFolder(root, filepath.Dir(tempRel), folder)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err = unix.Unlinkat(int(dir.Fd()), filepath.Base(tempRel), 0); err != nil {
		return &fs.PathError{Op: "unlinkat", Path: tempRel, Err: err}
	}
	return nil
}

// refuseSymlinkTarget refuses an upload that sets owner when name in dir is a
// symbolic link. Such an upload writes into a user's folder on their behalf,
// where any process running as the user can plant .env -> /etc/environment,
// so it never writes through a link there. fstatat on the open folder sees the
// entry the commit's renameat would replace, so no path walk can be raced.
func refuseSymlinkTarget(dir *os.File, name string) error {
	var st unix.Stat_t
	if unix.Fstatat(int(dir.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW) == nil && st.Mode&unix.S_IFMT == unix.S_IFLNK {
		return fmt.Errorf("%w: %s is a symbolic link; replace it with a regular file", bridgeipc.ErrConflict, name)
	}
	return nil
}

// commitUploadBuffer moves the finished buffer tempName to finalName. Both
// live in dir and every step works on that one handle, so a folder swapped
// for a symlink between two path resolutions cannot send the buffer anywhere
// else. Without overwrite it links instead of renaming, so a file that
// appeared since the upload started is never replaced.
func commitUploadBuffer(dir *os.File, tempName, finalName string, overwrite bool) error {
	fd := int(dir.Fd())
	if overwrite {
		return renameInDir(fd, tempName, finalName)
	}
	if err := unix.Linkat(fd, tempName, fd, finalName, 0); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return &os.LinkError{Op: "linkat", Old: tempName, New: finalName, Err: err}
		}
		// No hard links here (FAT, SMB, some FUSE): fall back to the old
		// stat-then-rename rather than refusing every upload on that mount.
		var st unix.Stat_t
		if unix.Fstatat(fd, finalName, &st, unix.AT_SYMLINK_NOFOLLOW) == nil {
			return fs.ErrExist
		}
		return renameInDir(fd, tempName, finalName)
	}
	if err := unix.Unlinkat(fd, tempName, 0); err != nil {
		slog.Warn("failed to remove committed upload buffer", "path", filepath.Join(dir.Name(), tempName), "error", err)
	}
	return nil
}

func renameInDir(fd int, oldName, newName string) error {
	if err := unix.Renameat(fd, oldName, fd, newName); err != nil {
		return &os.LinkError{Op: "renameat", Old: oldName, New: newName, Err: err}
	}
	return nil
}

func runUploadTask(ctx context.Context, task *bridgeipc.Task, req apischema.FileUploadRequest, session sessionOwner) (any, error) {
	path, expectedSize, overwrite, expectedVersion, err := parseUploadRequest(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	newOwner, err := uploadNewOwner(req.Owner, session, os.Geteuid())
	if err != nil {
		return nil, err
	}
	if expectedVersion != "" && expectedSize >= services.MaxTextFileBytes {
		return nil, fmt.Errorf("%w: editor save exceeds the maximum text file size", bridgeipc.ErrInvalidArgs)
	}

	// Uploads never overwrite unless explicitly told to: fail the conflict
	// before the client streams any bytes.
	if !overwrite {
		root, rootErr := fsroot.Open()
		if rootErr != nil {
			return nil, fmt.Errorf("open filesystem root: %w", rootErr)
		}
		_, statErr := root.Root.Stat(fsroot.ToRel(filepath.Clean(path)))
		if closeErr := root.Close(); closeErr != nil {
			slog.Debug("failed to close filesystem root", "error", closeErr)
		}
		if statErr == nil {
			return nil, fmt.Errorf("%w: destination already exists", bridgeipc.ErrConflict)
		}
	}

	transfer := &uploadTransferTask{
		ctx:             ctx,
		path:            filepath.Clean(path),
		expectedSize:    expectedSize,
		expectedVersion: expectedVersion,
		overwrite:       overwrite,
		newOwner:        newOwner,
		linkRule:        actingFor(session),
		private:         req.Private != nil && *req.Private,
		refuseSymlink:   req.Owner != "",
	}
	transfer.transferLifecycle = newTransferLifecycle(task, transfer.cleanupPartial, transfer.reportProgress)
	defer registerTransfer(task.ID(), transfer)()

	transfer.reportProgress("waiting_for_client")
	return transfer.await(ctx)
}

func (t *uploadTransferTask) attach(stream net.Conn, req bridgeipc.TaskDataAttachRequest) error {
	offset, ok, err := t.attachStream(stream, req, t.expectedSize)
	if !ok {
		return err
	}
	defer t.endAttach(stream)

	root, err := fsroot.Open()
	if err != nil {
		return t.fail(stream, "failed to access filesystem", 500, fmt.Errorf("failed to access filesystem: %w", err))
	}
	defer root.Close()

	err = t.prepare(root)
	if err != nil {
		code := 500
		if errors.Is(err, errUploadVersionConflict) || errors.Is(err, bridgeipc.ErrConflict) {
			code = 409
		}
		return t.fail(stream, err.Error(), code, err)
	}

	t.mu.Lock()
	tempRel, bufferID := t.tempRel, t.bufferID
	t.mu.Unlock()
	file, err := openUploadBuffer(root, tempRel, bufferID)
	if err != nil {
		return t.fail(stream, fmt.Sprintf("cannot open upload buffer: %v", err), 500, err)
	}
	defer file.Close()

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return t.fail(stream, fmt.Sprintf("cannot resume upload: %v", err), 500, err)
	}

	t.writeProgress(stream, "uploading")
	gate := newTransferGate(uploadProgressAckIntervalBytes)
	return t.receiveFrames(stream,
		func(payload []byte) error { return t.writeUploadChunk(stream, file, payload, gate) },
		func() error {
			if t.isComplete() {
				return t.complete(stream, root, file)
			}
			t.markWaiting()
			return nil
		})
}

// resolveTarget returns the path a save without owner writes to when a
// symlink sits at path: as the user's own programs resolve it on a bridge
// that is not root, and, for the session user a root bridge acts for, only
// through a link that user cannot have planted (see
// services.ResolveNoUserLinks).
func (t *uploadTransferTask) resolveTarget(root *fsroot.FSRoot, path string) (string, error) {
	if !t.linkRule.Set {
		resolved, _, err := iteminfo.ResolveSymlinksAt(t.ctx, root, path)
		return resolved, err
	}
	return services.ResolveNoUserLinks(root, path, t.linkRule)
}

// targetPath returns the path the upload writes to. Without owner (editor
// saves, plain uploads) it writes through a symlink so the atomic rename
// replaces the target, not the link (dotfile repos, sites-enabled); a
// dangling link is replaced as-is. With owner the link is refused in prepare
// instead. A root bridge acting for the session user follows such a link only
// where that user cannot have planted it.
func (t *uploadTransferTask) targetPath(root *fsroot.FSRoot) (string, error) {
	realPath := filepath.Clean(t.path)
	if t.refuseSymlink {
		return realPath, nil
	}
	resolvedPath, err := t.resolveTarget(root, realPath)
	switch {
	case err == nil:
		return resolvedPath, nil
	case errors.Is(err, bridgeipc.ErrForbidden):
		return "", err
	}
	if ctxErr := t.ctx.Err(); ctxErr != nil {
		return "", ctxErr
	}
	return realPath, nil
}

func (t *uploadTransferTask) prepare(root *fsroot.FSRoot) error {
	t.mu.Lock()
	if t.tempRel != "" {
		t.mu.Unlock()
		return nil
	}
	t.mu.Unlock()

	realPath, err := t.targetPath(root)
	if err != nil {
		return err
	}
	realRel := fsroot.ToRel(realPath)
	if err = refuseFolderTarget(root, realRel); err != nil {
		return err
	}
	// Without owner the version is checked before any missing folder is made.
	if !t.refuseSymlink {
		if err = t.verifyExpectedVersion(root, realPath); err != nil {
			return err
		}
	}
	dir, err := openUploadDir(root, filepath.Dir(realRel), t.linkRule, t.newOwner)
	if err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	defer dir.Close()
	// With owner the link is refused first, so its target is never read.
	if t.refuseSymlink {
		if err = refuseSymlinkTarget(dir, filepath.Base(realRel)); err != nil {
			return err
		}
		if err = t.verifyExpectedVersion(root, realPath); err != nil {
			return err
		}
	}
	attrs, err := uploadAttributesAt(dir, filepath.Base(realRel))
	if err != nil {
		return err
	}
	folderInfo, err := dir.Stat()
	if err != nil {
		return fmt.Errorf("stat parent dir: %w", err)
	}

	partName := "." + filepath.Base(realRel) + ".linuxio-upload-" + t.task.ID() + ".part"
	bufferID, err := createUploadBuffer(dir, partName)
	if err != nil {
		return fmt.Errorf("create upload buffer: %w", err)
	}

	t.mu.Lock()
	t.finalRel = realRel
	t.tempRel = filepath.Join(filepath.Dir(realRel), partName)
	t.attrs = attrs
	t.folderInfo = folderInfo
	t.bufferID = bufferID
	if handsOverIn(folderInfo, t.newOwner) {
		t.fileOwner = t.newOwner
	}
	t.mu.Unlock()
	return nil
}

// newUploadFileMode is the mode a file an upload creates gets: owner-only
// when the request asked for a private file, 0644 when it is handed to the
// session user, otherwise the file browser's create mode.
func newUploadFileMode(private, handedOver bool) os.FileMode {
	switch {
	case private:
		return privateUploadMode
	case handedOver:
		return handedFileMode
	default:
		return services.PermFile
	}
}

func (t *uploadTransferTask) verifyExpectedVersion(root *fsroot.FSRoot, path string) error {
	if t.expectedVersion == "" {
		return nil
	}
	version, err := services.EditorFileVersion(t.ctx, root, path)
	if err != nil {
		return err
	}
	if version != t.expectedVersion {
		return errUploadVersionConflict
	}
	return nil
}

func (t *uploadTransferTask) writeUploadChunk(stream net.Conn, file *os.File, payload []byte, gate *progressThrottle) error {
	n, err := file.Write(payload)
	if err != nil {
		return t.fail(stream, fmt.Sprintf("write error: %v", err), 500, err)
	}
	if n != len(payload) {
		return t.fail(stream, "short write during upload", 500, io.ErrShortWrite)
	}

	t.mu.Lock()
	t.bytes += int64(n)
	bytes := t.bytes
	total := t.expectedSize
	t.mu.Unlock()
	signalActivity(t.activity)

	if total >= 0 && bytes > total {
		return t.fail(stream, fmt.Sprintf("size mismatch: expected %d, got at least %d", total, bytes), 400, fmt.Errorf("size mismatch"))
	}
	if _, _, ok := gate.Set(bytes, total); ok {
		t.writeProgress(stream, "uploading")
	}
	return nil
}

func (t *uploadTransferTask) isComplete() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.expectedSize >= 0 && t.bytes == t.expectedSize
}

func (t *uploadTransferTask) complete(stream net.Conn, root *fsroot.FSRoot, file *os.File) error {
	t.mu.Lock()
	bytes := t.bytes
	total := t.expectedSize
	finalRel := t.finalRel
	tempRel := t.tempRel
	path := t.path
	t.mu.Unlock()

	if total >= 0 && bytes != total {
		return t.fail(stream, fmt.Sprintf("size mismatch: expected %d, got %d", total, bytes), 400, fmt.Errorf("size mismatch"))
	}
	code, commitErr := t.commit(root, file, path, tempRel, finalRel)
	if commitErr != nil {
		return t.fail(stream, commitErr.Error(), code, commitErr)
	}

	if _, err := root.Root.Stat(finalRel); err == nil {
		addToIndexerDetached("upload", path)
	}

	t.succeed(stream, FileUploadResult{Path: path, Size: bytes})
	slog.Info("upload complete", "path", path, "size", bytes, "task_id", t.task.ID())
	return nil
}

func (t *uploadTransferTask) commit(root *fsroot.FSRoot, file *os.File, path, tempRel, finalRel string) (int, error) {
	uploadCommitMu.Lock()
	defer uploadCommitMu.Unlock()

	if err := t.verifyExpectedVersion(root, path); err != nil {
		if errors.Is(err, errUploadVersionConflict) {
			return 409, err
		}
		return 500, err
	}
	t.mu.Lock()
	attrs, fileOwner, folderInfo := t.attrs, t.fileOwner, t.folderInfo
	t.mu.Unlock()
	// Open the folder before the buffer is handed over, so a swapped folder
	// leaves it the bridge's.
	dir, err := openUploadFolder(root, filepath.Dir(tempRel), folderInfo)
	if err != nil {
		return 500, fmt.Errorf("finalize upload: %w", err)
	}
	defer dir.Close()
	if err := settleUploadBuffer(file, attrs, fileOwner, newUploadFileMode(t.private, fileOwner.set), isSetgid(folderInfo)); err != nil {
		return 500, err
	}
	if err := file.Sync(); err != nil {
		return 500, fmt.Errorf("sync upload: %w", err)
	}
	if err := file.Close(); err != nil {
		return 500, fmt.Errorf("close upload: %w", err)
	}
	if err := commitUploadBuffer(dir, filepath.Base(tempRel), filepath.Base(finalRel), t.overwrite); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return 409, fmt.Errorf("destination already exists: %w", err)
		}
		return 500, fmt.Errorf("finalize upload: %w", err)
	}
	return 0, nil
}

func (t *uploadTransferTask) progress(phase string) FileProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	return FileProgress{
		Bytes: t.bytes,
		Total: t.expectedSize,
		Pct:   transferPct(t.bytes, t.expectedSize),
		Phase: phase,
	}
}

func (t *uploadTransferTask) writeProgress(stream net.Conn, phase string) {
	progress := t.progress(phase)
	t.sendProgress(stream, progress, progress)
}

func (t *uploadTransferTask) reportProgress(phase string) {
	t.task.ReportProgress(t.progress(phase))
}

func (t *uploadTransferTask) cleanupPartial() {
	t.mu.Lock()
	tempRel, folderInfo := t.tempRel, t.folderInfo
	t.mu.Unlock()
	if tempRel == "" {
		return
	}

	root, err := fsroot.Open()
	if err != nil {
		slog.Debug("failed to open root for partial upload cleanup", "path", t.path, "error", err)
		return
	}
	defer root.Close()
	if err := removeUploadBuffer(root, tempRel, folderInfo); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Debug("failed to remove partial upload", "path", t.path, "partial", tempRel, "error", err)
	}
}
