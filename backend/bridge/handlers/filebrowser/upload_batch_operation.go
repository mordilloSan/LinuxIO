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
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
)

// uploadBatchFile is one manifest entry of a batch upload: where the file
// lands and how many of the stream's bytes belong to it.
type uploadBatchFile struct {
	relPath  string // manifest path, relative to the batch destination
	absPath  string // cleaned absolute final path
	size     int64
	modTime  time.Time // zero when the manifest carried no modified time
	keepBoth bool      // land under a unique name when absPath already exists
}

// uploadBatchTransferTask receives many files over one data stream. The wire
// format needs no in-band framing: the manifest fixes the order and byte count
// of every file, so the client streams raw file bytes back-to-back and the
// bridge splits them on the manifest boundaries. Resume works like the
// single-file upload — the global byte offset identifies both the current file
// and the position inside it. Items are best-effort like the other batch tasks:
// a file that cannot be written is recorded in failures and its remaining
// stream bytes are discarded so the rest of the batch still lands.
type uploadBatchTransferTask struct {
	*transferLifecycle
	destination   string
	files         []uploadBatchFile
	directories   []string
	total         int64
	overwrite     bool
	newOwner      sessionOwner   // set when a root bridge hands what this upload creates to the session user
	linkRule      services.Owner // the session user a root bridge acts for: no link they could plant is followed (see actingFor)
	private       bool           // new files are owner-only (0600)
	refuseSymlink bool           // the request set owner: a symlink at a file's target is refused, not replaced

	// The fields below are guarded by mu. bytes (stream bytes consumed across
	// the whole batch) lives in the lifecycle.
	prepared   bool  // manifest directories created
	index      int   // current manifest file
	fileBytes  int64 // stream bytes consumed for the current file
	tempRel    string
	finalRel   string
	attrs      uploadAttributes
	folderInfo os.FileInfo  // the folder the current buffer was created in; the commit must find this folder
	buffer     *os.File     // the current file's buffer, held open until its commit or its cleanup closes it; only one is open at a time
	fileOwner  sessionOwner // newOwner when the current buffer's folder is already theirs
	curFailed  bool         // current file failed; its remaining bytes are discarded
	ended      bool         // the cleanup ran: the batch failed or was cancelled, and no buffer may be made
	succeeded  int
	failures   []FileBatchItemFailure
}

// cleanManifestPath validates a client-supplied path relative to the batch
// destination and rejects anything that would escape it.
func cleanManifestPath(raw string) (string, error) {
	cleaned := filepath.Clean(strings.Trim(raw, "/"))
	if cleaned == "" || cleaned == "." || !filepath.IsLocal(cleaned) {
		return "", fmt.Errorf("invalid manifest path: %q", raw)
	}
	return cleaned, nil
}

func parseUploadBatchRequest(req apischema.FileUploadBatchRequest) (string, []uploadBatchFile, []string, int64, error) {
	if strings.TrimSpace(req.Destination) == "" {
		return "", nil, nil, 0, fmt.Errorf("missing destination")
	}
	if len(req.Files) == 0 && len(req.Directories) == 0 {
		return "", nil, nil, 0, fmt.Errorf("empty manifest")
	}
	destination := fsroot.CleanAbsPath(req.Destination)

	keepBoth := make(map[string]bool, len(req.KeepBoth))
	for _, raw := range req.KeepBoth {
		rel, err := cleanManifestPath(raw)
		if err != nil {
			return "", nil, nil, 0, err
		}
		keepBoth[rel] = true
	}

	files := make([]uploadBatchFile, 0, len(req.Files))
	var total int64
	for _, entry := range req.Files {
		rel, err := cleanManifestPath(entry.Path)
		if err != nil {
			return "", nil, nil, 0, err
		}
		size, err := strconv.ParseInt(entry.Size, 10, 64)
		if err != nil || size < 0 {
			return "", nil, nil, 0, fmt.Errorf("invalid size for %q", entry.Path)
		}
		var modTime time.Time
		if entry.Modified != nil {
			modTime, err = time.Parse(time.RFC3339Nano, *entry.Modified)
			if err != nil {
				return "", nil, nil, 0, fmt.Errorf("invalid modified time for %q", entry.Path)
			}
		}
		files = append(files, uploadBatchFile{
			relPath:  rel,
			absPath:  filepath.Join(destination, rel),
			size:     size,
			modTime:  modTime,
			keepBoth: keepBoth[rel],
		})
		total += size
	}

	directories := make([]string, 0, len(req.Directories))
	for _, dir := range req.Directories {
		rel, err := cleanManifestPath(dir)
		if err != nil {
			return "", nil, nil, 0, err
		}
		directories = append(directories, rel)
	}
	return destination, files, directories, total, nil
}

