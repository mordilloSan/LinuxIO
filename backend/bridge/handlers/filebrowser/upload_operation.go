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
	owner       uploadOwner // applied to new files when the bridge runs as root
	modTime     time.Time   // zero keeps the write time
}

// uploadOwner is the session user new uploads belong to. A root bridge would
// otherwise leave root-owned files the user cannot touch from an unprivileged
// session; an unprivileged bridge already writes as that user.
type uploadOwner struct {
	uid uint32
	gid uint32
	set bool
}

type uploadTransferTask struct {
	*transferLifecycle
	ctx             context.Context
	path            string
	expectedSize    int64
	expectedVersion string
	owner           uploadOwner
	overwrite       bool

	// finalRel, tempRel and attrs are guarded by mu.
	finalRel string
	tempRel  string
	attrs    uploadAttributes
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

func loadUploadAttributes(root *fsroot.FSRoot, realRel string) (uploadAttributes, error) {
	existingStat, err := root.Root.Stat(realRel)
	if err != nil {
		return uploadAttributes{}, nil
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

// restoreUploadedFile settles ownership, mode and time on the upload buffer
// before it is committed, so the target mode never lags behind the content.
// Ownership goes first because a chown clears setuid and setgid bits.
func restoreUploadedFile(root *fsroot.FSRoot, rel string, attrs uploadAttributes) error {
	if attrs.hasExisting {
		if err := root.Root.Chown(rel, attrs.uid, attrs.gid); err != nil {
			return fmt.Errorf("restore upload ownership: %w", err)
		}
		if err := root.Root.Chmod(rel, attrs.mode); err != nil {
			return fmt.Errorf("restore upload permissions: %w", err)
		}
	} else {
		if attrs.owner.set && os.Geteuid() == 0 {
			if err := root.Root.Chown(rel, int(attrs.owner.uid), int(attrs.owner.gid)); err != nil {
				return fmt.Errorf("hand upload to session user: %w", err)
			}
		}
		if err := root.Root.Chmod(rel, services.PermFile); err != nil {
			return fmt.Errorf("set upload permissions: %w", err)
		}
	}
	if !attrs.modTime.IsZero() {
		if err := root.Root.Chtimes(rel, attrs.modTime, attrs.modTime); err != nil {
			slog.Debug("failed to apply uploaded file modified time", "path", rel, "error", err)
		}
	}
	return nil
}

// commitUploadBuffer moves the finished buffer into place. Without overwrite
// it links instead of renaming, so a file that appeared since the upload
// started is never replaced.
func commitUploadBuffer(root *fsroot.FSRoot, tempRel, finalRel string, overwrite bool) error {
	if overwrite {
		return root.Root.Rename(tempRel, finalRel)
	}
	if err := root.Root.Link(tempRel, finalRel); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return err
		}
		// No hard links here (FAT, SMB, some FUSE): fall back to the old
		// stat-then-rename rather than refusing every upload on that mount.
		if _, statErr := root.Root.Lstat(finalRel); statErr == nil {
			return fs.ErrExist
		}
		return root.Root.Rename(tempRel, finalRel)
	}
	if err := root.Root.Remove(tempRel); err != nil {
		slog.Warn("failed to remove committed upload buffer", "path", tempRel, "error", err)
	}
	return nil
}

func runUploadTask(ctx context.Context, task *bridgeipc.Task, req apischema.FileUploadRequest, owner uploadOwner) (any, error) {
	path, expectedSize, overwrite, expectedVersion, err := parseUploadRequest(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
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
		owner:           owner,
		overwrite:       overwrite,
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

	file, err := root.Root.OpenFile(t.tempRel, os.O_RDWR, services.PermFile)
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
	attrs.owner = t.owner
	versionErr := t.verifyExpectedVersion(root, realPath)
	if versionErr != nil {
		return versionErr
	}
	err = root.Root.MkdirAll(fsroot.ToRel(filepath.Dir(realPath)), services.PermDir)
	if err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	partName := "." + filepath.Base(realRel) + ".linuxio-upload-" + t.task.ID() + ".part"
	tempRel := filepath.Join(filepath.Dir(realRel), partName)
	file, err := root.Root.OpenFile(tempRel, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600) // owner-only until restoreUploadedFile applies the target mode
	if err != nil {
		return fmt.Errorf("create upload buffer: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close upload buffer: %w", err)
	}

	t.mu.Lock()
	t.finalRel = realRel
	t.tempRel = tempRel
	t.attrs = attrs
	t.mu.Unlock()
	return nil
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
	if err := file.Sync(); err != nil {
		return 500, fmt.Errorf("sync upload: %w", err)
	}
	if err := file.Close(); err != nil {
		return 500, fmt.Errorf("close upload: %w", err)
	}
	t.mu.Lock()
	attrs := t.attrs
	t.mu.Unlock()
	if err := restoreUploadedFile(root, tempRel, attrs); err != nil {
		return 500, err
	}
	if err := commitUploadBuffer(root, tempRel, finalRel, t.overwrite); err != nil {
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
