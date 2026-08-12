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
	"sync"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/config"
	bridgetasks "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

type transferOutcome struct {
	result any
	err    error
}

type uploadAttributes struct {
	mode        os.FileMode
	uid         int
	gid         int
	hasExisting bool
}

type uploadTransferTask struct {
	task         *bridgetasks.Task
	path         string
	expectedSize int64
	done         chan transferOutcome
	activity     chan struct{}
	finishOnce   sync.Once

	mu       sync.Mutex
	bytes    int64
	attached bool
	active   net.Conn
	finalRel string
	tempRel  string
	attrs    uploadAttributes
}

type downloadTransferTask struct {
	task     *bridgetasks.Task
	path     string
	realRel  string
	fileName string
	total    int64
	done     chan transferOutcome
	activity chan struct{}

	finishOnce sync.Once
	mu         sync.Mutex
	bytes      int64
	attached   bool
	active     net.Conn
}

type archiveTransferTask struct {
	task        *bridgetasks.Task
	format      string
	paths       []string
	archive     string
	archiveName string
	total       int64
	done        chan transferOutcome
	activity    chan struct{}
	ready       chan struct{}

	finishOnce  sync.Once
	readyOnce   sync.Once
	mu          sync.Mutex
	bytes       int64
	archiveSize int64
	attached    bool
	active      net.Conn
	readyErr    error
}

var fileTransferTasks sync.Map

// transferIdleTimeout bounds how long a transfer task may sit with no client
// progress before it is abandoned. Uploads/downloads/archives park in
// waiting_for_client on a stream error (so the client can resume) instead of
// failing; without this backstop a client that never reconnects — tab closed,
// crash, network loss — would hold a limited transfer task slot indefinitely.
const transferIdleTimeout = 5 * time.Minute

// signalActivity records forward progress on a transfer without blocking. The
// activity channel is buffered (size 1) and coalesces: a full buffer already
// means "progress happened since the last reset", so a dropped signal is fine.
func signalActivity(activity chan struct{}) {
	if activity == nil {
		return
	}
	select {
	case activity <- struct{}{}:
	default:
	}
}

// awaitTransferOutcome blocks until the transfer finishes, its context is
// cancelled, or the client is idle past transferIdleTimeout. Every activity
// signal resets the idle deadline, so transfers making progress are never
// interrupted; only genuinely abandoned ones are cancelled (freeing the slot).
func awaitTransferOutcome(ctx context.Context, done <-chan transferOutcome, activity <-chan struct{}, cancel func()) (any, error) {
	timer := time.NewTimer(transferIdleTimeout)
	defer timer.Stop()
	for {
		select {
		case outcome := <-done:
			return outcome.result, outcome.err
		case <-ctx.Done():
			cancel()
			return nil, context.Canceled
		case <-activity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(transferIdleTimeout)
		case <-timer.C:
			slog.Warn("transfer abandoned by client; cancelling task", "idle_timeout", transferIdleTimeout)
			cancel()
			return nil, bridgetasks.NewError("transfer abandoned: client idle", 408)
		}
	}
}

