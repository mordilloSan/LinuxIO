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
	"sync"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/userconfig"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
)

// archiveTransferTask builds an archive in a temp file, then streams it to the
// client. The ready gate parks an attaching client until the archive exists.
type archiveTransferTask struct {
	*transferLifecycle
	format      string
	paths       []string
	archiveName string
	ready       chan struct{}

	// size is the uncompressed size compression progress is measured against.
	// It lands mid-task because its walk runs alongside the archive rather than
	// gating it; an empty selection reads as "0 bytes", not "still counting".
	size *sizeEstimate

	readyOnce sync.Once

	// archive, archiveSize and readyErr are guarded by mu.
	archive     string
	archiveSize int64
	readyErr    error
}

func runArchiveTask(ctx context.Context, task *bridgeipc.Task, store *userconfig.UserStore, req apischema.FileArchiveRequest) (any, error) {
	if req.Format == "" || len(req.Paths) == 0 {
		return nil, fmt.Errorf("%w: missing format or paths", bridgeipc.ErrInvalidArgs)
	}

	paths := append([]string(nil), req.Paths...)
	extension, err := archiveExtension(req.Format)
	if err != nil {
		return nil, fmt.Errorf("%w: unsupported format: %s", bridgeipc.ErrInvalidArgs, req.Format)
	}
	// Register before any slow work. The client opens its data stream as soon
	// as the start reply carries the task id, and attachFileTransferData only
	// waits transferAttachWaitTimeout for this entry to appear. Both the
	// heavy-archive slot and the size estimate (a full recursive walk) can
	// outlast that on a large folder, so registering after them raced the
	// client into a spurious "transfer task not ready" 404 while the task kept
	// archiving for a client that had already given up.
	transfer := &archiveTransferTask{
		format:      req.Format,
		paths:       paths,
		archiveName: archiveNameForPaths(paths, extension),
		size:        &sizeEstimate{},
		ready:       make(chan struct{}),
	}
	transfer.transferLifecycle = newTransferLifecycle(task, transfer.cleanupArchive, transfer.reportProgress)
	defer registerTransfer(task.ID(), transfer)()
	defer transfer.cleanupArchive()
	// Declared last so it runs first: every early return below must release a
	// client already parked in attach() on <-t.ready before cleanupArchive
	// pulls the temp file out from under it. A no-op once the archive is ready.
	defer transfer.setReadyError(errors.New("archive task ended before the archive was ready"))

	transfer.reportProgress("preparing")

	settings := taskSettingsForTask(ctx, task, store)
	defer estimateArchiveSizeAsync(ctx, paths, transfer.size)()

	release, err := heavyArchiveLimiter.acquire(ctx, settings.HeavyArchiveConcurrency)
	if err != nil {
		return nil, context.Canceled
	}
	defer release()

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

	callbacks := newProgressCallbacks(ctx, newJobThrottle(settings), transfer.size, func(processed, _ int64, _ int, _ bool) {
		transfer.mu.Lock()
		transfer.bytes = processed
		transfer.mu.Unlock()
		transfer.reportProgress("compressing")
	})
	err = createArchive(req.Format, tempPath, callbacks, archiveCompressionWorkers(settings), paths)
	if err != nil {
		transfer.setReadyError(err)
		if errors.Is(err, context.Canceled) || errors.Is(err, services.ErrAborted) {
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

	return transfer.await(ctx)
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

func (t *archiveTransferTask) attach(stream net.Conn, req bridgeipc.TaskDataAttachRequest) error {
	<-t.ready
	if t.readyErr != nil {
		return ipc.WriteResultErrorAndClose(stream, 0, fmt.Sprintf("archive creation failed: %v", t.readyErr), 500)
	}

	offset, ok, err := t.attachStream(stream, req, t.archiveSize)
	if !ok {
		return err
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

	if _, err = file.Seek(offset, io.SeekStart); err != nil {
		return t.fail(stream, fmt.Sprintf("cannot resume archive download: %v", err), 500, err)
	}

	t.writeProgress(stream, "streaming")
	done, err := t.streamChunks(stream, file)
	if err != nil || !done {
		return err
	}

	t.succeed(stream, FileArchiveResult{ArchiveName: t.archiveName, Size: t.archiveSize, Format: t.format})
	slog.Debug("archive download complete", "count", len(t.paths), "size", t.archiveSize, "format", t.format, "task_id", t.task.ID())
	return nil
}

// streamChunks copies the archive to the client. done is false when the client
// went away mid-stream: the task then waits for a resume instead of completing.
func (t *archiveTransferTask) streamChunks(stream net.Conn, file io.Reader) (done bool, err error) {
	buf := make([]byte, progressReportIntervalBytes)
	gate := newTransferGate(transferProgressMaxBytes)

	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			if err := ipc.WriteRelayFrame(stream, &ipc.StreamFrame{
				Opcode:   ipc.OpStreamData,
				StreamID: 0,
				Payload:  buf[:n],
			}); err != nil {
				t.markWaiting()
				return false, nil
			}

			t.mu.Lock()
			t.bytes += int64(n)
			bytes := t.bytes
			total := t.archiveSize
			t.mu.Unlock()
			signalActivity(t.activity)

			if _, _, ok := gate.Set(bytes, total); ok {
				t.writeProgress(stream, "streaming")
			}
		}

		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return false, t.fail(stream, fmt.Sprintf("read archive: %v", readErr), 500, readErr)
		}
	}
	return true, nil
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

	t.sendProgress(stream, progress, downloadStreamProgress{FileProgress: progress, FileName: t.archiveName})
}

func (t *archiveTransferTask) reportProgress(phase string) {
	total, known := t.size.get()
	t.mu.Lock()
	// Once the archive exists its own size is the denominator and is always
	// known; only the phases that run against the source tree can be waiting on
	// the size estimate.
	indeterminate := !known
	if phase == "streaming" || phase == "waiting_for_client" || phase == "completed" {
		total = t.archiveSize
		indeterminate = false
	}
	progress := FileProgress{
		Bytes:         t.bytes,
		Total:         total,
		Pct:           transferPct(t.bytes, total),
		Phase:         phase,
		Indeterminate: indeterminate,
	}
	t.mu.Unlock()
	t.task.ReportProgress(progress)
}

// setReady and setReadyError both resolve the ready gate exactly once, so the
// first outcome wins: the defensive setReadyError on the task's exit path
// cannot overwrite a successful setReady with a spurious error.
func (t *archiveTransferTask) setReady(size int64) {
	t.readyOnce.Do(func() {
		t.mu.Lock()
		t.archiveSize = size
		t.bytes = 0
		t.mu.Unlock()
		close(t.ready)
	})
}

func (t *archiveTransferTask) setReadyError(err error) {
	t.readyOnce.Do(func() {
		t.mu.Lock()
		t.readyErr = err
		t.mu.Unlock()
		close(t.ready)
	})
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
