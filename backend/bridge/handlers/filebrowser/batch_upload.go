package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	bridgetasks "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

// uploadBatchFile is one manifest entry of a batch upload: where the file
// lands and how many of the stream's bytes belong to it.
type uploadBatchFile struct {
	relPath string // manifest path, relative to the batch destination
	absPath string // cleaned absolute final path
	size    int64
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
	task        *bridgetasks.Task
	destination string
	files       []uploadBatchFile
	directories []string
	total       int64
	overwrite   bool
	done        chan transferOutcome
	activity    chan struct{}
	finishOnce  sync.Once

	mu           sync.Mutex
	bytes        int64 // stream bytes consumed across the whole batch
	attached     bool
	active       net.Conn
	prepared     bool  // manifest directories created
	index        int   // current manifest file
	fileBytes    int64 // stream bytes consumed for the current file
	tempRel      string
	finalRel     string
	attrs        uploadAttributes
	curFailed    bool // current file failed; its remaining bytes are discarded
	succeeded    int
	failures     []FileBatchItemFailure
	indexedPaths []string
}

// uploadBatchSession holds per-attach state: the filesystem root and the open
// handle of the current file's temp buffer. Only one stream may be attached at
// a time (guarded by beginAttach), so the session is never shared.
type uploadBatchSession struct {
	root *fsroot.FSRoot
	file *os.File
}

func (s *uploadBatchSession) closeFile() {
	if s.file != nil {
		_ = s.file.Close()
		s.file = nil
	}
}