func parseUploadRequest(req apischema.FileUploadRequest) (string, int64, bool, error) {
	if req.TargetPath == "" || req.Size == "" {
		return "", 0, false, fmt.Errorf("missing path or size")
	}

	expectedSize, err := strconv.ParseInt(req.Size, 10, 64)
	if err != nil {
		return "", 0, false, fmt.Errorf("invalid size: %w", err)
	}

	return req.TargetPath, expectedSize, req.Overwrite != nil && *req.Overwrite, nil
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

func restoreUploadedFile(root *fsroot.FSRoot, realRel string, attrs uploadAttributes) {
	if attrs.hasExisting {
		if err := root.Root.Chmod(realRel, attrs.mode); err != nil {
			slog.Debug("failed to restore uploaded file permissions", "path", realRel, "error", err)
		}
		if err := root.Root.Chown(realRel, attrs.uid, attrs.gid); err != nil {
			slog.Debug("failed to restore uploaded file ownership", "path", realRel, "error", err)
		}
		return
	}
	if err := root.Root.Chmod(realRel, services.PermFile); err != nil {
		slog.Debug("failed to set uploaded file permissions", "path", realRel, "error", err)
	}
}

func notifyUploadedFile(path string, info os.FileInfo) {
	runDetachedIndexerUpdate("upload", func(ctx context.Context) error {
		return addToIndexer(ctx, path, info)
	})
}

func runUploadTask(ctx context.Context, task *bridgetasks.Task, req apischema.FileUploadRequest) (any, error) {
	path, expectedSize, overwrite, err := parseUploadRequest(req)
	if err != nil {
		return nil, bridgetasks.NewError(err.Error(), 400)
	}

	// Uploads never overwrite unless explicitly told to: fail the conflict
	// before the client streams any bytes.
	if !overwrite {
		root, rootErr := fsroot.Open()
		if rootErr != nil {
			return nil, bridgetasks.NewError("failed to access filesystem", 500)
		}
		_, statErr := root.Root.Stat(fsroot.ToRel(filepath.Clean(path)))
		if closeErr := root.Close(); closeErr != nil {
			slog.Debug("failed to close filesystem root", "error", closeErr)
		}
		if statErr == nil {
			return nil, bridgetasks.NewError("destination already exists", 409)
		}
	}

	transfer := &uploadTransferTask{
		task:         task,
		path:         filepath.Clean(path),
		expectedSize: expectedSize,
		done:         make(chan transferOutcome, 1),
		activity:     make(chan struct{}, 1),
	}
	fileTransferTasks.Store(task.ID(), transfer)
	defer fileTransferTasks.Delete(task.ID())

	transfer.reportProgress("waiting_for_client")
	return awaitTransferOutcome(ctx, transfer.done, transfer.activity, transfer.cancel)
}

func runDownloadTask(ctx context.Context, task *bridgetasks.Task, req apischema.PathRequest) (any, error) {
	if req.Path == "" {
		return nil, bridgetasks.NewError("missing file path", 400)
	}

	path := filepath.Clean(req.Path)
	root, err := fsroot.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to access filesystem: %w", err)
	}
	defer root.Close()

	realRel := fsroot.ToRel(path)
	stat, err := root.Root.Stat(realRel)
	if err != nil {
		return nil, bridgetasks.NewError(fmt.Sprintf("file not found: %v", err), 404)
	}
	if stat.IsDir() {
		return nil, bridgetasks.NewError("path is a directory, use archive download instead", 400)
	}

	transfer := &downloadTransferTask{
		task:     task,
		path:     path,
		realRel:  realRel,
		fileName: filepath.Base(path),
		total:    stat.Size(),
		done:     make(chan transferOutcome, 1),
		activity: make(chan struct{}, 1),
	}
	fileTransferTasks.Store(task.ID(), transfer)
	defer fileTransferTasks.Delete(task.ID())

	transfer.reportProgress("waiting_for_client")
	return awaitTransferOutcome(ctx, transfer.done, transfer.activity, transfer.cancel)
}

func runArchiveTask(ctx context.Context, task *bridgetasks.Task, store *config.UserStore, req apischema.FileArchiveRequest) (any, error) {
	if req.Format == "" || len(req.Paths) == 0 {
		return nil, bridgetasks.NewError("missing format or paths", 400)
	}

	paths := append([]string(nil), req.Paths...)
	extension, err := archiveExtension(req.Format)
	if err != nil {
		return nil, bridgetasks.NewError(fmt.Sprintf("unsupported format: %s", req.Format), 400)
	}
	settings := taskSettingsForTask(ctx, task, store)
	release, err := heavyArchiveLimiter.acquire(ctx, settings.HeavyArchiveConcurrency)
	if err != nil {
		return nil, context.Canceled
	}
	defer release()

	transfer := &archiveTransferTask{
		task:        task,
		format:      req.Format,
		paths:       paths,
		archiveName: archiveNameForPaths(paths, extension),
		total:       computeArchiveSize(paths),
		done:        make(chan transferOutcome, 1),
		activity:    make(chan struct{}, 1),
		ready:       make(chan struct{}),
	}
	fileTransferTasks.Store(task.ID(), transfer)
	defer fileTransferTasks.Delete(task.ID())
	defer transfer.cleanupArchive()

	transfer.reportProgress("preparing")
	tempFile, err := os.CreateTemp("", "linuxio-task-archive-*"+extension)
	if err != nil {
		return nil, fmt.Errorf("create temp archive: %w", err)
	}
	tempPath := tempFile.Name()
	err = tempFile.Close()
	if err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("close temp archive: %w", err)
	}

	transfer.mu.Lock()
	transfer.archive = tempPath
	transfer.mu.Unlock()

	callbacks := newArchiveTaskCallbacks(ctx, transfer, store)
	err = createArchive(req.Format, tempPath, callbacks, archiveCompressionWorkers(settings), paths)
	if err != nil {
		transfer.setReadyError(err)
		if errors.Is(err, context.Canceled) || errors.Is(err, ipc.ErrAborted) {
			return nil, context.Canceled
		}
		return nil, fmt.Errorf("create archive: %w", err)
	}

	stat, err := os.Stat(tempPath)
	if err != nil {
		transfer.setReadyError(err)
		return nil, fmt.Errorf("stat archive: %w", err)
	}
	transfer.setReady(stat.Size())
	transfer.reportProgress("waiting_for_client")

	return awaitTransferOutcome(ctx, transfer.done, transfer.activity, transfer.cancel)
}

