package filebrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

var (
	indexerAvailable      atomic.Bool
	errIndexerUnavailable = errors.New("indexer unavailable")
)

const (
	indexerServiceName = "indexer.service"
	indexerSocketName  = "indexer.socket"
)
const (
	deleteLocalPrescanMaxBytes           int64 = 512 * 1024 * 1024
	deleteLocalPrescanMaxTopLevelEntries       = 1000
)

var getIndexerUnitInfo = systemdapi.GetUnitInfo

func init() {
	indexerAvailable.Store(true)
}

func setIndexerAvailability(available bool) {
	indexerAvailable.Store(available)
}

func isIndexerEnabled() bool {
	return indexerAvailable.Load()
}

// runDetachedIndexerUpdate bounds intentionally fire-and-forget indexer notifications
// that should outlive the request/job which already completed the filesystem change.
func runDetachedIndexerUpdate(label string, fn func(context.Context) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			slog.Debug("detached indexer update failed", "operation", label, "error", err)
		}
	}()
}

// resourceGet retrieves information about a resource.
func resourceGet(ctx context.Context, req apischema.FileResourceGetRequest) (apischema.ExtendedFileInfo, error) {
	if err := ctx.Err(); err != nil {
		return apischema.ExtendedFileInfo{}, err
	}
	if req.Path == "" {
		return apischema.ExtendedFileInfo{}, fmt.Errorf("bad_request:missing path")
	}

	getContent := req.GetContent != nil && *req.GetContent == "true"

	fileInfo, err := services.FileInfoFaster(iteminfo.FileOptions{
		Path:    req.Path,
		Expand:  true,
		Content: getContent,
	})
	if err != nil {
		slog.Debug("error getting file info", "path", req.Path, "error", err)
		return apischema.ExtendedFileInfo{}, fmt.Errorf("bad_request:%v", err)
	}

	return extendedFileInfoResponse(fileInfo), nil
}

func extendedFileInfoResponse(info *iteminfo.ExtendedFileInfo) apischema.ExtendedFileInfo {
	if info == nil {
		return apischema.ExtendedFileInfo{
			Files:   []apischema.FileResourceItem{},
			Folders: []apischema.FileResourceItem{},
		}
	}
	return apischema.ExtendedFileInfo{
		Name:       info.Name,
		Size:       info.Size,
		Modified:   formatResourceModTime(info.ModTime),
		Type:       info.Type,
		Hidden:     info.Hidden,
		HasPreview: info.HasPreview,
		Symlink:    info.Symlink,
		Files:      fileResourceItems(info.Files),
		Folders:    fileResourceItems(info.Folders),
		Path:       info.Path,
		Content:    info.Content,
	}
}

func fileResourceItems(items []iteminfo.ItemInfo) []apischema.FileResourceItem {
	out := make([]apischema.FileResourceItem, 0, len(items))
	for _, item := range items {
		out = append(out, apischema.FileResourceItem{
			Name:       item.Name,
			Size:       item.Size,
			Modified:   formatResourceModTime(item.ModTime),
			Type:       item.Type,
			Hidden:     item.Hidden,
			HasPreview: item.HasPreview,
			Symlink:    item.Symlink,
		})
	}
	return out
}

func formatResourceModTime(modTime time.Time) string {
	if modTime.IsZero() {
		return ""
	}
	return modTime.Format(time.RFC3339Nano)
}

// resourceStat returns extended metadata.
func resourceStat(ctx context.Context, req apischema.PathRequest) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.Path == "" {
		return nil, fmt.Errorf("bad_request:missing path")
	}

	fileInfo, err := services.FileInfoFaster(iteminfo.FileOptions{
		Path:   req.Path,
		Expand: false,
	})
	if err != nil {
		slog.Debug("error getting file stat info", "path", req.Path, "error", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}

	statData, err := iteminfo.CollectStatInfo(fileInfo.RealPath)
	if err != nil {
		slog.Debug("error collecting stat info", "path", fileInfo.RealPath, "error", err)
		return nil, fmt.Errorf("error collecting stat info: %w", err)
	}

	statData.Path = req.Path
	statData.Name = fileInfo.Name
	if statData.Size == 0 {
		statData.Size = fileInfo.Size
	}

	return statData, nil
}