func (s *uploadBatchSession) syncAndCloseFile() error {
	if s.file == nil {
		return nil
	}
	file := s.file
	s.file = nil
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync upload: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close upload: %w", err)
	}
	return nil
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
	destination := utils.CleanAbsPath(req.Destination)

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
		files = append(files, uploadBatchFile{
			relPath: rel,
			absPath: filepath.Join(destination, rel),
			size:    size,
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

func runUploadBatchTask(ctx context.Context, task *bridgetasks.Task, req apischema.FileUploadBatchRequest) (any, error) {
	destination, files, directories, total, err := parseUploadBatchRequest(req)
	if err != nil {
		return nil, bridgetasks.NewError(err.Error(), 400)
	}

	root, err := fsroot.Open()
	if err != nil {
		return nil, bridgetasks.NewError("failed to access filesystem", 500)
	}
	destination, destErr := resolveBatchDestinationDir(root, destination)
	closeErr := root.Close()
	if destErr != nil {
		return nil, destErr
	}
	if closeErr != nil {
		slog.Debug("failed to close filesystem root", "error", closeErr)
	}

	transfer := &uploadBatchTransferTask{
		task:        task,
		destination: destination,
		files:       files,
		directories: directories,
		total:       total,
		overwrite:   req.Overwrite != nil && *req.Overwrite,
		done:        make(chan transferOutcome, 1),
		activity:    make(chan struct{}, 1),
	}
	fileTransferTasks.Store(task.ID(), transfer)
	defer fileTransferTasks.Delete(task.ID())

	transfer.reportProgress("waiting_for_client")
	return awaitTransferOutcome(ctx, transfer.done, transfer.activity, transfer.cancel)
}

func (t *uploadBatchTransferTask) attach(stream net.Conn, req bridgetasks.TaskDataAttachRequest) error {
	offset, err := parseTransferOffset(req)
	if err != nil {
		return ipc.WriteResultErrorAndClose(stream, 0, err.Error(), 400)
	}
	err = t.beginAttach(stream, offset)
	if err != nil {
		return ipc.WriteResultErrorAndClose(stream, 0, err.Error(), 409)
	}
	defer t.endAttach(stream)

	root, err := fsroot.Open()
	if err != nil {
		return t.fail(stream, "failed to access filesystem", 500, fmt.Errorf("failed to access filesystem: %w", err))
	}
	defer root.Close()

	session := &uploadBatchSession{root: root}
	defer session.closeFile()

	t.prepareDirectories(root)
	t.writeProgress(stream, "uploading")
	return t.receive(stream, session, newTransferProgressGate(uploadProgressAckIntervalBytes))
}

func (t *uploadBatchTransferTask) beginAttach(stream net.Conn, offset int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.attached {
		return fmt.Errorf("transfer already has an attached data stream")
	}
	if offset != t.bytes {
		return fmt.Errorf("offset mismatch: expected %d, got %d", t.bytes, offset)
	}
	if offset > t.total {
		return fmt.Errorf("offset exceeds transfer size")
	}
	t.attached = true
	t.active = stream
	signalActivity(t.activity)
	return nil
}

func (t *uploadBatchTransferTask) endAttach(stream net.Conn) {
	t.mu.Lock()
	if t.active == stream {
		t.attached = false
		t.active = nil
	}
	t.mu.Unlock()
}

// prepareDirectories creates the manifest's explicit directories once per task.
// Failures are per-item: they are recorded and the batch continues.
func (t *uploadBatchTransferTask) prepareDirectories(root *fsroot.FSRoot) {
	t.mu.Lock()
	if t.prepared {
		t.mu.Unlock()
		return
	}
	t.prepared = true
	t.mu.Unlock()

	for _, rel := range t.directories {
		absPath := filepath.Join(t.destination, rel)
		if err := root.Root.MkdirAll(fsroot.ToRel(absPath), services.PermDir); err != nil {
			slog.Debug("batch upload directory failed", "path", absPath, "error", err)
			t.recordFailure(rel, fmt.Sprintf("create directory: %v", err))
			continue
		}
		t.mu.Lock()
		t.succeeded++
		t.mu.Unlock()

		if _, err := root.Root.Stat(fsroot.ToRel(absPath)); err == nil {
			t.mu.Lock()
			t.indexedPaths = append(t.indexedPaths, absPath)
			t.mu.Unlock()
		}
	}
}

func (t *uploadBatchTransferTask) receive(stream net.Conn, session *uploadBatchSession, progressGate *transferProgressGate) error {
	for {
		frame, err := ipc.ReadRelayFrame(stream)
		if err != nil {
			t.markWaiting()
			return nil
		}

		switch frame.Opcode {
		case ipc.OpStreamData:
			if len(frame.Payload) == 0 {
				continue
			}
			if err := t.consume(stream, session, frame.Payload, progressGate); err != nil {
				return err
			}
		case ipc.OpStreamClose:
			t.settle(session)
			if t.isComplete() {
				return t.complete(stream)
			}
			t.markWaiting()
			return nil
		case ipc.OpStreamAbort:
			t.cancel()
			return ipc.ErrAborted
		default:
			slog.Debug("ignoring file transfer stream opcode", "opcode", fmt.Sprintf("0x%02x", frame.Opcode))
		}
	}
}

// consume splits one stream payload on manifest boundaries: it may finish the
// current file and start the next several times within a single frame.
func (t *uploadBatchTransferTask) consume(stream net.Conn, session *uploadBatchSession, payload []byte, progressGate *transferProgressGate) error {
	for len(payload) > 0 {
		t.settle(session)

		t.mu.Lock()
		if t.index >= len(t.files) {
			t.mu.Unlock()
			return t.fail(stream, fmt.Sprintf("size mismatch: expected %d bytes total, got more", t.total), 400, fmt.Errorf("size mismatch"))
		}
		remaining := t.files[t.index].size - t.fileBytes
		t.mu.Unlock()

		n := min(int64(len(payload)), remaining)
		if err := t.writeChunk(session, payload[:n]); err != nil {
			t.failCurrent(session, err)
		}

		t.mu.Lock()
		t.fileBytes += n
		t.bytes += n
		t.mu.Unlock()
		payload = payload[n:]
	}
	t.settle(session)
	signalActivity(t.activity)

	t.mu.Lock()
	bytes, total := t.bytes, t.total
	t.mu.Unlock()
	if progressGate.ShouldReport(bytes, total) {
		t.writeProgress(stream, "uploading")
	}
	return nil
}

// settle finalizes the current file while it is fully received, advancing to
// the next manifest entry. Zero-size files pass through here without ever
// seeing a data chunk.
func (t *uploadBatchTransferTask) settle(session *uploadBatchSession) {
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
			if err := t.finalizeCurrent(session, current); err != nil {
				slog.Debug("batch upload item failed", "path", current.absPath, "error", err)
				t.recordFailure(current.relPath, err.Error())
				t.cleanupCurrentTemp(session.root)
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
		t.curFailed = false
		t.mu.Unlock()
	}
}

// ensurePrepared lazily creates the current file's temp buffer and captures
// existing-file attributes. Idempotent across re-attaches: the temp survives a
// dropped stream and is reused on resume.
func (t *uploadBatchTransferTask) ensurePrepared(session *uploadBatchSession) error {
	t.mu.Lock()
	if t.tempRel != "" {
		t.mu.Unlock()
		return nil
	}
	current := t.files[t.index]
	index := t.index
	t.mu.Unlock()

	finalRel := fsroot.ToRel(current.absPath)
	if !t.overwrite {
		if _, statErr := session.root.Root.Stat(finalRel); statErr == nil {
			return fmt.Errorf("destination already exists")
		}
	}
	attrs, err := loadUploadAttributes(session.root, finalRel)
	if err != nil {
		return err
	}
	err = session.root.Root.MkdirAll(fsroot.ToRel(filepath.Dir(current.absPath)), services.PermDir)
	if err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	partName := fmt.Sprintf(".%s.linuxio-upload-%s-%d.part", filepath.Base(finalRel), t.task.ID(), index)
	tempRel := filepath.Join(filepath.Dir(finalRel), partName)
	file, err := session.root.Root.OpenFile(tempRel, os.O_RDWR|os.O_CREATE|os.O_TRUNC, services.PermFile)
	if err != nil {
		return fmt.Errorf("create upload buffer: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close upload buffer: %w", err)
	}

	t.mu.Lock()
	t.finalRel = finalRel
	t.tempRel = tempRel
	t.attrs = attrs
	t.mu.Unlock()
	return nil
}

func (t *uploadBatchTransferTask) writeChunk(session *uploadBatchSession, chunk []byte) error {
	t.mu.Lock()
	failed := t.curFailed
	t.mu.Unlock()
	if failed {
		return nil // discarding the remainder of a failed item
	}

	if session.file == nil {
		if err := t.ensurePrepared(session); err != nil {
			return err
		}
		t.mu.Lock()
		tempRel, at := t.tempRel, t.fileBytes
		t.mu.Unlock()
		file, err := session.root.Root.OpenFile(tempRel, os.O_RDWR, services.PermFile)
		if err != nil {
			return fmt.Errorf("cannot open upload buffer: %w", err)
		}
		if _, err := file.Seek(at, io.SeekStart); err != nil {
			_ = file.Close()
			return fmt.Errorf("cannot position upload buffer: %w", err)
		}
		session.file = file
	}

	n, err := session.file.Write(chunk)
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
func (t *uploadBatchTransferTask) failCurrent(session *uploadBatchSession, cause error) {
	session.closeFile()

	t.mu.Lock()
	current := t.files[t.index]
	t.curFailed = true
	t.mu.Unlock()

	slog.Debug("batch upload item failed", "path", current.absPath, "error", cause)
	t.recordFailure(current.relPath, cause.Error())
	t.cleanupCurrentTemp(session.root)
}

func (t *uploadBatchTransferTask) finalizeCurrent(session *uploadBatchSession, current uploadBatchFile) error {
	// Zero-size files never opened a buffer; create the (empty) temp now so
	// every finalize is a rename of a fully-written buffer.
	if err := t.ensurePrepared(session); err != nil {
		return err
	}
	if err := session.syncAndCloseFile(); err != nil {
		return err
	}

	t.mu.Lock()
	tempRel, finalRel, attrs := t.tempRel, t.finalRel, t.attrs
	t.mu.Unlock()

	if err := session.root.Root.Rename(tempRel, finalRel); err != nil {
		return fmt.Errorf("finalize upload: %w", err)
	}
	restoreUploadedFile(session.root, finalRel, attrs)
	if _, err := session.root.Root.Stat(finalRel); err == nil {
		t.mu.Lock()
		t.indexedPaths = append(t.indexedPaths, current.absPath)
		t.mu.Unlock()
	}
	return nil
}

func (t *uploadBatchTransferTask) recordFailure(path, message string) {
	t.mu.Lock()
	t.failures = append(t.failures, FileBatchItemFailure{Path: path, Error: message})
	t.mu.Unlock()
}

func (t *uploadBatchTransferTask) cleanupCurrentTemp(root *fsroot.FSRoot) {
	t.mu.Lock()
	tempRel := t.tempRel
	t.tempRel = ""
	t.mu.Unlock()
	if tempRel == "" {
		return
	}
	if err := root.Root.Remove(tempRel); err != nil && !errors.Is(err, os.ErrNotExist) {
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
	indexedPaths := append([]string(nil), t.indexedPaths...)
	t.mu.Unlock()
	if len(indexedPaths) > 0 {
		runDetachedIndexerUpdate("upload_batch", func(ctx context.Context) error {
			for _, path := range indexedPaths {
				if err := addToIndexer(ctx, path); err != nil {
					slog.Debug("batch upload indexer reconciliation failed", "path", path, "error", err)
				}
			}
			return nil
		})
	}

	t.reportProgress("completed")
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, result))
	t.finish(result, nil)
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
	t.task.ReportProgress(progress)
	logWriteErr("progress", ipc.WriteProgress(stream, 0, progress))
}

func (t *uploadBatchTransferTask) reportProgress(phase string) {
	t.task.ReportProgress(t.progress(phase))
}

func (t *uploadBatchTransferTask) markWaiting() {
	t.reportProgress("waiting_for_client")
}

func (t *uploadBatchTransferTask) fail(stream net.Conn, message string, code int, err error) error {
	t.cleanupPartial()
	taskErr := bridgetasks.NewError(message, code)
	if stream != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, message, code))
	}
	t.finish(nil, taskErr)
	if err != nil {
		return err
	}
	return taskErr
}

func (t *uploadBatchTransferTask) cancel() {
	t.mu.Lock()
	active := t.active
	t.mu.Unlock()
	if active != nil {
		_ = active.Close()
	}
	t.cleanupPartial()
	t.finish(nil, context.Canceled)
}

// cleanupPartial removes the current file's temp buffer. Files already
// finalized stay in place — a cancelled batch keeps what it landed, matching
// the best-effort semantics of the other batch tasks.
func (t *uploadBatchTransferTask) cleanupPartial() {
	t.mu.Lock()
	tempRel := t.tempRel
	t.tempRel = ""
	t.mu.Unlock()
	if tempRel == "" {
		return
	}

	root, err := fsroot.Open()
	if err != nil {
		slog.Debug("failed to open root for partial batch upload cleanup", "error", err)
		return
	}
	defer root.Close()
	if err := root.Root.Remove(tempRel); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Debug("failed to remove partial batch upload", "partial", tempRel, "error", err)
	}
}

func (t *uploadBatchTransferTask) finish(result any, err error) {
	t.finishOnce.Do(func() {
		t.done <- transferOutcome{result: result, err: err}
	})
}