func attachFileTransferData(ctx context.Context, task *bridgetasks.Task, stream net.Conn, request any) error {
	req, ok := request.(bridgetasks.TaskDataAttachRequest)
	if !ok {
		return ipc.WriteResultErrorAndClose(stream, 0, "invalid transfer request", 400)
	}
	transfer, ok := waitForFileTransferTask(ctx, task.ID())
	if !ok {
		return ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("transfer task not ready: %s", task.ID()), 404)
	}

	switch active := transfer.(type) {
	case *uploadTransferTask:
		return active.attach(stream, req)
	case *uploadBatchTransferTask:
		return active.attach(stream, req)
	case *downloadTransferTask:
		return active.attach(stream, req)
	case *archiveTransferTask:
		return active.attach(stream, req)
	default:
		return ipc.WriteResultErrorAndClose(stream, 0, "unsupported transfer task", 400)
	}
}

func waitForFileTransferTask(ctx context.Context, taskID string) (any, bool) {
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		if transfer, ok := fileTransferTasks.Load(taskID); ok {
			return transfer, true
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-deadline.C:
			return nil, false
		case <-ticker.C:
		}
	}
}

func parseTransferOffset(req bridgetasks.TaskDataAttachRequest) (int64, error) {
	if req.Offset == nil || *req.Offset == "" {
		return 0, nil
	}
	offset, err := strconv.ParseInt(*req.Offset, 10, 64)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("invalid transfer offset")
	}
	return offset, nil
}

func transferPct(bytes, total int64) int {
	if total <= 0 {
		return 0
	}
	pct := int(bytes * 100 / total)
	if pct > 100 {
		return 100
	}
	return pct
}

func archiveNameForPaths(paths []string, extension string) string {
	if len(paths) == 1 {
		base := filepath.Base(paths[0])
		if base != "" && base != "." && base != "/" {
			return base + extension
		}
	}
	return "download" + extension
}

func newArchiveTaskCallbacks(ctx context.Context, transfer *archiveTransferTask, store *config.UserStore) *ipc.OperationCallbacks {
	limiter := newProgressLimiter(taskSettingsForTask(ctx, transfer.task, store), transfer.total)
	return &ipc.OperationCallbacks{
		Cancel: func() bool {
			return ctx.Err() != nil
		},
		Progress: func(n int64) {
			processed, _, ok := limiter.Add(n)
			if !ok {
				return
			}
			transfer.mu.Lock()
			transfer.bytes = processed
			transfer.mu.Unlock()
			transfer.reportProgress("compressing")
		},
	}
}

