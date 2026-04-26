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

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	bridgejobs "github.com/mordilloSan/LinuxIO/backend/bridge/jobs"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
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

type uploadTransferJob struct {
	job          *bridgejobs.Job
	path         string
	expectedSize int64
	done         chan transferOutcome
	finishOnce   sync.Once

	mu       sync.Mutex
	bytes    int64
	attached bool
	active   net.Conn
	finalRel string
	tempRel  string
	attrs    uploadAttributes
}

type downloadTransferJob struct {
	job      *bridgejobs.Job
	path     string
	realRel  string
	fileName string
	total    int64
	done     chan transferOutcome

	finishOnce sync.Once
	mu         sync.Mutex
	bytes      int64
	attached   bool
	active     net.Conn
}

type archiveTransferJob struct {
	job         *bridgejobs.Job
	format      string
	paths       []string
	archive     string
	archiveName string
	total       int64
	done        chan transferOutcome
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

var fileTransferJobs sync.Map

func parseUploadArgs(args []string) (string, int64, error) {
	if len(args) < 2 {
		return "", 0, fmt.Errorf("missing path or size")
	}

	expectedSize, err := strconv.ParseInt(args[1], 10, 64)
	if err != nil {
		return "", 0, fmt.Errorf("invalid size: %w", err)
	}

	return args[0], expectedSize, nil
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
	go func(stat os.FileInfo) {
		if err := addToIndexer(path, stat); err != nil {
			slog.Debug("failed to update indexer after upload", "path", path, "error", err)
		}
	}(info)
}

func runUploadJob(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
	path, expectedSize, err := parseUploadArgs(args)
	if err != nil {
		return nil, bridgejobs.NewError(err.Error(), 400)
	}

	transfer := &uploadTransferJob{
		job:          job,
		path:         filepath.Clean(path),
		expectedSize: expectedSize,
		done:         make(chan transferOutcome, 1),
	}
	fileTransferJobs.Store(job.ID(), transfer)
	defer fileTransferJobs.Delete(job.ID())

	transfer.reportProgress("waiting_for_client")
	select {
	case outcome := <-transfer.done:
		return outcome.result, outcome.err
	case <-ctx.Done():
		transfer.cancel()
		return nil, context.Canceled
	}
}

func runDownloadJob(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
	if len(args) < 1 {
		return nil, bridgejobs.NewError("missing file path", 400)
	}

	path := filepath.Clean(args[0])
	root, err := fsroot.Open()
	if err != nil {
		return nil, fmt.Errorf("failed to access filesystem: %w", err)
	}
	defer root.Close()

	realRel := fsroot.ToRel(path)
	stat, err := root.Root.Stat(realRel)
	if err != nil {
		return nil, bridgejobs.NewError(fmt.Sprintf("file not found: %v", err), 404)
	}
	if stat.IsDir() {
		return nil, bridgejobs.NewError("path is a directory, use archive download instead", 400)
	}

	transfer := &downloadTransferJob{
		job:      job,
		path:     path,
		realRel:  realRel,
		fileName: filepath.Base(path),
		total:    stat.Size(),
		done:     make(chan transferOutcome, 1),
	}
	fileTransferJobs.Store(job.ID(), transfer)
	defer fileTransferJobs.Delete(job.ID())

	transfer.reportProgress("waiting_for_client")
	select {
	case outcome := <-transfer.done:
		return outcome.result, outcome.err
	case <-ctx.Done():
		transfer.cancel()
		return nil, context.Canceled
	}
}

func runArchiveJob(ctx context.Context, job *bridgejobs.Job, args []string) (any, error) {
	if len(args) < 2 {
		return nil, bridgejobs.NewError("missing format or paths", 400)
	}

	format := args[0]
	paths := append([]string(nil), args[1:]...)
	extension, err := archiveExtension(format)
	if err != nil {
		return nil, bridgejobs.NewError(fmt.Sprintf("unsupported format: %s", format), 400)
	}

	transfer := &archiveTransferJob{
		job:         job,
		format:      format,
		paths:       paths,
		archiveName: archiveNameForPaths(paths, extension),
		total:       computeArchiveSize(paths),
		done:        make(chan transferOutcome, 1),
		ready:       make(chan struct{}),
	}
	fileTransferJobs.Store(job.ID(), transfer)
	defer fileTransferJobs.Delete(job.ID())
	defer transfer.cleanupArchive()

	transfer.reportProgress("preparing")
	tempFile, err := os.CreateTemp("", "linuxio-job-archive-*"+extension)
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

	callbacks := newArchiveJobCallbacks(ctx, transfer)
	err = createArchive(format, tempPath, callbacks, paths)
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

	select {
	case outcome := <-transfer.done:
		return outcome.result, outcome.err
	case <-ctx.Done():
		transfer.cancel()
		return nil, context.Canceled
	}
}

func attachFileTransferData(ctx context.Context, job *bridgejobs.Job, stream net.Conn, args []string) error {
	transfer, ok := waitForFileTransferJob(ctx, job.ID())
	if !ok {
		return ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("transfer job not ready: %s", job.ID()), 404)
	}

	switch active := transfer.(type) {
	case *uploadTransferJob:
		return active.attach(stream, args)
	case *downloadTransferJob:
		return active.attach(stream, args)
	case *archiveTransferJob:
		return active.attach(stream, args)
	default:
		return ipc.WriteResultErrorAndClose(stream, 0, "unsupported transfer job", 400)
	}
}