// resourceDelete deletes a resource.
func resourceDelete(ctx context.Context, req apischema.PathRequest, emit bridgeipc.Events) (any, error) {
	if req.Path == "" {
		return nil, fmt.Errorf("bad_request:missing path")
	}

	if req.Path == "/" {
		return nil, fmt.Errorf("bad_request:cannot delete root")
	}

	isDir, err := deleteTargetIsDir(req.Path)
	if err != nil {
		slog.Debug("error getting file info", "path", req.Path, "error", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}

	deleteOpts := deleteOptionsForPath(ctx, req.Path, isDir)
	reportDeleteProgress(emit, 0, deleteOpts.Total, deleteOpts.Indeterminate, "preparing")
	deleteOpts.Progress = newDeleteProgressReporter(emit)

	processed, err := services.DeleteFilesWithProgress(ctx, req.Path, deleteOpts)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		slog.Debug("error deleting file", "path", req.Path, "error", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}

	// Notify indexer about the deletion
	if err := deleteFromIndexer(ctx, req.Path); err != nil {
		slog.Debug("failed to update indexer after delete", "path", req.Path, "error", err)
		// Don't fail the operation if indexer update fails
	}
	slog.Info("delete complete", "path", req.Path)

	return map[string]any{
		"message":   "deleted",
		"processed": processed,
	}, nil
}

func deleteTargetIsDir(path string) (bool, error) {
	root, err := fsroot.Open()
	if err != nil {
		return false, err
	}
	defer root.Close()

	cleanPath := utils.CleanAbsPath(path)
	info, err := root.Root.Lstat(fsroot.ToRel(cleanPath))
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

func deleteOptionsForPath(ctx context.Context, path string, isDir bool) services.DeleteOptions {
	if !isDir {
		return services.DeleteOptions{Total: 1}
	}

	if total, err := fetchEntryCountFromIndexer(ctx, path); err == nil {
		if total > 0 {
			return services.DeleteOptions{Total: total}
		}
		if shouldPrescanDeletePath(path) {
			return services.DeleteOptions{Prescan: true}
		}
		return services.DeleteOptions{Indeterminate: true}
	} else {
		slog.Debug("failed to get delete entry count from indexer", "path", path, "error", err)
	}

	if size, err := fetchDirSizeFromIndexer(ctx, path); err == nil {
		if size > deleteLocalPrescanMaxBytes {
			return services.DeleteOptions{Indeterminate: true}
		}
		if size > 0 || shouldPrescanDeletePath(path) {
			return services.DeleteOptions{Prescan: true}
		}
	} else {
		slog.Debug("failed to get delete directory size from indexer", "path", path, "error", err)
	}

	if shouldPrescanDeletePath(path) {
		return services.DeleteOptions{Prescan: true}
	}
	return services.DeleteOptions{Indeterminate: true}
}

func shouldPrescanDeletePath(path string) bool {
	ok, err := services.TopLevelEntryCountWithin(path, deleteLocalPrescanMaxTopLevelEntries)
	if err != nil {
		slog.Debug("failed to inspect delete directory top-level entries", "path", path, "error", err)
		return false
	}
	return ok
}

func newDeleteProgressReporter(emit bridgeipc.Events) func(processed, total int64, indeterminate bool) {
	var lastProcessed int64 = -1
	lastPct := -1
	var lastAt time.Time
	const minInterval = 250 * time.Millisecond

	return func(processed, total int64, indeterminate bool) {
		pct := deleteProgressPct(processed, total, indeterminate)
		final := !indeterminate && total > 0 && processed >= total
		firstItem := processed <= 1
		now := time.Now()
		if !final && !firstItem && !lastAt.IsZero() && now.Sub(lastAt) < minInterval {
			return
		}
		if !final && processed == lastProcessed && pct == lastPct {
			return
		}
		reportDeleteProgress(emit, processed, total, indeterminate, "deleting")
		lastProcessed = processed
		lastPct = pct
		lastAt = now
	}
}

func reportDeleteProgress(emit bridgeipc.Events, processed, total int64, indeterminate bool, phase string) {
	if err := emit.Progress(DeleteProgress{
		Processed:     processed,
		Total:         total,
		Pct:           deleteProgressPct(processed, total, indeterminate),
		Phase:         phase,
		Indeterminate: indeterminate,
	}); err != nil {
		slog.Debug("failed to write delete progress update", "phase", phase, "error", err)
	}
}

func deleteProgressPct(processed, total int64, indeterminate bool) int {
	if indeterminate || total <= 0 {
		return 0
	}
	return min(int(processed*100/total), 100)
}

type resourcePostRequest struct {
	cleanPath string
	relPath   string
	isDir     bool
	override  bool
}

type resourcePatchRequest struct {
	action    string
	src       string
	dst       string
	realSrc   string
	realDest  string
	overwrite bool
}

type computedTransferSize struct {
	total int64
	known bool
}

func parseResourcePostRequest(req apischema.FileResourcePostRequest) (resourcePostRequest, error) {
	if req.Path == "" {
		return resourcePostRequest{}, fmt.Errorf("bad_request:missing path")
	}

	path, err := url.QueryUnescape(req.Path)
	if err != nil {
		return resourcePostRequest{}, fmt.Errorf("bad_request:invalid path encoding")
	}

	cleanPath := utils.CleanAbsPath(path)
	if cleanPath == "/" {
		return resourcePostRequest{}, fmt.Errorf("bad_request:cannot create root")
	}

	return resourcePostRequest{
		cleanPath: cleanPath,
		relPath:   strings.TrimPrefix(cleanPath, "/"),
		isDir:     strings.HasSuffix(path, "/"),
		override:  req.Override != nil && *req.Override,
	}, nil
}

func ensureResourcePostType(root *fsroot.FSRoot, req resourcePostRequest) error {
	stat, err := root.Root.Stat(req.relPath)
	if err != nil {
		return nil
	}
	if stat.IsDir() != req.isDir && !req.override {
		return fmt.Errorf("bad_request:resource already exists with different type")
	}
	return nil
}

func createDirectoryResource(ctx context.Context, root *fsroot.FSRoot, req resourcePostRequest) (any, error) {
	if stat, statErr := root.Root.Stat(req.relPath); statErr == nil && !stat.IsDir() && req.override {
		if removeErr := root.Root.Remove(req.relPath); removeErr != nil {
			slog.Debug("error removing existing file for directory create", "path", req.cleanPath, "error", removeErr)
			return nil, fmt.Errorf("bad_request:%v", removeErr)
		}
	}

	if mkdirErr := root.Root.MkdirAll(req.relPath, services.PermDir); mkdirErr != nil {
		slog.Debug("error writing directory", "path", req.cleanPath, "error", mkdirErr)
		return nil, fmt.Errorf("bad_request:%v", mkdirErr)
	}
	if chmodErr := root.Root.Chmod(req.relPath, services.PermDir); chmodErr != nil {
		slog.Debug("error setting directory permissions", "path", req.cleanPath, "error", chmodErr)
		return nil, fmt.Errorf("bad_request:%v", chmodErr)
	}

	notifyIndexerForCreatedResource(ctx, root, req.cleanPath, req.relPath, "directory")
	slog.Info("directory created", "path", req.cleanPath)
	return map[string]any{"message": "created"}, nil
}

func createFileResource(ctx context.Context, root *fsroot.FSRoot, req resourcePostRequest) (any, error) {
	parentRel := filepath.Dir(req.relPath)
	if parentRel != "." {
		if mkdirErr := root.Root.MkdirAll(parentRel, services.PermDir); mkdirErr != nil {
			slog.Debug("error creating parent directory", "path", req.cleanPath, "error", mkdirErr)
			return nil, fmt.Errorf("bad_request:failed to create parent directory: %v", mkdirErr)
		}
	}

	if _, statErr := root.Root.Stat(req.relPath); statErr == nil && !req.override {
		return nil, fmt.Errorf("bad_request:file already exists")
	}

	f, err := root.Root.OpenFile(req.relPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, services.PermFile)
	if err != nil {
		slog.Debug("error creating file", "path", req.cleanPath, "error", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}
	if cerr := f.Close(); cerr != nil {
		slog.Warn("failed to close created file", "path", req.cleanPath, "error", cerr)
	}

	notifyIndexerForCreatedResource(ctx, root, req.cleanPath, req.relPath, "file")
	slog.Info("file created", "path", req.cleanPath)
	return map[string]any{"message": "created"}, nil
}

func notifyIndexerForCreatedResource(ctx context.Context, root *fsroot.FSRoot, cleanPath, relPath, kind string) {
	if info, err := root.Root.Stat(relPath); err == nil {
		if err := addToIndexer(ctx, cleanPath, info); err != nil {
			slog.Debug("failed to update indexer after create", "path", cleanPath, "type", kind, "error", err)
		}
	}
}

func parseResourcePatchRequest(req apischema.ActionSourceDestinationRequest) (resourcePatchRequest, error) {
	if req.Action == "" || req.Source == "" || req.Dest == "" {
		return resourcePatchRequest{}, fmt.Errorf("bad_request:missing action, from, or destination")
	}

	src, err := url.QueryUnescape(req.Source)
	if err != nil {
		return resourcePatchRequest{}, fmt.Errorf("bad_request:invalid source path encoding")
	}
	dst, err := url.QueryUnescape(req.Dest)
	if err != nil {
		return resourcePatchRequest{}, fmt.Errorf("bad_request:invalid destination path encoding")
	}
	if dst == "/" || src == "/" {
		return resourcePatchRequest{}, fmt.Errorf("bad_request:cannot modify root directory")
	}

	return resourcePatchRequest{
		action: req.Action,
		src:    src,
		dst:    dst,
	}, nil
}

func prepareResourcePatch(root *fsroot.FSRoot, req resourcePatchRequest) (resourcePatchRequest, error) {
	dstClean := strings.TrimRight(req.dst, "/")
	parentDir := filepath.Dir(dstClean)
	if _, err := root.Root.Stat(fsroot.ToRel(parentDir)); err != nil {
		slog.Debug("parent directory not found", "path", parentDir, "error", err)
		return req, fmt.Errorf("bad_request:parent directory not found")
	}

	req.realDest = filepath.Join(parentDir, filepath.Base(dstClean))
	if strings.HasSuffix(req.dst, "/") && !strings.HasSuffix(req.realDest, "/") {
		req.realDest += "/"
	}
	req.realSrc = utils.CleanAbsPath(req.src)

	srcInfo, err := root.Root.Stat(fsroot.ToRel(req.realSrc))
	if err != nil {
		slog.Debug("error getting source info", "path", req.realSrc, "error", err)
		return req, fmt.Errorf("bad_request:source not found")
	}
	if req.realSrc == req.realDest && req.action == "copy" {
		req.realDest = generateUniquePath(req.realDest, srcInfo.IsDir(), root)
	}
	return req, validatePatchDestination(root, req, srcInfo)
}

func validatePatchDestination(root *fsroot.FSRoot, req resourcePatchRequest, srcInfo os.FileInfo) error {
	destInfo, err := root.Root.Stat(fsroot.ToRel(req.realDest))
	destExists := err == nil
	if err != nil && !os.IsNotExist(err) {
		slog.Debug("error stating destination", "path", req.realDest, "error", err)
		return fmt.Errorf("bad_request:could not stat destination")
	}
	if !destExists {
		return nil
	}
	if req.realSrc == req.realDest {
		return fmt.Errorf("bad_request:source and destination are the same")
	}
	if !req.overwrite {
		return fmt.Errorf("bad_request:destination exists")
	}
	if srcInfo.IsDir() != destInfo.IsDir() {
		return fmt.Errorf("bad_request:destination exists with different type")
	}
	return nil
}

func computeTransferSize(ctx context.Context, path string, info os.FileInfo) computedTransferSize {
	if info != nil && !info.IsDir() {
		return computedTransferSize{total: info.Size(), known: true}
	}

	if info != nil && info.IsDir() {
		if totalSize, err := fetchDirSizeFromIndexer(ctx, path); err == nil {
			if totalSize > 0 || indexerHasEntry(ctx, path) {
				return computedTransferSize{total: totalSize, known: true}
			}
		} else {
			slog.Debug("failed to get transfer size from indexer", "path", path, "error", err)
		}
	}

	totalSize, err := services.ComputeCopySize(path)
	if err != nil {
		slog.Debug("failed to compute filebrowser operation size", "path", path, "error", err)
		return computedTransferSize{}
	}
	return computedTransferSize{total: totalSize, known: true}
}

func indexerHasEntry(ctx context.Context, path string) bool {
	total, err := fetchEntryCountFromIndexer(ctx, path)
	if err != nil {
		slog.Debug("failed to confirm indexed transfer path", "path", path, "error", err)
		return false
	}
	return total > 0
}

func indexerEntrySize(info os.FileInfo, size computedTransferSize) int64 {
	if info == nil {
		return 0
	}
	if info.IsDir() && size.known {
		return size.total
	}
	return info.Size()
}

func moveFileOptions(size computedTransferSize) services.MoveFileOptions {
	if !size.known {
		return services.MoveFileOptions{}
	}
	return services.MoveFileOptions{
		KnownSize:    size.total,
		HasKnownSize: true,
	}
}

func newPatchCallbacks(ctx context.Context, emit bridgeipc.Events, action string, totalSize int64) *ipc.OperationCallbacks {
	var bytesProcessed int64
	var lastProgress int64
	const progressInterval = int64(2 * 1024 * 1024)

	return &ipc.OperationCallbacks{
		Progress: func(n int64) {
			bytesProcessed += n
			if totalSize <= 0 || (bytesProcessed-lastProgress < progressInterval && bytesProcessed < totalSize) {
				return
			}

			phase := "copying"
			if action == "move" || action == "rename" {
				phase = "moving"
			}
			pct := min(int(bytesProcessed*100/totalSize), 100)
			slog.Debug("filebrowser operation progress",
				"action", action,
				"bytes", bytesProcessed,
				"total", totalSize,
				"pct", pct,
				"phase", phase)
			if err := emit.Progress(FileProgress{
				Bytes: bytesProcessed,
				Total: totalSize,
				Pct:   pct,
				Phase: phase,
			}); err != nil {
				slog.Debug("failed to write filebrowser progress update", "action", action, "error", err)
				return
			}
			lastProgress = bytesProcessed
		},
		Cancel: func() bool {
			select {
			case <-ctx.Done():
				return true
			default:
				return false
			}
		},
	}
}

func executeResourcePatch(req resourcePatchRequest, opts *ipc.OperationCallbacks, size computedTransferSize) error {
	switch req.action {
	case "copy":
		return services.CopyFileWithCallbacks(req.realSrc, req.realDest, req.overwrite, opts)
	case "rename", "move":
		return services.MoveFileWithCallbacks(req.realSrc, req.realDest, req.overwrite, opts, moveFileOptions(size))
	default:
		return fmt.Errorf("bad_request:unsupported action: %s", req.action)
	}
}

func notifyIndexerAfterPatch(ctx context.Context, root *fsroot.FSRoot, req resourcePatchRequest, size computedTransferSize, destExisted bool) {
	switch req.action {
	case "copy":
		if info, err := root.Root.Stat(fsroot.ToRel(req.realDest)); err == nil {
			if err := addCopiedPathToIndexer(ctx, req.realDest, info, size, destExisted && req.overwrite); err != nil {
				slog.Debug("failed to update indexer after copy", "path", req.realDest, "error", err)
			}
		}
	case "rename", "move":
		if err := movePathInIndexer(ctx, req.realSrc, req.realDest, size, destExisted && req.overwrite, func() (os.FileInfo, error) {
			return root.Root.Stat(fsroot.ToRel(req.realDest))
		}); err != nil {
			slog.Debug("failed to update indexer after move", "source", req.realSrc, "destination", req.realDest, "error", err)
		}
	}
}

// resourcePost creates a new resource.
func resourcePost(ctx context.Context, req apischema.FileResourcePostRequest) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	postReq, err := parseResourcePostRequest(req)
	if err != nil {
		return nil, err
	}

	root, err := fsroot.Open()
	if err != nil {
		slog.Debug("error opening filesystem root", "error", err)
		return nil, fmt.Errorf("bad_request:failed to access filesystem")
	}
	defer func() {
		if cerr := root.Close(); cerr != nil {
			slog.Warn("failed to close filesystem root", "error", cerr)
		}
	}()

	if err := ensureResourcePostType(root, postReq); err != nil {
		return nil, err
	}

	if postReq.isDir {
		return createDirectoryResource(ctx, root, postReq)
	}
	return createFileResource(ctx, root, postReq)
}

// resourcePatchWithProgress performs patch operations with progress feedback.
func resourcePatchWithProgress(ctx context.Context, req apischema.ActionSourceDestinationRequest, emit bridgeipc.Events) (any, error) {
	patchReq, err := parseResourcePatchRequest(req)
	if err != nil {
		return nil, err
	}

	root, err := fsroot.Open()
	if err != nil {
		return nil, fmt.Errorf("bad_request:failed to access filesystem")
	}
	defer root.Close()

	patchReq, err = prepareResourcePatch(root, patchReq)
	if err != nil {
		return nil, err
	}

	srcInfo, err := root.Root.Stat(fsroot.ToRel(patchReq.realSrc))
	if err != nil {
		slog.Debug("error getting source info", "path", patchReq.realSrc, "error", err)
		return nil, fmt.Errorf("bad_request:source not found")
	}
	_, destStatErr := root.Root.Stat(fsroot.ToRel(patchReq.realDest))
	destExisted := destStatErr == nil

	size := computeTransferSize(ctx, patchReq.realSrc, srcInfo)
	// Send initial progress.
	slog.Info("starting filebrowser operation",
		"action", patchReq.action,
		"source", patchReq.realSrc,
		"destination", patchReq.realDest,
		"size", size.total)
	if err := emit.Progress(FileProgress{
		Total: size.total,
		Phase: "preparing",
	}); err != nil {
		return nil, fmt.Errorf("write progress: %w", err)
	}

	opts := newPatchCallbacks(ctx, emit, patchReq.action, size.total)
	if err := executeResourcePatch(patchReq, opts, size); err != nil {
		slog.Debug("error patching resource", "action", patchReq.action, "source", patchReq.realSrc, "destination", patchReq.realDest, "error", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}

	notifyIndexerAfterPatch(ctx, root, patchReq, size, destExisted)
	return map[string]any{"message": "operation completed"}, nil
}

// generateUniquePath generates a unique path by appending a suffix like " (copy)" or " (copy 2)"
func generateUniquePath(path string, isDir bool, root *fsroot.FSRoot) string {
	dir := filepath.Dir(path)
	base := filepath.Base(path)

	// For files, split name and extension
	var name, ext string
	if !isDir {
		ext = filepath.Ext(base)
		name = strings.TrimSuffix(base, ext)
	} else {
		name = base
	}

	// Try "name (copy).ext" first
	newPath := filepath.Join(dir, name+" (copy)"+ext)
	if _, err := root.Root.Stat(fsroot.ToRel(newPath)); os.IsNotExist(err) {
		return newPath
	}

	// Try "name (copy 2).ext", "name (copy 3).ext", etc.
	for i := 2; i < 1000; i++ {
		newPath = filepath.Join(dir, fmt.Sprintf("%s (copy %d)%s", name, i, ext))
		if _, err := root.Root.Stat(fsroot.ToRel(newPath)); os.IsNotExist(err) {
			return newPath
		}
	}

	// Fallback to timestamp-based name
	timestamp := time.Now().Unix()
	return filepath.Join(dir, fmt.Sprintf("%s (copy %d)%s", name, timestamp, ext))
}

// indexerHTTPClient is a shared HTTP client for communicating with the indexer daemon.
// It uses a Unix socket connection and is reused across all indexer operations.
var indexerHTTPClient = &http.Client{
	Transport: &http.Transport{
		// Dial over the unix domain socket exposed by the indexer systemd service.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout:   2 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return dialer.DialContext(ctx, "unix", "/var/run/indexer.sock")
		},
	},
	Timeout: 10 * time.Second,
}

// indexerEntry represents a file or directory entry for the indexer API
type indexerEntry struct {
	Path    string `json:"path"`
	AbsPath string `json:"absPath"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	IsDir   bool   `json:"isDir"`
	Type    string `json:"type"`
	Hidden  bool   `json:"hidden"`
	ModUnix int64  `json:"modUnix"`
	Inode   uint64 `json:"inode"`
}

// addToIndexer notifies the indexer daemon about a new or updated file/directory.
// This updates the cached directory sizes in the indexer.
func addToIndexer(ctx context.Context, path string, info os.FileInfo) error {
	if info == nil {
		return nil
	}
	return addToIndexerWithSize(ctx, path, info, info.Size())
}

func addToIndexerWithSize(ctx context.Context, path string, info os.FileInfo, size int64) error {
	if !isIndexerEnabled() {
		return nil
	}
	if info == nil {
		return nil
	}
	if !info.IsDir() || size < 0 {
		size = info.Size()
	}

	// Get inode number
	var inode uint64
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		inode = stat.Ino
	}

	entry := indexerEntry{
		Path:    utils.NormalizeIndexerPath(path),
		AbsPath: path,
		Name:    filepath.Base(path),
		Size:    size,
		IsDir:   info.IsDir(),
		Type: func() string {
			if info.IsDir() {
				return "directory"
			}
			return "file"
		}(),
		Hidden:  strings.HasPrefix(filepath.Base(path), "."),
		ModUnix: info.ModTime().Unix(),
		Inode:   inode,
	}

	body, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal indexer entry: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/add", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("failed to build indexer add request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		slog.
			// Log but don't fail the operation if indexer is unavailable
			Debug("indexer add request failed (indexer may be offline)", "error", err)
		setIndexerAvailability(false)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug("indexer add returned non-OK status", "status", resp.Status)
		if resp.StatusCode >= http.StatusInternalServerError {
			setIndexerAvailability(false)
		}
		return nil
	}

	setIndexerAvailability(true)

	// Log successful indexer update
	fileType := "file"
	if entry.IsDir {
		fileType = "directory"
	}
	slog.Info("notified indexer of added/updated entry",
		"path", entry.Path,
		"type", fileType,
		"size", entry.Size)

	return nil
}

func addCopiedPathToIndexer(ctx context.Context, path string, info os.FileInfo, size computedTransferSize, removeExisting bool) error {
	if removeExisting {
		if err := deleteFromIndexer(ctx, path); err != nil {
			slog.Debug("failed to remove overwritten indexer entry", "path", path, "error", err)
		}
	}
	if info == nil {
		return nil
	}
	if err := addToIndexerWithSize(ctx, path, info, indexerEntrySize(info, size)); err != nil {
		return err
	}
	if info.IsDir() {
		if err := requestIndexerReindex(ctx, path); err != nil {
			slog.Debug("failed to request indexer refresh", "path", path, "error", err)
		}
	}
	return nil
}

func movePathInIndexer(ctx context.Context, source, destination string, size computedTransferSize, removeExistingDestination bool, statDestination func() (os.FileInfo, error)) error {
	if err := deleteFromIndexer(ctx, source); err != nil {
		slog.Debug("failed to delete source from indexer after move", "source", source, "error", err)
	}
	if removeExistingDestination {
		if err := deleteFromIndexer(ctx, destination); err != nil {
			slog.Debug("failed to delete overwritten destination from indexer after move", "destination", destination, "error", err)
		}
	}

	info, err := statDestination()
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Debug("failed to stat destination for indexer update", "destination", destination, "error", err)
		}
		return nil
	}
	return addCopiedPathToIndexer(ctx, destination, info, size, false)
}

func requestIndexerReindex(ctx context.Context, path string) error {
	if !isIndexerEnabled() {
		return nil
	}

	query := url.Values{}
	query.Set("path", utils.NormalizeIndexerPath(path))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/reindex?"+query.Encode(), nil)
	if err != nil {
		return fmt.Errorf("failed to build indexer reindex request: %w", err)
	}

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		setIndexerAvailability(false)
		return fmt.Errorf("%w: indexer reindex request failed: %v", errIndexerUnavailable, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusAccepted, http.StatusOK:
		setIndexerAvailability(true)
		return nil
	case http.StatusConflict:
		return nil
	default:
		if resp.StatusCode >= http.StatusInternalServerError {
			setIndexerAvailability(false)
		}
		return fmt.Errorf("indexer reindex returned status %s", resp.Status)
	}
}

// deleteFromIndexer notifies the indexer daemon about a deleted file/directory.
// This updates the cached directory sizes in the indexer.
func deleteFromIndexer(ctx context.Context, path string) error {
	if !isIndexerEnabled() {
		return nil
	}

	normPath := utils.NormalizeIndexerPath(path)
	deleteURL := fmt.Sprintf("http://unix/delete?path=%s", url.QueryEscape(normPath))

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build indexer delete request: %w", err)
	}

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		slog.
			// Log but don't fail the operation if indexer is unavailable
			Debug("indexer delete request failed (indexer may be offline)", "error", err)
		setIndexerAvailability(false)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Debug("indexer delete returned non-OK status", "status", resp.Status)
		if resp.StatusCode >= http.StatusInternalServerError {
			setIndexerAvailability(false)
		}
		return nil
	}

	setIndexerAvailability(true)
	slog.

		// Log successful indexer deletion
		Info("notified indexer of deleted entry", "path", normPath)

	return nil
}

// CheckIndexerAvailability checks whether the indexer API entrypoint is
// available. Newer indexer installs are socket activated, so the socket unit is
// the primary availability signal; the service check remains for older installs.
func CheckIndexerAvailability(ctx context.Context) (bool, error) {
	var socketErr error
	if ok, err := checkIndexerSocketAvailability(ctx); err == nil && ok {
		setIndexerAvailability(true)
		return true, nil
	} else {
		socketErr = err
	}

	var serviceErr error
	if ok, err := checkIndexerServiceAvailability(ctx); err == nil && ok {
		setIndexerAvailability(true)
		return true, nil
	} else {
		serviceErr = err
	}

	setIndexerAvailability(false)

	switch {
	case socketErr != nil && serviceErr != nil:
		return false, fmt.Errorf("%v; %v", socketErr, serviceErr)
	case socketErr != nil:
		return false, socketErr
	case serviceErr != nil:
		return false, serviceErr
	default:
		return false, fmt.Errorf("indexer socket and service are unavailable")
	}
}

func checkIndexerSocketAvailability(ctx context.Context) (bool, error) {
	info, err := getIndexerUnitInfo(ctx, indexerSocketName)
	if err != nil {
		return false, fmt.Errorf("indexer socket unavailable: %w", err)
	}

	activeState, subState, ok := indexerUnitStates(info)
	if !ok {
		return false, fmt.Errorf("indexer socket state unavailable")
	}
	if activeState != "active" {
		return false, indexerUnitStateError("socket", activeState, subState)
	}

	return true, nil
}

func checkIndexerServiceAvailability(ctx context.Context) (bool, error) {
	info, err := getIndexerUnitInfo(ctx, indexerServiceName)
	if err != nil {
		return false, fmt.Errorf("indexer service unavailable: %w", err)
	}

	activeState, subState, ok := indexerUnitStates(info)
	if !ok {
		return false, fmt.Errorf("indexer service state unavailable")
	}
	if activeState != "active" || subState != "running" {
		return false, indexerUnitStateError("service", activeState, subState)
	}

	return true, nil
}

func indexerUnitStates(info map[string]any) (string, string, bool) {
	activeState, ok := info["ActiveState"].(string)
	if !ok || activeState == "" {
		return "", "", false
	}

	subState, subStateOK := info["SubState"].(string)
	if !subStateOK {
		subState = ""
	}

	return activeState, subState, true
}

func indexerUnitStateError(label, activeState, subState string) error {
	if subState != "" {
		return fmt.Errorf("indexer %s not active: %s (%s)", label, activeState, subState)
	}
	return fmt.Errorf("indexer %s not active: %s", label, activeState)
}

type indexerDirSizeResponse struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Bytes int64  `json:"bytes"`
}

type indexerEntryCountResponse struct {
	Path  string `json:"path"`
	Files int64  `json:"files"`
	Dirs  int64  `json:"dirs"`
}

type indexerStatusResponse struct {
	Running      bool   `json:"running"`
	Status       string `json:"status"`
	FilesIndexed int64  `json:"files_indexed"`
	DirsIndexed  int64  `json:"dirs_indexed"`
	TotalSize    int64  `json:"total_size"`
	LastIndexed  string `json:"last_indexed,omitempty"`
	Warning      string `json:"warning,omitempty"`
}

// fetchDirSizeFromIndexer queries the indexer daemon over its Unix socket for a cached directory size.
func fetchDirSizeFromIndexer(ctx context.Context, path string) (int64, error) {
	normPath := utils.NormalizeIndexerPath(path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/dirsize", nil)
	if err != nil {
		return 0, fmt.Errorf("failed to build indexer request: %w", err)
	}
	q := req.URL.Query()
	q.Set("path", normPath)
	req.URL.RawQuery = q.Encode()

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		setIndexerAvailability(false)
		return 0, fmt.Errorf("%w: indexer dirsize request failed: %v", errIndexerUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= http.StatusInternalServerError {
			setIndexerAvailability(false)
			return 0, fmt.Errorf("%w: indexer dirsize returned status %s", errIndexerUnavailable, resp.Status)
		}
		return 0, fmt.Errorf("indexer dirsize returned status %s", resp.Status)
	}

	var payload indexerDirSizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode indexer dirsize response: %w", err)
	}

	setIndexerAvailability(true)

	if payload.Size != 0 {
		return payload.Size, nil
	}
	return payload.Bytes, nil
}

// fetchEntryCountFromIndexer queries the indexer daemon for cached recursive entry counts.
func fetchEntryCountFromIndexer(ctx context.Context, path string) (int64, error) {
	normPath := utils.NormalizeIndexerPath(path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/entrycount", nil)
	if err != nil {
		return 0, fmt.Errorf("failed to build indexer entrycount request: %w", err)
	}
	q := req.URL.Query()
	q.Set("path", normPath)
	req.URL.RawQuery = q.Encode()

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		setIndexerAvailability(false)
		return 0, fmt.Errorf("%w: indexer entrycount request failed: %v", errIndexerUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= http.StatusInternalServerError {
			setIndexerAvailability(false)
			return 0, fmt.Errorf("%w: indexer entrycount returned status %s", errIndexerUnavailable, resp.Status)
		}
		return 0, fmt.Errorf("indexer entrycount returned status %s", resp.Status)
	}

	var payload indexerEntryCountResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode indexer entrycount response: %w", err)
	}

	setIndexerAvailability(true)
	return payload.Files + payload.Dirs, nil
}

func fetchIndexerStatusFromIndexer(ctx context.Context) (indexerStatusResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/status", nil)
	if err != nil {
		return indexerStatusResponse{}, fmt.Errorf("failed to build indexer status request: %w", err)
	}

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		setIndexerAvailability(false)
		return indexerStatusResponse{}, fmt.Errorf("%w: indexer status request failed: %v", errIndexerUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= http.StatusInternalServerError {
			setIndexerAvailability(false)
			return indexerStatusResponse{}, fmt.Errorf("%w: indexer status returned status %s", errIndexerUnavailable, resp.Status)
		}
		return indexerStatusResponse{}, fmt.Errorf("indexer status returned status %s", resp.Status)
	}

	var raw struct {
		Status      string `json:"status"`
		NumDirs     int64  `json:"num_dirs"`
		NumFiles    int64  `json:"num_files"`
		TotalSize   int64  `json:"total_size"`
		LastIndexed string `json:"last_indexed"`
		Warning     string `json:"warning,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return indexerStatusResponse{}, fmt.Errorf("decode indexer status response: %w", err)
	}

	setIndexerAvailability(true)

	status := strings.ToLower(strings.TrimSpace(raw.Status))
	if status == "" {
		status = "unknown"
	}

	return indexerStatusResponse{
		Running:      status == "running",
		Status:       status,
		FilesIndexed: raw.NumFiles,
		DirsIndexed:  raw.NumDirs,
		TotalSize:    raw.TotalSize,
		LastIndexed:  raw.LastIndexed,
		Warning:      raw.Warning,
	}, nil
}

// indexerStatus returns current indexer status for refresh recovery.
func indexerStatus(ctx context.Context) (any, error) {
	status, err := fetchIndexerStatusFromIndexer(ctx)
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return nil, fmt.Errorf("bad_request:indexer unavailable")
		}
		slog.Debug("error fetching indexer status", "error", err)
		return nil, fmt.Errorf("error fetching indexer status: %w", err)
	}

	return status, nil
}