func (t *uploadTransferTask) attach(stream net.Conn, req bridgetasks.TaskDataAttachRequest) error {
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

	err = t.prepare(root)
	if err != nil {
		return t.fail(stream, err.Error(), 500, err)
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
	return t.receiveUpload(stream, root, file, newTransferProgressGate(uploadProgressAckIntervalBytes))
}

func (t *uploadTransferTask) receiveUpload(stream net.Conn, root *fsroot.FSRoot, file *os.File, progressGate *transferProgressGate) error {
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
			if err := t.writeUploadChunk(stream, file, frame.Payload, progressGate); err != nil {
				return err
			}
		case ipc.OpStreamClose:
			if t.isComplete() {
				return t.complete(stream, root, file)
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

func (t *uploadTransferTask) beginAttach(stream net.Conn, offset int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.attached {
		return fmt.Errorf("transfer already has an attached data stream")
	}
	if offset != t.bytes {
		return fmt.Errorf("offset mismatch: expected %d, got %d", t.bytes, offset)
	}
	if t.expectedSize >= 0 && offset > t.expectedSize {
		return fmt.Errorf("offset exceeds transfer size")
	}
	t.attached = true
	t.active = stream
	signalActivity(t.activity)
	return nil
}

func (t *uploadTransferTask) endAttach(stream net.Conn) {
	t.mu.Lock()
	if t.active == stream {
		t.attached = false
		t.active = nil
	}
	t.mu.Unlock()
}

func (t *uploadTransferTask) prepare(root *fsroot.FSRoot) error {
	t.mu.Lock()
	if t.tempRel != "" {
		t.mu.Unlock()
		return nil
	}
	t.mu.Unlock()

	realPath := filepath.Clean(t.path)
	realRel := fsroot.ToRel(realPath)
	attrs, err := loadUploadAttributes(root, realRel)
	if err != nil {
		return err
	}
	err = root.Root.MkdirAll(fsroot.ToRel(filepath.Dir(realPath)), services.PermDir)
	if err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	partName := "." + filepath.Base(realRel) + ".linuxio-upload-" + t.task.ID() + ".part"
	tempRel := filepath.Join(filepath.Dir(realRel), partName)
	file, err := root.Root.OpenFile(tempRel, os.O_RDWR|os.O_CREATE|os.O_TRUNC, services.PermFile)
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

func (t *uploadTransferTask) writeUploadChunk(stream net.Conn, file *os.File, payload []byte, progressGate *transferProgressGate) error {
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
	if progressGate.ShouldReport(bytes, total) {
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
	attrs := t.attrs
	path := t.path
	t.mu.Unlock()

	if total >= 0 && bytes != total {
		return t.fail(stream, fmt.Sprintf("size mismatch: expected %d, got %d", total, bytes), 400, fmt.Errorf("size mismatch"))
	}
	if err := file.Sync(); err != nil {
		return t.fail(stream, fmt.Sprintf("sync upload: %v", err), 500, err)
	}
	if err := file.Close(); err != nil {
		return t.fail(stream, fmt.Sprintf("close upload: %v", err), 500, err)
	}
	if err := root.Root.Rename(tempRel, finalRel); err != nil {
		return t.fail(stream, fmt.Sprintf("finalize upload: %v", err), 500, err)
	}

	restoreUploadedFile(root, finalRel, attrs)
	if finalInfo, err := root.Root.Stat(finalRel); err == nil {
		notifyUploadedFile(path, finalInfo)
	}

	result := FileUploadResult{Path: path, Size: bytes}
	t.reportProgress("completed")
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, result))
	t.finish(result, nil)
	slog.Info("upload complete", "path", path, "size", bytes, "task_id", t.task.ID())
	return nil
}

func (t *uploadTransferTask) writeProgress(stream net.Conn, phase string) {
	t.mu.Lock()
	progress := FileProgress{
		Bytes: t.bytes,
		Total: t.expectedSize,
		Pct:   transferPct(t.bytes, t.expectedSize),
		Phase: phase,
	}
	t.mu.Unlock()

	t.task.ReportProgress(progress)
	logWriteErr("progress", ipc.WriteProgress(stream, 0, progress))
}

func (t *uploadTransferTask) reportProgress(phase string) {
	t.mu.Lock()
	progress := FileProgress{
		Bytes: t.bytes,
		Total: t.expectedSize,
		Pct:   transferPct(t.bytes, t.expectedSize),
		Phase: phase,
	}
	t.mu.Unlock()
	t.task.ReportProgress(progress)
}

func (t *uploadTransferTask) markWaiting() {
	t.reportProgress("waiting_for_client")
}

func (t *uploadTransferTask) fail(stream net.Conn, message string, code int, err error) error {
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

func (t *uploadTransferTask) cancel() {
	t.mu.Lock()
	active := t.active
	t.mu.Unlock()
	if active != nil {
		_ = active.Close()
	}
	t.cleanupPartial()
	t.finish(nil, context.Canceled)
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

func (t *uploadTransferTask) finish(result any, err error) {
	t.finishOnce.Do(func() {
		t.done <- transferOutcome{result: result, err: err}
	})
}

func (t *downloadTransferTask) attach(stream net.Conn, req bridgetasks.TaskDataAttachRequest) error {
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

	file, err := root.Root.Open(t.realRel)
	if err != nil {
		return t.fail(stream, fmt.Sprintf("cannot open file: %v", err), 500, err)
	}
	defer file.Close()

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return t.fail(stream, fmt.Sprintf("cannot resume download: %v", err), 500, err)
	}

	t.writeProgress(stream, "streaming")
	if err := t.streamChunks(stream, file); err != nil {
		return err
	}

	result := FileDownloadResult{Path: t.path, Size: t.total, FileName: t.fileName}
	t.reportProgress("completed")
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, result))
	t.finish(result, nil)
	slog.Info("download complete", "path", t.path, "size", t.total, "task_id", t.task.ID())
	return nil
}

func (t *downloadTransferTask) beginAttach(stream net.Conn, offset int64) error {
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
	t.bytes = offset
	t.attached = true
	t.active = stream
	signalActivity(t.activity)
	return nil
}

func (t *downloadTransferTask) endAttach(stream net.Conn) {
	t.mu.Lock()
	if t.active == stream {
		t.attached = false
		t.active = nil
	}
	t.mu.Unlock()
}

func (t *downloadTransferTask) streamChunks(stream net.Conn, file io.Reader) error {
	buf := make([]byte, progressReportIntervalBytes)
	progressGate := newTransferProgressGate(transferProgressMaxBytes)

	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			if err := ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
				Opcode:   ipc.OpStreamData,
				StreamID: 0,
				Payload:  buf[:n],
			}); err != nil {
				t.markWaiting()
				return nil
			}

			t.mu.Lock()
			t.bytes += int64(n)
			bytes := t.bytes
			total := t.total
			t.mu.Unlock()
			signalActivity(t.activity)

			if progressGate.ShouldReport(bytes, total) {
				t.writeProgress(stream, "streaming")
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return t.fail(stream, fmt.Sprintf("read error: %v", readErr), 500, readErr)
		}
	}
	return nil
}

