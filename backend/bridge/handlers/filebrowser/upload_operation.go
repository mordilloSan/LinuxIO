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
	newOwner        sessionOwner // set when a root bridge hands what this upload creates to the session user
	private         bool         // a new file is owner-only (0600)

	// finalRel, tempRel, attrs, folderInfo, bufferInfo and fileOwner are
	// guarded by mu.
	finalRel   string
	tempRel    string
	attrs      uploadAttributes
	folderInfo os.FileInfo  // the folder prepare put the buffer in; the commit must find this folder
	bufferInfo os.FileInfo  // the buffer prepare created; every reopen must find this file
	fileOwner  sessionOwner // newOwner when the buffer's folder is already theirs
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

// loadUploadAttributes reads the owner and mode of the file the commit
// replaces: the entry at realRel itself. The commit's renameat replaces a
// symlink there, not its target, so the target lends no owner or mode (a
// setuid bit included); a link to a folder still refuses like the folder.
func loadUploadAttributes(root *fsroot.FSRoot, realRel string) (uploadAttributes, error) {
	existingStat, err := root.Root.Lstat(realRel)
	if err != nil {
		return uploadAttributes{}, nil
	}
	if existingStat.Mode()&os.ModeSymlink != 0 {
		target, err := root.Root.Stat(realRel)
		if err != nil || !target.IsDir() {
			return uploadAttributes{}, nil
		}
		existingStat = target // only to refuse the folder below
	}
	if existingStat.IsDir() {
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
		private:         req.Private != nil && *req.Private,
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
		if errors.Is(err, errUploadVersionConflict) {
			code = 409
		}
		return t.fail(stream, err.Error(), code, err)
	}

	t.mu.Lock()
	tempRel, bufferInfo := t.tempRel, t.bufferInfo
	t.mu.Unlock()
	file, err := openUploadBuffer(root, tempRel, bufferInfo)
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

func (t *uploadTransferTask) prepare(root *fsroot.FSRoot) error {
	t.mu.Lock()
	if t.tempRel != "" {
		t.mu.Unlock()
		return nil
	}
	t.mu.Unlock()

	realPath := filepath.Clean(t.path)
	// Write through a symlink so the atomic rename replaces the target, not the
	// link (dotfile repos, sites-enabled). A dangling link is replaced as-is.
	if resolvedPath, _, err := iteminfo.ResolveSymlinksAt(t.ctx, root, realPath); err == nil {
		realPath = resolvedPath
	} else if ctxErr := t.ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	realRel := fsroot.ToRel(realPath)
	attrs, err := loadUploadAttributes(root, realRel)
	if err != nil {
		return err
	}
	versionErr := t.verifyExpectedVersion(root, realPath)
	if versionErr != nil {
		return versionErr
	}
	dir, err := openUploadDir(root, filepath.Dir(realRel), t.newOwner)
	if err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	defer dir.Close()
	folderInfo, err := dir.Stat()
	if err != nil {
		return fmt.Errorf("stat parent dir: %w", err)
	}

	partName := "." + filepath.Base(realRel) + ".linuxio-upload-" + t.task.ID() + ".part"
	bufferInfo, err := createUploadBuffer(dir, partName)
	if err != nil {
		return fmt.Errorf("create upload buffer: %w", err)
	}

	t.mu.Lock()
	t.finalRel = realRel
	t.tempRel = filepath.Join(filepath.Dir(realRel), partName)
	t.attrs = attrs
	t.folderInfo = folderInfo
	t.bufferInfo = bufferInfo
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
	tempRel := t.tempRel
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
	if err := root.Root.Remove(tempRel); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Debug("failed to remove partial upload", "path", t.path, "partial", tempRel, "error", err)
	}
}