// dirSize calculates the total size of a directory recursively.
func dirSize(ctx context.Context, req apischema.PathRequest) (any, error) {
	if req.Path == "" {
		return nil, fmt.Errorf("bad_request:missing path")
	}

	root, err := fsroot.Open()
	if err != nil {
		return nil, fmt.Errorf("bad_request:failed to access filesystem")
	}
	defer root.Close()

	// Check if path exists and is a directory
	stat, err := root.Root.Stat(fsroot.ToRel(req.Path))
	if err != nil {
		slog.Debug("error stating directory", "path", req.Path, "error", err)
		return nil, fmt.Errorf("bad_request:directory not found")
	}

	if !stat.IsDir() {
		return nil, fmt.Errorf("bad_request:path is not a directory")
	}

	// Get directory size from the indexer daemon (precomputed)
	size, err := fetchDirSizeFromIndexer(ctx, req.Path)
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return nil, fmt.Errorf("bad_request:indexer unavailable")
		}
		slog.Debug("error fetching directory size from indexer", "path", req.Path, "error", err)
		return nil, fmt.Errorf("error fetching directory size: %w", err)
	}

	return map[string]any{
		"path": req.Path,
		"size": size,
	}, nil
}

// subfoldersResponse represents a subfolder entry from the indexer
type subfoldersResponse struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	Bytes   int64  `json:"bytes,omitempty"`
	ModTime string `json:"mod_time"`
}