func runUploadBatchTask(ctx context.Context, task *bridgeipc.Task, req apischema.FileUploadBatchRequest, session sessionOwner) (any, error) {
	destination, files, directories, total, err := parseUploadBatchRequest(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", bridgeipc.ErrInvalidArgs, err)
	}
	newOwner, err := uploadNewOwner(req.Owner, session, os.Geteuid())
	if err != nil {
		return nil, err
	}

	root, err := fsroot.Open()
	if err != nil {
		return nil, fmt.Errorf("open filesystem root: %w", err)
	}
	destination, destErr := resolveBatchDestinationDir(root, destination)
	closeErr := root.Close()
	if destErr != nil {
		return nil, destErr
	}
	if closeErr != nil {
		slog.Debug("failed to close filesystem root", "error", closeErr)
	}
	if err := services.EnsureFreeSpace(destination, total); err != nil {
		return nil, bridgeipc.NewError(err.Error(), 507)
	}

	transfer := &uploadBatchTransferTask{
		destination:   destination,
		files:         files,
		directories:   directories,
		total:         total,
		overwrite:     req.Overwrite != nil && *req.Overwrite,
		newOwner:      newOwner,
		linkRule:      actingFor(session),
		private:       req.Private != nil && *req.Private,
		refuseSymlink: req.Owner != "",
	}
	transfer.transferLifecycle = newTransferLifecycle(task, transfer.cleanupPartial, transfer.reportProgress)
	defer registerTransfer(task.ID(), transfer)()

	transfer.reportProgress("waiting_for_client")
	return transfer.await(ctx)
}

func (t *uploadBatchTransferTask) attach(stream net.Conn, req bridgeipc.TaskDataAttachRequest) error {
	_, ok, err := t.attachStream(stream, req, t.total)
	if !ok {
		return err
	}
	defer t.endAttach(stream)

	root, err := fsroot.Open()
	if err != nil {
		return t.fail(stream, "failed to access filesystem", 500, fmt.Errorf("failed to access filesystem: %w", err))
	}
	defer root.Close()

	t.prepareDirectories(root)
	t.writeProgress(stream, "uploading")
	gate := newTransferGate(uploadProgressAckIntervalBytes)
	return t.receiveFrames(stream,
		func(payload []byte) error { return t.consume(stream, root, payload, gate) },
		func() error {
			t.settle(root)
			if t.isComplete() {
				return t.complete(stream)
			}
			t.markWaiting()
			return nil
		})
}

// prepareDirectories creates the manifest's explicit directories once per task,
// handing the new ones over as openUploadDir does. Failures are per-item: they
// are recorded and the batch continues.
func (t *uploadBatchTransferTask) prepareDirectories(root *fsroot.FSRoot) {
	t.mu.Lock()
	if t.prepared || t.ended {
		t.mu.Unlock()
		return
	}
	t.prepared = true
	t.mu.Unlock()

	for _, rel := range t.directories {
		absPath := filepath.Join(t.destination, rel)
		dir, err := openUploadDir(root, fsroot.ToRel(absPath), t.linkRule, t.newOwner)
		if err != nil {
			slog.Debug("batch upload directory failed", "path", absPath, "error", err)
			t.recordFailure(rel, fmt.Sprintf("create directory: %v", err))
			continue
		}
		_ = dir.Close()
		t.mu.Lock()
		t.succeeded++
		t.mu.Unlock()
	}
}

