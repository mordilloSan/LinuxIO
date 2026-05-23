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

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
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

// resourceGet retrieves information about a resource
// Args: [path, "", getContent?] or [path]
func resourceGet(ctx context.Context, args []string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) < 1 {
		return nil, fmt.Errorf("bad_request:missing path")
	}

	path := args[0]
	getContent := len(args) > 2 && args[2] == "true"

	fileInfo, err := services.FileInfoFaster(iteminfo.FileOptions{
		Path:    path,
		Expand:  true,
		Content: getContent,
	})
	if err != nil {
		slog.Debug("error getting file info", "path", path, "error", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}

	return fileInfo, nil
}

// resourceStat returns extended metadata
// Args: [path]
func resourceStat(ctx context.Context, args []string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(args) < 1 {
		return nil, fmt.Errorf("bad_request:missing path")
	}

	path := args[0]

	fileInfo, err := services.FileInfoFaster(iteminfo.FileOptions{
		Path:   path,
		Expand: false,
	})
	if err != nil {
		slog.Debug("error getting file stat info", "path", path, "error", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}

	statData, err := iteminfo.CollectStatInfo(fileInfo.RealPath)
	if err != nil {
		slog.Debug("error collecting stat info", "path", fileInfo.RealPath, "error", err)
		return nil, fmt.Errorf("error collecting stat info: %w", err)
	}

	statData.Path = path
	statData.Name = fileInfo.Name
	if statData.Size == 0 {
		statData.Size = fileInfo.Size
	}

	return statData, nil
}

// resourceDelete deletes a resource
// Args: [path]
func resourceDelete(ctx context.Context, args []string, emit bridgeipc.Events) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("bad_request:missing path")
	}

	path := args[0]

	if path == "/" {
		return nil, fmt.Errorf("bad_request:cannot delete root")
	}

	isDir, err := deleteTargetIsDir(path)
	if err != nil {
		slog.Debug("error getting file info", "path", path, "error", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}

	deleteOpts := deleteOptionsForPath(ctx, path, isDir)
	reportDeleteProgress(emit, 0, deleteOpts.Total, deleteOpts.Indeterminate, "preparing")
	deleteOpts.Progress = newDeleteProgressReporter(emit)

	processed, err := services.DeleteFilesWithProgress(ctx, path, deleteOpts)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
		slog.Debug("error deleting file", "path", path, "error", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}

	// Notify indexer about the deletion
	if err := deleteFromIndexer(ctx, path); err != nil {
		slog.Debug("failed to update indexer after delete", "path", path, "error", err)
		// Don't fail the operation if indexer update fails
	}
	slog.Info("delete complete", "path", path)

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

	cleanPath := filepath.Clean("/" + strings.TrimPrefix(path, "/"))
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

func parseResourcePostArgs(args []string) (resourcePostRequest, error) {
	if len(args) < 1 {
		return resourcePostRequest{}, fmt.Errorf("bad_request:missing path")
	}

	path, err := url.QueryUnescape(args[0])
	if err != nil {
		return resourcePostRequest{}, fmt.Errorf("bad_request:invalid path encoding")
	}

	cleanPath := filepath.Clean("/" + strings.TrimPrefix(path, "/"))
	if cleanPath == "/" {
		return resourcePostRequest{}, fmt.Errorf("bad_request:cannot create root")
	}

	return resourcePostRequest{
		cleanPath: cleanPath,
		relPath:   strings.TrimPrefix(cleanPath, "/"),
		isDir:     strings.HasSuffix(path, "/"),
		override:  len(args) > 1 && args[1] == "true",
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

func parseResourcePatchArgs(args []string) (resourcePatchRequest, error) {
	if len(args) < 3 {
		return resourcePatchRequest{}, fmt.Errorf("bad_request:missing action, from, or destination")
	}

	src, err := url.QueryUnescape(args[1])
	if err != nil {
		return resourcePatchRequest{}, fmt.Errorf("bad_request:invalid source path encoding")
	}
	dst, err := url.QueryUnescape(args[2])
	if err != nil {
		return resourcePatchRequest{}, fmt.Errorf("bad_request:invalid destination path encoding")
	}
	if dst == "/" || src == "/" {
		return resourcePatchRequest{}, fmt.Errorf("bad_request:cannot modify root directory")
	}

	return resourcePatchRequest{
		action:    args[0],
		src:       src,
		dst:       dst,
		overwrite: len(args) > 3 && args[3] == "true",
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
	req.realSrc = filepath.Clean("/" + strings.TrimPrefix(req.src, "/"))

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

func computePatchSize(realSrc string) int64 {
	totalSize, err := services.ComputeCopySize(realSrc)
	if err != nil {
		slog.Debug("failed to compute filebrowser operation size", "path", realSrc, "error", err)
		return 0
	}
	return totalSize
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

func executeResourcePatch(req resourcePatchRequest, opts *ipc.OperationCallbacks) error {
	switch req.action {
	case "copy":
		return services.CopyFileWithCallbacks(req.realSrc, req.realDest, req.overwrite, opts)
	case "rename", "move":
		return services.MoveFileWithCallbacks(req.realSrc, req.realDest, req.overwrite, opts)
	default:
		return fmt.Errorf("bad_request:unsupported action: %s", req.action)
	}
}

func notifyIndexerAfterPatch(ctx context.Context, root *fsroot.FSRoot, req resourcePatchRequest) {
	switch req.action {
	case "copy":
		if info, err := root.Root.Stat(fsroot.ToRel(req.realDest)); err == nil {
			if err := addToIndexer(ctx, req.dst, info); err != nil {
				slog.Debug("failed to update indexer after copy", "path", req.dst, "error", err)
			}
		}
	case "rename", "move":
		if err := deleteFromIndexer(ctx, req.src); err != nil {
			slog.Debug("failed to update indexer after move delete", "path", req.src, "error", err)
		}
		if info, err := root.Root.Stat(fsroot.ToRel(req.realDest)); err == nil {
			if err := addToIndexer(ctx, req.dst, info); err != nil {
				slog.Debug("failed to update indexer after move add", "path", req.dst, "error", err)
			}
		}
	}
}

// resourcePost creates or uploads a new resource
// Args: [path, override?, chunkOffset?, totalSize?, body]
func resourcePost(ctx context.Context, args []string) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	req, err := parseResourcePostArgs(args)
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

	if err := ensureResourcePostType(root, req); err != nil {
		return nil, err
	}

	if req.isDir {
		return createDirectoryResource(ctx, root, req)
	}
	return createFileResource(ctx, root, req)
}

// resourcePatchWithProgress performs patch operations with progress feedback
// Args: [action, from, destination, overwrite?]
func resourcePatchWithProgress(ctx context.Context, args []string, emit bridgeipc.Events) (any, error) {
	req, err := parseResourcePatchArgs(args)
	if err != nil {
		return nil, err
	}

	root, err := fsroot.Open()
	if err != nil {
		return nil, fmt.Errorf("bad_request:failed to access filesystem")
	}
	defer root.Close()

	req, err = prepareResourcePatch(root, req)
	if err != nil {
		return nil, err
	}

	totalSize := computePatchSize(req.realSrc)
	// Send initial progress.
	slog.Info("starting filebrowser operation",
		"action", req.action,
		"source", req.realSrc,
		"destination", req.realDest,
		"size", totalSize)
	if err := emit.Progress(FileProgress{
		Total: totalSize,
		Phase: "preparing",
	}); err != nil {
		return nil, fmt.Errorf("write progress: %w", err)
	}

	opts := newPatchCallbacks(ctx, emit, req.action, totalSize)
	if err := executeResourcePatch(req, opts); err != nil {
		slog.Debug("error patching resource", "action", req.action, "source", req.realSrc, "destination", req.realDest, "error", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}

	notifyIndexerAfterPatch(ctx, root, req)
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

func normalizeIndexerPath(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	// Strip trailing slashes - indexer is sensitive to them
	normalized := strings.TrimRight(path, "/")
	// Ensure leading slash
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	return normalized
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
	if !isIndexerEnabled() {
		return nil
	}

	// Get inode number
	var inode uint64
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		inode = stat.Ino
	}

	entry := indexerEntry{
		Path:    normalizeIndexerPath(path),
		AbsPath: path,
		Name:    filepath.Base(path),
		Size:    info.Size(),
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

// deleteFromIndexer notifies the indexer daemon about a deleted file/directory.
// This updates the cached directory sizes in the indexer.
func deleteFromIndexer(ctx context.Context, path string) error {
	if !isIndexerEnabled() {
		return nil
	}

	normPath := normalizeIndexerPath(path)
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
	normPath := normalizeIndexerPath(path)

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
	normPath := normalizeIndexerPath(path)

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
// Args: []
func indexerStatus(ctx context.Context, args []string) (any, error) {
	if len(args) > 0 {
		return nil, fmt.Errorf("bad_request:unexpected arguments")
	}

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

// dirSize calculates the total size of a directory recursively
// Args: [path]
func dirSize(ctx context.Context, args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("bad_request:missing path")
	}

	path := args[0]

	root, err := fsroot.Open()
	if err != nil {
		return nil, fmt.Errorf("bad_request:failed to access filesystem")
	}
	defer root.Close()

	// Check if path exists and is a directory
	stat, err := root.Root.Stat(fsroot.ToRel(path))
	if err != nil {
		slog.Debug("error stating directory", "path", path, "error", err)
		return nil, fmt.Errorf("bad_request:directory not found")
	}

	if !stat.IsDir() {
		return nil, fmt.Errorf("bad_request:path is not a directory")
	}

	// Get directory size from the indexer daemon (precomputed)
	size, err := fetchDirSizeFromIndexer(ctx, path)
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return nil, fmt.Errorf("bad_request:indexer unavailable")
		}
		slog.Debug("error fetching directory size from indexer", "path", path, "error", err)
		return nil, fmt.Errorf("error fetching directory size: %w", err)
	}

	return map[string]any{
		"path": path,
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

// subfolders gets direct child folders with their pre-calculated sizes
// Args: [path]
func subfolders(ctx context.Context, args []string) (any, error) {
	path := "/"
	if len(args) > 0 && args[0] != "" {
		path = args[0]
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
	normPath := normalizeIndexerPath(path)

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

// searchFiles searches for files/directories in the indexer database
// Args: [query, limit?, basePath?]
func searchFiles(ctx context.Context, args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("bad_request:missing search query")
	}

	query := args[0]
	if strings.TrimSpace(query) == "" {
		return nil, fmt.Errorf("bad_request:search query cannot be empty")
	}

	limit := "100" // default limit
	if len(args) > 1 && args[1] != "" {
		limit = args[1]
	}

	basePath := "/" // default to root
	if len(args) > 2 && args[2] != "" {
		basePath = normalizeIndexerPath(args[2])
	}

	results, err := searchInIndexer(ctx, query, limit, basePath)
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return nil, fmt.Errorf("bad_request:indexer unavailable")
		}
		slog.Debug("error searching indexer", "query", query, "base_path", basePath, "error", err)
		return nil, fmt.Errorf("error searching files: %w", err)
	}

	normalizeIndexerSearchResults(results)

	return map[string]any{
		"query":   query,
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