// subfolders gets direct child folders with their pre-calculated sizes.
func subfolders(ctx context.Context, req apischema.PathRequest) (any, error) {
	path := "/"
	if req.Path != "" {
		path = req.Path
	}

	root, err := fsroot.Open()
	if err != nil {
		return nil, fmt.Errorf("bad_request:failed to access filesystem")
	}
	defer root.Close()

	// Validate path exists and is a directory if not root.
	if path != "/" {
		stat, statErr := root.Root.Stat(fsroot.ToRel(path))
		if statErr != nil {
			slog.Debug("error stating directory", "path", path, "error", statErr)
			return nil, fmt.Errorf("bad_request:directory not found")
		}
		if !stat.IsDir() {
			return nil, fmt.Errorf("bad_request:path is not a directory")
		}
	}

	// Fetch subfolders from indexer (it will handle path validation)
	folders, err := fetchSubfoldersFromIndexer(ctx, path)
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return nil, fmt.Errorf("bad_request:indexer unavailable")
		}
		slog.Debug("error fetching subfolders from indexer", "path", path, "error", err)
		return nil, fmt.Errorf("error fetching subfolders: %w", err)
	}

	return map[string]any{
		"path":       path,
		"subfolders": folders,
		"count":      len(folders),
	}, nil
}