// consume splits one stream payload on manifest boundaries: it may finish the
// current file and start the next several times within a single frame.
func (t *uploadBatchTransferTask) consume(stream net.Conn, root *fsroot.FSRoot, payload []byte, gate *progressThrottle) error {
	for len(payload) > 0 {
		t.settle(root)

		t.mu.Lock()
		if t.index >= len(t.files) {
			t.mu.Unlock()
			return t.fail(stream, fmt.Sprintf("size mismatch: expected %d bytes total, got more", t.total), 400, fmt.Errorf("size mismatch"))
		}
		remaining := t.files[t.index].size - t.fileBytes
		t.mu.Unlock()

		n := min(int64(len(payload)), remaining)
		if err := t.writeChunk(root, payload[:n]); err != nil {
			t.failCurrent(root, err)
		}

		t.mu.Lock()
		t.fileBytes += n
		t.bytes += n
		t.mu.Unlock()
		payload = payload[n:]
	}
	t.settle(root)
	signalActivity(t.activity)

	t.mu.Lock()
	bytes, total := t.bytes, t.total
	t.mu.Unlock()
	if _, _, ok := gate.Set(bytes, total); ok {
		t.writeProgress(stream, "uploading")
	}
	return nil
}

// settle finalizes the current file while it is fully received, advancing to
// the next manifest entry. Zero-size files pass through here without ever
// seeing a data chunk.
func (t *uploadBatchTransferTask) settle(root *fsroot.FSRoot) {
	for {
		t.mu.Lock()
		if t.index >= len(t.files) || t.fileBytes < t.files[t.index].size {
			t.mu.Unlock()
			return
		}
		current := t.files[t.index]
		failed := t.curFailed
		t.mu.Unlock()

		if !failed {
			if err := t.finalizeCurrent(root); err != nil {
				slog.Debug("batch upload item failed", "path", current.absPath, "error", err)
				t.recordFailure(current.relPath, err.Error())
				t.cleanupCurrentTemp(root)
			} else {
				t.mu.Lock()
				t.succeeded++
				t.mu.Unlock()
			}
		}

		t.mu.Lock()
		t.index++
		t.fileBytes = 0
		t.tempRel = ""
		t.finalRel = ""
		t.attrs = uploadAttributes{}
		t.folderInfo = nil
		t.fileOwner = sessionOwner{}
		t.curFailed = false
		t.mu.Unlock()
	}
}

// ensurePrepared returns the current file's buffer, creating it on first use
// exclusively and through the handle of its folder (see createUploadBuffer),
// and captures existing-file attributes. The task holds the buffer open until
// the file is committed or dropped, so a dropped stream resumes into the same
// open file.
func (t *uploadBatchTransferTask) ensurePrepared(root *fsroot.FSRoot) (*os.File, error) {
	t.mu.Lock()
	if t.ended {
		t.mu.Unlock()
		return nil, errUploadEnded // before any folder or buffer is made
	}
	if t.tempRel != "" {
		buffer := t.buffer
		t.mu.Unlock()
		if buffer == nil {
			return nil, errUploadEnded
		}
		return buffer, nil
	}
	current := t.files[t.index]
	index := t.index
	t.mu.Unlock()

	finalRel := fsroot.ToRel(current.absPath)
	if _, statErr := root.Root.Stat(finalRel); statErr == nil {
		switch {
		case current.keepBoth:
			finalRel = fsroot.ToRel(generateUniquePath(current.absPath, false, root))
		case !t.overwrite:
			return nil, fmt.Errorf("destination already exists")
		}
	}
	if err := refuseFolderTarget(root, finalRel); err != nil {
		return nil, err
	}
	dir, err := openUploadDir(root, filepath.Dir(finalRel), t.linkRule, t.newOwner)
	if err != nil {
		return nil, fmt.Errorf("create parent dir: %w", err)
	}
	defer dir.Close()
	if t.refuseSymlink {
		if err = refuseSymlinkTarget(dir, filepath.Base(finalRel)); err != nil {
			return nil, err
		}
	}
	attrs, err := uploadAttributesAt(dir, filepath.Base(finalRel))
	if err != nil {
		return nil, err
	}
	attrs.modTime = current.modTime
	folderInfo, err := dir.Stat()
	if err != nil {
		return nil, fmt.Errorf("stat parent dir: %w", err)
	}

	partName := fmt.Sprintf(".%s.linuxio-upload-%s-%d.part", filepath.Base(finalRel), t.task.ID(), index)
	buffer, err := createUploadBuffer(dir, partName)
	if err != nil {
		return nil, fmt.Errorf("create upload buffer: %w", err)
	}

	t.mu.Lock()
	if t.ended {
		t.mu.Unlock()
		discardUploadBuffer(dir, partName, buffer)
		return nil, errUploadEnded
	}
	t.finalRel = finalRel
	t.tempRel = filepath.Join(filepath.Dir(finalRel), partName)
	t.attrs = attrs
	t.folderInfo = folderInfo
	t.buffer = buffer
	if handsOverIn(folderInfo, t.newOwner) {
		t.fileOwner = t.newOwner
	}
	t.mu.Unlock()
	return buffer, nil
}