func waitForFileTransferJob(ctx context.Context, jobID string) (any, bool) {
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		if transfer, ok := fileTransferJobs.Load(jobID); ok {
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

func parseTransferOffset(args []string) (int64, error) {
	if len(args) == 0 || args[0] == "" {
		return 0, nil
	}
	offset, err := strconv.ParseInt(args[0], 10, 64)
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

func newArchiveJobCallbacks(ctx context.Context, transfer *archiveTransferJob) *ipc.OperationCallbacks {
	var processed int64
	var last int64
	return &ipc.OperationCallbacks{
		Cancel: func() bool {
			return ctx.Err() != nil
		},
		Progress: func(n int64) {
			processed += n
			if transfer.total > 0 && processed-last < progressReportIntervalBytes && processed < transfer.total {
				return
			}
			last = processed
			transfer.mu.Lock()
			transfer.bytes = processed
			transfer.mu.Unlock()
			transfer.reportProgress("compressing")
		},
	}
}

func (t *uploadTransferJob) attach(stream net.Conn, args []string) error {
	offset, err := parseTransferOffset(args)
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
	return t.receiveUpload(stream, root, file)
}

func (t *uploadTransferJob) receiveUpload(stream net.Conn, root *fsroot.FSRoot, file *os.File) error {
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
			if err := t.writeUploadChunk(stream, file, frame.Payload); err != nil {
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

func (t *uploadTransferJob) beginAttach(stream net.Conn, offset int64) error {
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
	return nil
}

func (t *uploadTransferJob) endAttach(stream net.Conn) {
	t.mu.Lock()
	if t.active == stream {
		t.attached = false
		t.active = nil
	}
	t.mu.Unlock()
}

func (t *uploadTransferJob) prepare(root *fsroot.FSRoot) error {
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

	partName := "." + filepath.Base(realRel) + ".linuxio-upload-" + t.job.ID() + ".part"
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

func (t *uploadTransferJob) writeUploadChunk(stream net.Conn, file *os.File, payload []byte) error {
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

	if total >= 0 && bytes > total {
		return t.fail(stream, fmt.Sprintf("size mismatch: expected %d, got at least %d", total, bytes), 400, fmt.Errorf("size mismatch"))
	}
	if bytes%uploadProgressAckIntervalBytes < int64(n) || bytes == total {
		t.writeProgress(stream, "uploading")
	}
	return nil
}

func (t *uploadTransferJob) isComplete() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.expectedSize >= 0 && t.bytes == t.expectedSize
}

func (t *uploadTransferJob) complete(stream net.Conn, root *fsroot.FSRoot, file *os.File) error {
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

	result := map[string]any{"path": path, "size": bytes}
	t.reportProgress("completed")
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, result))
	t.finish(result, nil)
	slog.Info("upload complete", "path", path, "size", bytes, "job_id", t.job.ID())
	return nil
}

func (t *uploadTransferJob) writeProgress(stream net.Conn, phase string) {
	t.mu.Lock()
	progress := FileProgress{
		Bytes: t.bytes,
		Total: t.expectedSize,
		Pct:   transferPct(t.bytes, t.expectedSize),
		Phase: phase,
	}
	t.mu.Unlock()

	t.job.ReportProgress(progress)
	logWriteErr("progress", ipc.WriteProgress(stream, 0, progress))
}

func (t *uploadTransferJob) reportProgress(phase string) {
	t.mu.Lock()
	progress := FileProgress{
		Bytes: t.bytes,
		Total: t.expectedSize,
		Pct:   transferPct(t.bytes, t.expectedSize),
		Phase: phase,
	}
	t.mu.Unlock()
	t.job.ReportProgress(progress)
}

func (t *uploadTransferJob) markWaiting() {
	t.reportProgress("waiting_for_client")
}

func (t *uploadTransferJob) fail(stream net.Conn, message string, code int, err error) error {
	t.cleanupPartial()
	jobErr := bridgejobs.NewError(message, code)
	if stream != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, message, code))
	}
	t.finish(nil, jobErr)
	if err != nil {
		return err
	}
	return jobErr
}