// fetchSubfoldersFromIndexer queries the indexer daemon for direct child folders with sizes
func fetchSubfoldersFromIndexer(ctx context.Context, path string) ([]subfoldersResponse, error) {
	normPath := utils.NormalizeIndexerPath(path)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/subfolders", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build indexer request: %w", err)
	}
	q := req.URL.Query()
	q.Set("path", normPath)
	req.URL.RawQuery = q.Encode()

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		setIndexerAvailability(false)
		return nil, fmt.Errorf("%w: indexer subfolders request failed: %v", errIndexerUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= http.StatusInternalServerError {
			setIndexerAvailability(false)
			return nil, fmt.Errorf("%w: indexer subfolders returned status %s", errIndexerUnavailable, resp.Status)
		}
		return nil, fmt.Errorf("indexer subfolders returned status %s", resp.Status)
	}

	var folders []subfoldersResponse
	if err := json.NewDecoder(resp.Body).Decode(&folders); err != nil {
		return nil, fmt.Errorf("decode indexer subfolders response: %w", err)
	}

	setIndexerAvailability(true)

	for i := range folders {
		if folders[i].Size == 0 && folders[i].Bytes != 0 {
			folders[i].Size = folders[i].Bytes
		}
		folders[i].Bytes = 0
	}

	return folders, nil
}