func (t *uploadBatchTransferTask) writeChunk(root *fsroot.FSRoot, chunk []byte) error {
	t.mu.Lock()
	failed := t.curFailed
	t.mu.Unlock()
	if failed {
		return nil // discarding the remainder of a failed item
	}

	buffer, err := t.ensurePrepared(root)
	if err != nil {
		return err
	}
	n, err := buffer.Write(chunk)
	if err != nil {
		return err
	}
	if n != len(chunk) {
		return io.ErrShortWrite
	}
	return nil
}

// failCurrent marks the current file failed, records it, and drops its temp
// buffer; the rest of its stream bytes will be discarded.
func (t *uploadBatchTransferTask) failCurrent(root *fsroot.FSRoot, cause error) {
	t.mu.Lock()
	current := t.files[t.index]
	t.curFailed = true
	t.mu.Unlock()

	slog.Debug("batch upload item failed", "path", current.absPath, "error", cause)
	t.recordFailure(current.relPath, cause.Error())
	t.cleanupCurrentTemp(root)
}

// finalizeCurrent settles the current buffer through its open handle, so a
// name swapped under it is never touched, and commits it in the folder it was
// created in. It takes the buffer from the task and closes it whatever
// happens; the cleanup of a failed item only removes its name.
func (t *uploadBatchTransferTask) finalizeCurrent(root *fsroot.FSRoot) error {
	// Zero-size files never opened a buffer; create the (empty) temp now so
	// every finalize is a rename of a fully-written buffer.
	if _, err := t.ensurePrepared(root); err != nil {
		return err
	}

	t.mu.Lock()
	tempRel, finalRel, attrs := t.tempRel, t.finalRel, t.attrs
	folderInfo, buffer, fileOwner := t.folderInfo, t.buffer, t.fileOwner
	t.buffer = nil
	overwrite := t.overwrite && !t.files[t.index].keepBoth
	t.mu.Unlock()
	if buffer == nil {
		return errUploadEnded
	}
	defer buffer.Close()

	// Open the folder and check the buffer's name before the buffer is handed
	// over, so a swapped folder or name leaves it the bridge's and private.
	dir, err := openUploadFolder(root, filepath.Dir(tempRel), folderInfo)
	if err != nil {
		return fmt.Errorf("finalize upload: %w", err)
	}
	defer dir.Close()
	if err := checkUploadBuffer(dir, filepath.Base(tempRel), buffer); err != nil {
		return fmt.Errorf("finalize upload: %w", err)
	}
	if err := settleUploadBuffer(buffer, attrs, fileOwner, newUploadFileMode(t.private, fileOwner.set), isSetgid(folderInfo)); err != nil {
		return err
	}
	if !attrs.modTime.IsZero() {
		// Through the open file (/proc/self/fd), not the buffer's name.
		tv := unix.NsecToTimeval(attrs.modTime.UnixNano())
		if err := unix.Futimes(int(buffer.Fd()), []unix.Timeval{tv, tv}); err != nil {
			slog.Debug("failed to apply uploaded file modified time", "path", tempRel, "error", err)
		}
	}
	if err := buffer.Sync(); err != nil {
		return fmt.Errorf("sync upload: %w", err)
	}
	// Again right before the commit, which goes by name.
	if err := checkUploadBuffer(dir, filepath.Base(tempRel), buffer); err != nil {
		return fmt.Errorf("finalize upload: %w", err)
	}
	if err := buffer.Close(); err != nil {
		return fmt.Errorf("close upload: %w", err)
	}
	if err := commitUploadBuffer(dir, filepath.Base(tempRel), filepath.Base(finalRel), overwrite); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("destination already exists: %w", err)
		}
		return fmt.Errorf("finalize upload: %w", err)
	}
	return nil
}