func (t *uploadTransferJob) cancel() {
	t.mu.Lock()
	active := t.active
	t.mu.Unlock()
	if active != nil {
		_ = active.Close()
	}
	t.cleanupPartial()
	t.finish(nil, context.Canceled)
}

func (t *uploadTransferJob) cleanupPartial() {
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

func (t *uploadTransferJob) finish(result any, err error) {
	t.finishOnce.Do(func() {
		t.done <- transferOutcome{result: result, err: err}
	})
}

func (t *downloadTransferJob) attach(stream net.Conn, args []string) error {
	offset, err := parseTransferOffset(args)
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

	result := map[string]any{
		"path":     t.path,
		"size":     t.total,
		"fileName": t.fileName,
	}
	t.reportProgress("completed")
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, result))
	t.finish(result, nil)
	slog.Info("download complete", "path", t.path, "size", t.total, "job_id", t.job.ID())
	return nil
}

func (t *downloadTransferJob) beginAttach(stream net.Conn, offset int64) error {
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
	return nil
}

func (t *downloadTransferJob) endAttach(stream net.Conn) {
	t.mu.Lock()
	if t.active == stream {
		t.attached = false
		t.active = nil
	}
	t.mu.Unlock()
}

func (t *downloadTransferJob) streamChunks(stream net.Conn, file io.Reader) error {
	buf := make([]byte, progressReportIntervalBytes)
	var lastProgress int64

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

			if bytes-lastProgress >= progressReportIntervalBytes || bytes == total {
				t.writeProgress(stream, "streaming")
				lastProgress = bytes
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

func (t *downloadTransferJob) writeProgress(stream net.Conn, phase string) {
	t.mu.Lock()
	progress := FileProgress{
		Bytes: t.bytes,
		Total: t.total,
		Pct:   transferPct(t.bytes, t.total),
		Phase: phase,
	}
	t.mu.Unlock()

	t.job.ReportProgress(progress)
	logWriteErr("progress", ipc.WriteProgress(stream, 0, progress))
}

func (t *downloadTransferJob) reportProgress(phase string) {
	t.mu.Lock()
	progress := FileProgress{
		Bytes: t.bytes,
		Total: t.total,
		Pct:   transferPct(t.bytes, t.total),
		Phase: phase,
	}
	t.mu.Unlock()
	t.job.ReportProgress(progress)
}

func (t *downloadTransferJob) markWaiting() {
	t.reportProgress("waiting_for_client")
}

func (t *downloadTransferJob) fail(stream net.Conn, message string, code int, err error) error {
	jobErr := bridgejobs.NewError(message, code)
	if stream != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, message, code))
	}
	t.finish(nil, jobErr)
	if err != nil {
		return err
	}
	return jobErr
}