// searchFiles searches for files/directories in the indexer database.
func searchFiles(ctx context.Context, req apischema.FileSearchRequest) (any, error) {
	if req.Query == "" {
		return nil, fmt.Errorf("bad_request:missing search query")
	}

	if strings.TrimSpace(req.Query) == "" {
		return nil, fmt.Errorf("bad_request:search query cannot be empty")
	}

	limit := "100" // default limit
	if req.Limit != nil && *req.Limit != "" {
		limit = *req.Limit
	}

	basePath := "/" // default to root
	if req.BasePath != nil && *req.BasePath != "" {
		basePath = utils.NormalizeIndexerPath(*req.BasePath)
	}

	results, err := searchInIndexer(ctx, req.Query, limit, basePath)
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return nil, fmt.Errorf("bad_request:indexer unavailable")
		}
		slog.Debug("error searching indexer", "query", req.Query, "base_path", basePath, "error", err)
		return nil, fmt.Errorf("error searching files: %w", err)
	}

	normalizeIndexerSearchResults(results)

	return map[string]any{
		"query":   req.Query,
		"results": results,
		"count":   len(results),
	}, nil
}

func normalizeIndexerSearchResults(results []map[string]any) {
	for _, result := range results {
		path, pathOK := result["path"].(string)
		if !pathOK {
			path = ""
		}
		typeRaw, typeOk := result["type"].(string)
		normalizedType := strings.ToLower(typeRaw)

		isDir, isDirOk := result["isDir"].(bool)

		derivedIsDir := false
		switch normalizedType {
		case "directory", "dir", "folder":
			derivedIsDir = true
		case "file":
			derivedIsDir = false
		default:
			if isDirOk {
				derivedIsDir = isDir
			} else if strings.HasSuffix(path, "/") {
				derivedIsDir = true
			}
		}

		if !isDirOk {
			result["isDir"] = derivedIsDir
		}

		if derivedIsDir {
			result["type"] = "directory"
			continue
		}

		if !typeOk || normalizedType == "" || normalizedType == "file" || normalizedType == "directory" || normalizedType == "dir" || normalizedType == "folder" {
			result["type"] = "file"
		}
	}
}