func (t *downloadTransferTask) writeProgress(stream net.Conn, phase string) {
	t.mu.Lock()
	progress := FileProgress{
		Bytes: t.bytes,
		Total: t.total,
		Pct:   transferPct(t.bytes, t.total),
		Phase: phase,
	}
	t.mu.Unlock()

	t.task.ReportProgress(progress)
	logWriteErr("progress", ipc.WriteProgress(stream, 0, progress))
}

func (t *downloadTransferTask) reportProgress(phase string) {
	t.mu.Lock()
	progress := FileProgress{
		Bytes: t.bytes,
		Total: t.total,
		Pct:   transferPct(t.bytes, t.total),
		Phase: phase,
	}
	t.mu.Unlock()
	t.task.ReportProgress(progress)
}

func (t *downloadTransferTask) markWaiting() {
	t.reportProgress("waiting_for_client")
}

func (t *downloadTransferTask) fail(stream net.Conn, message string, code int, err error) error {
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

func (t *downloadTransferTask) cancel() {
	t.mu.Lock()
	active := t.active
	t.mu.Unlock()
	if active != nil {
		_ = active.Close()
	}
	t.finish(nil, context.Canceled)
}

func (t *downloadTransferTask) finish(result any, err error) {
	t.finishOnce.Do(func() {
		t.done <- transferOutcome{result: result, err: err}
	})
}

func (t *archiveTransferTask) attach(stream net.Conn, req bridgetasks.TaskDataAttachRequest) error {
	offset, err := parseTransferOffset(req)
	if err != nil {
		return ipc.WriteResultErrorAndClose(stream, 0, err.Error(), 400)
	}

	<-t.ready
	if t.readyErr != nil {
		return ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("archive creation failed: %v", t.readyErr), 500)
	}

	err = t.beginAttach(stream, offset)
	if err != nil {
		return ipc.WriteResultErrorAndClose(stream, 0, err.Error(), 409)
	}
	defer t.endAttach(stream)

	t.mu.Lock()
	archivePath := t.archive
	t.mu.Unlock()

	file, err := os.Open(archivePath)
	if err != nil {
		return t.fail(stream, fmt.Sprintf("cannot open archive: %v", err), 500, err)
	}
	defer file.Close()

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return t.fail(stream, fmt.Sprintf("cannot resume archive download: %v", err), 500, err)
	}

	t.writeProgress(stream, "streaming")
	if err := t.streamChunks(stream, file); err != nil {
		return err
	}

	t.mu.Lock()
	result := FileArchiveResult{ArchiveName: t.archiveName, Size: t.archiveSize, Format: t.format}
	t.mu.Unlock()

	t.reportProgress("completed")
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, result))
	t.finish(result, nil)
	slog.Info("archive download complete", "count", len(t.paths), "size", t.archiveSize, "format", t.format, "task_id", t.task.ID())
	return nil
}