func (t *downloadTransferJob) cancel() {
	t.mu.Lock()
	active := t.active
	t.mu.Unlock()
	if active != nil {
		_ = active.Close()
	}
	t.finish(nil, context.Canceled)
}

func (t *downloadTransferJob) finish(result any, err error) {
	t.finishOnce.Do(func() {
		t.done <- transferOutcome{result: result, err: err}
	})
}

func (t *archiveTransferJob) attach(stream net.Conn, args []string) error {
	offset, err := parseTransferOffset(args)
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
	result := map[string]any{
		"archiveName": t.archiveName,
		"size":        t.archiveSize,
		"format":      t.format,
	}
	t.mu.Unlock()

	t.reportProgress("completed")
	logWriteErr("ok+close", ipc.WriteResultOKAndClose(stream, 0, result))
	t.finish(result, nil)
	slog.Info("archive download complete", "count", len(t.paths), "size", t.archiveSize, "format", t.format, "job_id", t.job.ID())
	return nil
}

func (t *archiveTransferJob) beginAttach(stream net.Conn, offset int64) error {
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
	return nil
}

func (t *archiveTransferJob) endAttach(stream net.Conn) {
	t.mu.Lock()
	if t.active == stream {
		t.attached = false
		t.active = nil
	}
	t.mu.Unlock()
}

func (t *archiveTransferJob) streamChunks(stream net.Conn, file io.Reader) error {
	buf := make([]byte, progressReportIntervalBytes)
	var lastProgress int64

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

			if bytes-lastProgress >= progressReportIntervalBytes || bytes == total {
				t.writeProgress(stream, "streaming")
				lastProgress = bytes
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

func (t *archiveTransferJob) writeProgress(stream net.Conn, phase string) {
	t.mu.Lock()
	progress := FileProgress{
		Bytes: t.bytes,
		Total: t.archiveSize,
		Pct:   transferPct(t.bytes, t.archiveSize),
		Phase: phase,
	}
	t.mu.Unlock()

	t.job.ReportProgress(progress)
	logWriteErr("progress", ipc.WriteProgress(stream, 0, progress))
}

func (t *archiveTransferJob) reportProgress(phase string) {
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
	t.job.ReportProgress(progress)
}

func (t *archiveTransferJob) markWaiting() {
	t.reportProgress("waiting_for_client")
}

func (t *archiveTransferJob) setReady(size int64) {
	t.mu.Lock()
	t.archiveSize = size
	t.bytes = 0
	t.mu.Unlock()
	t.readyOnce.Do(func() {
		close(t.ready)
	})
}

func (t *archiveTransferJob) setReadyError(err error) {
	t.mu.Lock()
	t.readyErr = err
	t.mu.Unlock()
	t.readyOnce.Do(func() {
		close(t.ready)
	})
}

func (t *archiveTransferJob) fail(stream net.Conn, message string, code int, err error) error {
	jobErr := bridgejobs.NewError(message, code)
	if stream != nil {
		logWriteErr("error+close", ipc.WriteResultErrorAndClose(stream, 0, message, code))
	}
	t.finish(nil, jobErr)
	if err != nil {
		return err
	}
	return jobErr
}

func (t *archiveTransferJob) cancel() {
	t.mu.Lock()
	active := t.active
	t.mu.Unlock()
	if active != nil {
		_ = active.Close()
	}
	t.cleanupArchive()
	t.finish(nil, context.Canceled)
}

func (t *archiveTransferJob) cleanupArchive() {
	t.mu.Lock()
	archivePath := t.archive
	t.mu.Unlock()
	if archivePath == "" {
		return
	}
	if err := os.Remove(archivePath); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Debug("failed to remove job archive", "path", archivePath, "job_id", t.job.ID(), "error", err)
	}
}

func (t *archiveTransferJob) finish(result any, err error) {
	t.finishOnce.Do(func() {
		t.done <- transferOutcome{result: result, err: err}
	})
}