// searchInIndexer queries the indexer for files matching the search term
func searchInIndexer(ctx context.Context, query, limit, basePath string) ([]map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unix/search", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build indexer search request: %w", err)
	}

	q := req.URL.Query()
	q.Set("q", query)
	q.Set("limit", limit)
	if basePath != "/" {
		q.Set("base", basePath)
	}
	req.URL.RawQuery = q.Encode()

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		setIndexerAvailability(false)
		return nil, fmt.Errorf("%w: indexer search request failed: %v", errIndexerUnavailable, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode >= http.StatusInternalServerError {
			setIndexerAvailability(false)
			return nil, fmt.Errorf("%w: indexer search returned status %s", errIndexerUnavailable, resp.Status)
		}
		return nil, fmt.Errorf("indexer search returned status %s", resp.Status)
	}

	// Indexer returns array directly, not wrapped in object
	var results []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&results); err != nil {
		return nil, fmt.Errorf("decode indexer search response: %w", err)
	}

	setIndexerAvailability(true)

	return results, nil
}

func defaultExtractDestination(archivePath string) string {
	baseDir := filepath.Dir(archivePath)
	baseName := filepath.Base(archivePath)
	lowerName := strings.ToLower(baseName)

	switch {
	case strings.HasSuffix(lowerName, ".tar.gz"):
		baseName = strings.TrimSuffix(baseName, ".tar.gz")
	case strings.HasSuffix(lowerName, ".tgz"):
		baseName = strings.TrimSuffix(baseName, ".tgz")
	default:
		baseName = strings.TrimSuffix(baseName, filepath.Ext(baseName))
	}

	if baseName == "" || baseName == "/" || baseName == "." {
		baseName = "extracted"
	}

	return filepath.Join(baseDir, baseName)
}