func (t *uploadBatchTransferTask) recordFailure(path, message string) {
	t.mu.Lock()
	t.failures = append(t.failures, FileBatchItemFailure{Path: path, Error: message})
	t.mu.Unlock()
}

// cleanupCurrentTemp closes the current file's buffer and removes it.
func (t *uploadBatchTransferTask) cleanupCurrentTemp(root *fsroot.FSRoot) {
	t.mu.Lock()
	tempRel, folderInfo, buffer := t.tempRel, t.folderInfo, t.buffer
	t.tempRel = ""
	t.buffer = nil
	t.mu.Unlock()
	if buffer != nil {
		_ = buffer.Close()
	}
	if tempRel == "" {
		return
	}
	if err := removeUploadBuffer(root, tempRel, folderInfo); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Debug("failed to remove partial batch upload", "partial", tempRel, "error", err)
	}
}

func (t *uploadBatchTransferTask) isComplete() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.bytes == t.total && t.index >= len(t.files)
}

func (t *uploadBatchTransferTask) complete(stream net.Conn) error {
	t.mu.Lock()
	result := FileUploadBatchResult{
		FileBatchResult: batchResult(len(t.directories)+len(t.files), t.succeeded, append([]FileBatchItemFailure(nil), t.failures...)),
		Destination:     t.destination,
		Size:            t.bytes,
	}
	bytes := t.bytes
	failed := len(t.failures)
	succeeded := t.succeeded
	t.mu.Unlock()
	if succeeded > 0 {
		reindexDetached("upload_batch", t.destination)
	}

	t.succeed(stream, result)
	slog.Info("batch upload complete", "destination", t.destination, "files", len(t.files), "directories", len(t.directories), "size", bytes, "failed", failed, "task_id", t.task.ID())
	return nil
}

func (t *uploadBatchTransferTask) progress(phase string) BatchUploadProgress {
	t.mu.Lock()
	defer t.mu.Unlock()
	return BatchUploadProgress{
		Bytes:      t.bytes,
		Total:      t.total,
		Pct:        transferPct(t.bytes, t.total),
		Phase:      phase,
		FilesDone:  t.index,
		FilesTotal: len(t.files),
	}
}

func (t *uploadBatchTransferTask) writeProgress(stream net.Conn, phase string) {
	progress := t.progress(phase)
	t.sendProgress(stream, progress, progress)
}

func (t *uploadBatchTransferTask) reportProgress(phase string) {
	t.task.ReportProgress(t.progress(phase))
}

// cleanupPartial runs once the batch failed or was cancelled: it closes the
// current file's buffer and removes it. Files already finalized stay in
// place — a cancelled batch keeps what it landed, matching the best-effort
// semantics of the other batch tasks.
func (t *uploadBatchTransferTask) cleanupPartial() {
	t.mu.Lock()
	tempRel, folderInfo, buffer := t.tempRel, t.folderInfo, t.buffer
	t.tempRel = ""
	t.buffer = nil
	t.ended = true
	t.mu.Unlock()
	if buffer != nil {
		_ = buffer.Close()
	}
	if tempRel == "" {
		return
	}

	root, err := fsroot.Open()
	if err != nil {
		slog.Debug("failed to open root for partial batch upload cleanup", "error", err)
		return
	}
	defer root.Close()
	if err := removeUploadBuffer(root, tempRel, folderInfo); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Debug("failed to remove partial batch upload", "partial", tempRel, "error", err)
	}
}