func (t *archiveTransferTask) beginAttach(stream net.Conn, offset int64) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.attached {
		return fmt.Errorf("transfer already has an attached data stream")
	}
	if offset != t.bytes {
		return fmt.Errorf("offset mismatch: expected %d, got %d", t.bytes, offset)
	}
	if offset > t.archiveSize {
		return fmt.Errorf("offset exceeds archive size")
	}
	t.bytes = offset
	t.attached = true
	t.active = stream
	signalActivity(t.activity)
	return nil
}

func (t *archiveTransferTask) endAttach(stream net.Conn) {
	t.mu.Lock()
	if t.active == stream {
		t.attached = false
		t.active = nil
	}
	t.mu.Unlock()
}

func (t *archiveTransferTask) streamChunks(stream net.Conn, file io.Reader) error {
	buf := make([]byte, progressReportIntervalBytes)
	progressGate := newTransferProgressGate(transferProgressMaxBytes)

	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			if err := ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
				Opcode:   ipc.OpStreamData,
				StreamID: 0,
				Payload:  buf[:n],
			}); err != nil {
				t.markWaiting()
				return nil
			}

			t.mu.Lock()
			t.bytes += int64(n)
			bytes := t.bytes
			total := t.archiveSize
			t.mu.Unlock()
			signalActivity(t.activity)

			if progressGate.ShouldReport(bytes, total) {
				t.writeProgress(stream, "streaming")
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return t.fail(stream, fmt.Sprintf("read archive: %v", readErr), 500, readErr)
		}
	}
	return nil
}

func (t *archiveTransferTask) writeProgress(stream net.Conn, phase string) {
	t.mu.Lock()
	progress := FileProgress{
		Bytes: t.bytes,
		Total: t.archiveSize,
		Pct:   transferPct(t.bytes, t.archiveSize),
		Phase: phase,
	}
	t.mu.Unlock()

	t.task.ReportProgress(progress)
	logWriteErr("progress", ipc.WriteProgress(stream, 0, progress))
}

func (t *archiveTransferTask) reportProgress(phase string) {
	t.mu.Lock()
	total := t.total
	if phase == "streaming" || phase == "waiting_for_client" || phase == "completed" {
		total = t.archiveSize
	}
	progress := FileProgress{
		Bytes: t.bytes,
		Total: total,
		Pct:   transferPct(t.bytes, total),
		Phase: phase,
	}
	t.mu.Unlock()
	t.task.ReportProgress(progress)
}

func (t *archiveTransferTask) markWaiting() {
	t.reportProgress("waiting_for_client")
}

func (t *archiveTransferTask) setReady(size int64) {
	t.mu.Lock()
	t.archiveSize = size
	t.bytes = 0
	t.mu.Unlock()
	t.readyOnce.Do(func() {
		close(t.ready)
	})
}

func (t *archiveTransferTask) setReadyError(err error) {
	t.mu.Lock()
	t.readyErr = err
	t.mu.Unlock()
	t.readyOnce.Do(func() {
		close(t.ready)
	})
}

func (t *archiveTransferTask) fail(stream net.Conn, message string, code int, err error) error {
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

func (t *archiveTransferTask) cancel() {
	t.mu.Lock()
	active := t.active
	t.mu.Unlock()
	if active != nil {
		_ = active.Close()
	}
	t.cleanupArchive()
	t.finish(nil, context.Canceled)
}

func (t *archiveTransferTask) cleanupArchive() {
	t.mu.Lock()
	archivePath := t.archive
	t.mu.Unlock()
	if archivePath == "" {
		return
	}
	if err := os.Remove(archivePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Debug("failed to remove task archive", "path", archivePath, "task_id", t.task.ID(), "error", err)
	}
}

func (t *archiveTransferTask) finish(result any, err error) {
	t.finishOnce.Do(func() {
		t.done <- transferOutcome{result: result, err: err}
	})
}