func resolveUserID(identifier string) (int, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return -1, nil
	}

	if u, err := user.Lookup(trimmed); err == nil {
		return strconv.Atoi(u.Uid)
	}

	if u, err := user.LookupId(trimmed); err == nil {
		return strconv.Atoi(u.Uid)
	}

	if id, err := strconv.Atoi(trimmed); err == nil {
		return id, nil
	}

	return -1, fmt.Errorf("unknown user: %s", trimmed)
}

func resolveGroupID(identifier string) (int, error) {
	trimmed := strings.TrimSpace(identifier)
	if trimmed == "" {
		return -1, nil
	}

	if g, err := user.LookupGroup(trimmed); err == nil {
		return strconv.Atoi(g.Gid)
	}

	if g, err := user.LookupGroupId(trimmed); err == nil {
		return strconv.Atoi(g.Gid)
	}

	if id, err := strconv.Atoi(trimmed); err == nil {
		return id, nil
	}

	return -1, fmt.Errorf("unknown group: %s", trimmed)
}

// usersGroups returns lists of all users and groups on the system
// Args: []
func usersGroups(ctx context.Context) (any, error) {
	users, err := getAllUsers(ctx)
	if err != nil {
		slog.Debug("error getting users", "error", err)
		return nil, fmt.Errorf("error getting users: %w", err)
	}

	groups, err := getAllGroups(ctx)
	if err != nil {
		slog.Debug("error getting groups", "error", err)
		return nil, fmt.Errorf("error getting groups: %w", err)
	}

	return map[string]any{
		"users":  users,
		"groups": groups,
	}, nil
}

func getAllUsers(ctx context.Context) ([]string, error) {
	content, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return nil, err
	}

	users := []string{}
	lines := strings.SplitSeq(string(content), "\n")
	for line := range lines {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Format: username:password:UID:GID:GECOS:home:shell
		parts := strings.Split(line, ":")
		if len(parts) > 0 {
			username := strings.TrimSpace(parts[0])
			if username != "" {
				users = append(users, username)
			}
		}
	}

	return users, nil
}

func getAllGroups(ctx context.Context) ([]string, error) {
	content, err := os.ReadFile("/etc/group")
	if err != nil {
		return nil, err
	}

	groups := []string{}
	lines := strings.SplitSeq(string(content), "\n")
	for line := range lines {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Format: groupname:password:GID:user_list
		parts := strings.Split(line, ":")
		if len(parts) > 0 {
			groupname := strings.TrimSpace(parts[0])
			if groupname != "" {
				groups = append(groups, groupname)
			}
		}
	}

	return groups, nil
}

// NOTE: fileUploadFromTemp, fileUpdateFromTemp, fileDownloadToTemp, archiveDownloadSetup removed.
// These operations now use durable jobs plus built-in jobs.data streams.
