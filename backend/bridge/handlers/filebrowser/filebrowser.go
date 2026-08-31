package filebrowser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	indexer "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/indexer"
	systemdapi "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/systemd"
	bridgeipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/bridge"
	ipc "github.com/mordilloSan/LinuxIO/backend/common/ipc/relay"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
	indexerapi "github.com/mordilloSan/LinuxIO/backend/indexer/api"
)

var (
	detachedIndexerUpdates sync.WaitGroup
	indexerAvailable       atomic.Bool
	errIndexerUnavailable  = errors.New("indexer unavailable")
)

const (
	indexerServiceName = "linuxio-indexer.service"
	indexerSocketName  = "linuxio-indexer.socket"
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
// that should outlive the request/task which already completed the filesystem change.
func runDetachedIndexerUpdate(label string, fn func(context.Context) error) {
	detachedIndexerUpdates.Go(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := fn(ctx); err != nil {
			slog.Debug("detached indexer update failed", "operation", label, "error", err)
		}
	})
}

func listDirectory(ctx context.Context, req apischema.PathRequest) (apischema.DirectoryListing, error) {
	if err := ctx.Err(); err != nil {
		return apischema.DirectoryListing{}, err
	}
	if req.Path == "" {
		return apischema.DirectoryListing{}, fmt.Errorf("bad_request:missing path")
	}

	listing, err := services.ListDirectory(ctx, req.Path)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return apischema.DirectoryListing{}, err
		}
		return apischema.DirectoryListing{}, fmt.Errorf("bad_request:%v", err)
	}
	if err := ctx.Err(); err != nil {
		return apischema.DirectoryListing{}, err
	}
	return directoryListingResponse(listing), nil
}

func directoryChildren(ctx context.Context, req apischema.DirectoryChildrenRequest) (apischema.DirectoryChildren, error) {
	if err := ctx.Err(); err != nil {
		return apischema.DirectoryChildren{}, err
	}
	if req.Path == "" {
		return apischema.DirectoryChildren{}, fmt.Errorf("bad_request:missing path")
	}
	children, err := services.ListDirectoryChildren(ctx, req.Path, req.IncludeFiles)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return apischema.DirectoryChildren{}, err
		}
		return apischema.DirectoryChildren{}, fmt.Errorf("bad_request:%v", err)
	}
	if err := ctx.Err(); err != nil {
		return apischema.DirectoryChildren{}, err
	}
	return apischema.DirectoryChildren{Folders: children.Folders, Files: children.Files}, nil
}

func readText(ctx context.Context, req apischema.PathRequest) (apischema.TextFile, error) {
	if err := ctx.Err(); err != nil {
		return apischema.TextFile{}, err
	}
	if req.Path == "" {
		return apischema.TextFile{}, fmt.Errorf("bad_request:missing path")
	}
	file, err := services.ReadEditorFile(ctx, req.Path)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return apischema.TextFile{}, err
		}
		return apischema.TextFile{}, fmt.Errorf("bad_request:%v", err)
	}
	return apischema.TextFile{Content: string(file.Content), Version: file.Version, CanSave: file.CanSave}, nil
}

func directoryListingResponse(listing iteminfo.DirectoryListing) apischema.DirectoryListing {
	response := apischema.DirectoryListing{
		Folders: make([]apischema.DirectoryListingFolder, 0, len(listing.Folders)),
		Files:   make([]apischema.DirectoryListingFile, 0, len(listing.Files)),
	}
	for _, folder := range listing.Folders {
		response.Folders = append(response.Folders, apischema.DirectoryListingFolder{
			Name: folder.Name, Modified: formatResourceModTime(folder.ModTime), Symlink: folder.Symlink,
		})
	}
	for _, file := range listing.Files {
		response.Files = append(response.Files, apischema.DirectoryListingFile{
			Name: file.Name, Size: file.Size, Modified: formatResourceModTime(file.ModTime),
			Symlink: file.Symlink, IsRegularFile: file.IsRegularFile, CanOpenAsText: file.CanOpenAsText,
		})
	}
	return response
}

func formatResourceModTime(modTime time.Time) string {
	if modTime.IsZero() {
		return ""
	}
	return modTime.Format(time.RFC3339Nano)
}

// resourceStat returns direct permission metadata without enumerating siblings.
func resourceStat(ctx context.Context, req apischema.PathRequest) (*apischema.ResourceStatData, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.Path == "" {
		return nil, bridgeipc.NewError("missing path", 400)
	}

	statData, err := iteminfo.CollectStatInfo(ctx, req.Path)
	if err != nil {
		slog.Debug("error collecting stat info", "path", req.Path, "error", err)
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, bridgeipc.NewError("path not found", 404)
		}
		return nil, fmt.Errorf("error collecting stat info: %w", err)
	}

	return statData, nil
}

// existsBatch reports which of the requested paths already exist. Used as a
// pre-flight check so transfers can ask the user about collisions before any
// bytes move.
func existsBatch(ctx context.Context, req apischema.BatchPathRequest) (apischema.ExistsBatchResponse, error) {
	response := apischema.ExistsBatchResponse{Existing: []apischema.ExistsBatchItem{}}
	if len(req.Paths) == 0 {
		return response, nil
	}

	root, err := fsroot.Open()
	if err != nil {
		slog.Debug("error opening filesystem root", "error", err)
		return response, fmt.Errorf("bad_request:failed to access filesystem")
	}
	defer root.Close()

	for _, raw := range req.Paths {
		if err := ctx.Err(); err != nil {
			return response, err
		}
		path := utils.CleanAbsPath(raw)
		info, statErr := root.Root.Lstat(fsroot.ToRel(path))
		if statErr != nil {
			continue
		}
		response.Existing = append(response.Existing, apischema.ExistsBatchItem{
			Path:  path,
			IsDir: info.IsDir(),
		})
	}
	return response, nil
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

// deleteEntryTotalForPath resolves how many entries deleting path will remove,
// so the batch task can aggregate a grand total before deletion starts. It
// returns 0 when the total is unknown (indexer miss and the directory is too
// large to prescan cheaply).
func deleteEntryTotalForPath(ctx context.Context, path string, isDir bool) int64 {
	if !isDir {
		return 1
	}

	if counts, err := fetchEntryCountsFromIndexer(ctx, path); err == nil {
		total := counts.Files + counts.Dirs
		if total > 0 {
			return total
		}
		return prescanDeleteEntryTotal(ctx, path, shouldPrescanDeletePath(path))
	} else {
		slog.Debug("failed to get delete entry count from indexer", "path", path, "error", err)
	}

	if size, err := fetchDirSizeFromIndexer(ctx, path); err == nil {
		if size > deleteLocalPrescanMaxBytes {
			return 0
		}
		if size > 0 {
			return prescanDeleteEntryTotal(ctx, path, true)
		}
	} else {
		slog.Debug("failed to get delete directory size from indexer", "path", path, "error", err)
	}

	return prescanDeleteEntryTotal(ctx, path, shouldPrescanDeletePath(path))
}

func prescanDeleteEntryTotal(ctx context.Context, path string, allowed bool) int64 {
	if !allowed {
		return 0
	}
	total, err := services.CountEntries(ctx, path, true)
	if err != nil {
		slog.Debug("failed to prescan delete directory entries", "path", path, "error", err)
		return 0
	}
	return total
}

func shouldPrescanDeletePath(path string) bool {
	ok, err := services.TopLevelEntryCountWithin(path, deleteLocalPrescanMaxTopLevelEntries)
	if err != nil {
		slog.Debug("failed to inspect delete directory top-level entries", "path", path, "error", err)
		return false
	}
	return ok
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

	srcInfo, err := root.Root.Lstat(fsroot.ToRel(req.realSrc))
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
	destInfo, err := root.Root.Lstat(fsroot.ToRel(req.realDest))
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
	if info != nil && info.Mode()&os.ModeSymlink != 0 {
		return computedTransferSize{total: 0, known: true}
	}

	if info != nil && !info.IsDir() {
		if !info.Mode().IsRegular() {
			return computedTransferSize{total: 0, known: true}
		}
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
	counts, err := fetchEntryCountsFromIndexer(ctx, path)
	if err != nil {
		slog.Debug("failed to confirm indexed transfer path", "path", path, "error", err)
		return false
	}
	return counts.Files+counts.Dirs > 0
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
		if info, err := root.Root.Lstat(fsroot.ToRel(req.realDest)); err == nil {
			if err := addCopiedPathToIndexer(ctx, req.realDest, info, size, destExisted && req.overwrite); err != nil {
				slog.Debug("failed to update indexer after copy", "path", req.realDest, "error", err)
			}
		}
	case "rename", "move":
		if err := movePathInIndexer(ctx, req.realSrc, req.realDest, size, destExisted && req.overwrite, func() (os.FileInfo, error) {
			return root.Root.Lstat(fsroot.ToRel(req.realDest))
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
func resourcePatchWithProgress(ctx context.Context, req apischema.ActionSourceDestinationRequest, task *bridgeipc.Task) (apischema.MessageResponse, error) {
	patchReq, err := parseResourcePatchRequest(req)
	if err != nil {
		return apischema.MessageResponse{}, err
	}

	root, err := fsroot.Open()
	if err != nil {
		return apischema.MessageResponse{}, fmt.Errorf("bad_request:failed to access filesystem")
	}
	defer root.Close()

	patchReq, err = prepareResourcePatch(root, patchReq)
	if err != nil {
		return apischema.MessageResponse{}, err
	}

	srcInfo, err := root.Root.Lstat(fsroot.ToRel(patchReq.realSrc))
	if err != nil {
		slog.Debug("error getting source info", "path", patchReq.realSrc, "error", err)
		return apischema.MessageResponse{}, fmt.Errorf("bad_request:source not found")
	}
	_, destStatErr := root.Root.Lstat(fsroot.ToRel(patchReq.realDest))
	destExisted := destStatErr == nil

	size := computeTransferSize(ctx, patchReq.realSrc, srcInfo)
	// Send initial progress.
	slog.Info("starting filebrowser operation",
		"action", patchReq.action,
		"source", patchReq.realSrc,
		"destination", patchReq.realDest,
		"size", size.total)
	task.ReportProgress(FileProgress{
		Total: size.total,
		Phase: "preparing",
	})

	opts := newPatchTaskCallbacks(ctx, task, patchReq.action, size.total)
	if err := executeResourcePatch(patchReq, opts, size); err != nil {
		slog.Debug("error patching resource", "action", patchReq.action, "source", patchReq.realSrc, "destination", patchReq.realDest, "error", err)
		return apischema.MessageResponse{}, fmt.Errorf("bad_request:%v", err)
	}

	notifyIndexerAfterPatch(ctx, root, patchReq, size, destExisted)
	return apischema.MessageResponse{Message: "operation completed"}, nil
}

func newPatchTaskCallbacks(ctx context.Context, task *bridgeipc.Task, action string, totalSize int64) *ipc.OperationCallbacks {
	var bytesProcessed, lastProgress int64
	return &ipc.OperationCallbacks{Progress: func(n int64) {
		bytesProcessed += n
		if totalSize <= 0 || (bytesProcessed-lastProgress < 2*1024*1024 && bytesProcessed < totalSize) {
			return
		}
		phase := "copying"
		if action == "move" || action == "rename" {
			phase = "moving"
		}
		task.ReportProgress(FileProgress{Bytes: bytesProcessed, Total: totalSize, Pct: min(int(bytesProcessed*100/totalSize), 100), Phase: phase})
		lastProgress = bytesProcessed
	}, Cancel: func() bool { return ctx.Err() != nil }}
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
	if _, err := root.Root.Lstat(fsroot.ToRel(newPath)); os.IsNotExist(err) {
		return newPath
	}

	// Try "name (copy 2).ext", "name (copy 3).ext", etc.
	for i := 2; i < 1000; i++ {
		newPath = filepath.Join(dir, fmt.Sprintf("%s (copy %d)%s", name, i, ext))
		if _, err := root.Root.Lstat(fsroot.ToRel(newPath)); os.IsNotExist(err) {
			return newPath
		}
	}

	// Fallback to timestamp-based name
	timestamp := time.Now().Unix()
	return filepath.Join(dir, fmt.Sprintf("%s (copy %d)%s", name, timestamp, ext))
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

	err := indexer.Add(ctx, indexer.EntryFromFileInfo(path, info, size))
	if err != nil {
		slog.Debug("indexer add request failed (indexer may be offline)", "error", err)
		if errors.Is(err, indexer.ErrUnavailable) {
			setIndexerAvailability(false)
			return fmt.Errorf("%w: indexer add request failed: %v", errIndexerUnavailable, err)
		}
		return fmt.Errorf("indexer add request failed: %w", err)
	}
	setIndexerAvailability(true)
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
	err := indexer.Reindex(ctx, path)
	if err != nil {
		if errors.Is(err, indexer.ErrUnavailable) {
			setIndexerAvailability(false)
			return fmt.Errorf("%w: indexer reindex request failed: %v", errIndexerUnavailable, err)
		}
		return fmt.Errorf("indexer reindex request failed: %w", err)
	}
	setIndexerAvailability(true)
	return nil
}

// deleteFromIndexer notifies the indexer daemon about a deleted file/directory.
// This updates the cached directory sizes in the indexer.
func deleteFromIndexer(ctx context.Context, path string) error {
	if !isIndexerEnabled() {
		return nil
	}
	err := indexer.Delete(ctx, path)
	if err != nil {
		slog.Debug("indexer delete request failed (indexer may be offline)", "error", err)
		if errors.Is(err, indexer.ErrUnavailable) {
			setIndexerAvailability(false)
			return fmt.Errorf("%w: indexer delete request failed: %v", errIndexerUnavailable, err)
		}
		return fmt.Errorf("indexer delete request failed: %w", err)
	}
	setIndexerAvailability(true)
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

func indexerUnitStates(info apischema.UnitInfo) (string, string, bool) {
	if info.ActiveState == nil || *info.ActiveState == "" {
		return "", "", false
	}

	var subState string
	if info.SubState != nil {
		subState = *info.SubState
	}

	return *info.ActiveState, subState, true
}

func indexerUnitStateError(label, activeState, subState string) error {
	if subState != "" {
		return fmt.Errorf("indexer %s not active: %s (%s)", label, activeState, subState)
	}
	return fmt.Errorf("indexer %s not active: %s", label, activeState)
}

type indexerEntryCountResponse = indexerapi.EntryCountResponse

type indexerStatusResponse struct {
	Running      bool   `json:"running"`
	Status       string `json:"status"`
	FTSActive    bool   `json:"fts_active"`
	FilesIndexed int64  `json:"files_indexed"`
	DirsIndexed  int64  `json:"dirs_indexed"`
	TotalSize    int64  `json:"total_size"`
	LastIndexed  string `json:"last_indexed,omitempty"`
	Warning      string `json:"warning,omitempty"`
}

// fetchDirSizeFromIndexer queries the indexer daemon over its Unix socket for a cached directory size.
func fetchDirSizeFromIndexer(ctx context.Context, path string) (int64, error) {
	payload, err := indexer.DirSize(ctx, path)
	if err != nil {
		if errors.Is(err, indexer.ErrUnavailable) {
			setIndexerAvailability(false)
			return 0, fmt.Errorf("%w: indexer dirsize request failed: %v", errIndexerUnavailable, err)
		}
		return 0, err
	}
	setIndexerAvailability(true)
	return payload.Size, nil
}

// fetchEntryCountsFromIndexer queries the indexer daemon for cached recursive entry counts.
func fetchEntryCountsFromIndexer(ctx context.Context, path string) (indexerEntryCountResponse, error) {
	payload, err := indexer.EntryCount(ctx, path)
	if err != nil {
		if errors.Is(err, indexer.ErrUnavailable) {
			setIndexerAvailability(false)
			return indexerEntryCountResponse{}, fmt.Errorf("%w: indexer entrycount request failed: %v", errIndexerUnavailable, err)
		}
		return indexerEntryCountResponse{}, err
	}
	setIndexerAvailability(true)
	return payload, nil
}

func fetchIndexerStatusFromIndexer(ctx context.Context) (indexerStatusResponse, error) {
	raw, err := indexer.FetchStatus(ctx)
	if err != nil {
		if errors.Is(err, indexer.ErrUnavailable) {
			setIndexerAvailability(false)
			return indexerStatusResponse{}, fmt.Errorf("%w: indexer status request failed: %v", errIndexerUnavailable, err)
		}
		return indexerStatusResponse{}, err
	}
	setIndexerAvailability(true)
	return indexerStatusResponse{
		Running:      raw.Running,
		Status:       raw.Status,
		FTSActive:    raw.FTSActive,
		FilesIndexed: raw.NumFiles,
		DirsIndexed:  raw.NumDirs,
		TotalSize:    raw.TotalSize,
		LastIndexed:  raw.LastIndexed,
		Warning:      raw.Warning,
	}, nil
}

// indexerStatus returns current indexer status for refresh recovery.
func indexerStatus(ctx context.Context) (indexerStatusResponse, error) {
	status, err := fetchIndexerStatusFromIndexer(ctx)
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return indexerStatusResponse{}, fmt.Errorf("bad_request:indexer unavailable")
		}
		slog.Debug("error fetching indexer status", "error", err)
		return indexerStatusResponse{}, fmt.Errorf("error fetching indexer status: %w", err)
	}

	return status, nil
}

// dirSize calculates the total size of a directory recursively.
func dirSize(ctx context.Context, req apischema.PathRequest) (apischema.DirectorySizeData, error) {
	if req.Path == "" {
		return apischema.DirectorySizeData{}, fmt.Errorf("bad_request:missing path")
	}

	root, err := fsroot.Open()
	if err != nil {
		return apischema.DirectorySizeData{}, fmt.Errorf("bad_request:failed to access filesystem")
	}
	defer root.Close()

	// Check if path exists and is a directory
	stat, err := root.Root.Stat(fsroot.ToRel(req.Path))
	if err != nil {
		slog.Debug("error stating directory", "path", req.Path, "error", err)
		return apischema.DirectorySizeData{}, fmt.Errorf("bad_request:directory not found")
	}

	if !stat.IsDir() {
		return apischema.DirectorySizeData{}, fmt.Errorf("bad_request:path is not a directory")
	}

	var size int64
	var counts indexerEntryCountResponse
	var group errgroup.Group
	group.Go(func() error {
		var fetchErr error
		size, fetchErr = fetchDirSizeFromIndexer(ctx, req.Path)
		return fetchErr
	})
	group.Go(func() error {
		var fetchErr error
		counts, fetchErr = fetchEntryCountsFromIndexer(ctx, req.Path)
		return fetchErr
	})
	err = group.Wait()
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return apischema.DirectorySizeData{}, fmt.Errorf("bad_request:indexer unavailable")
		}
		slog.Debug("error fetching directory details from indexer", "path", req.Path, "error", err)
		return apischema.DirectorySizeData{}, fmt.Errorf("error fetching directory details: %w", err)
	}

	return apischema.DirectorySizeData{
		Size:        size,
		FileCount:   counts.Files,
		FolderCount: counts.Dirs,
	}, nil
}

type indexerSubfolder = indexerapi.SubfolderResult

// subfolders gets direct child folders with their pre-calculated sizes.
func subfolders(ctx context.Context, req apischema.PathRequest) (apischema.SubfoldersResponse, error) {
	path := "/"
	if req.Path != "" {
		path = req.Path
	}

	root, err := fsroot.Open()
	if err != nil {
		return apischema.SubfoldersResponse{}, fmt.Errorf("bad_request:failed to access filesystem")
	}
	defer root.Close()

	// Validate path exists and is a directory if not root.
	if path != "/" {
		stat, statErr := root.Root.Stat(fsroot.ToRel(path))
		if statErr != nil {
			slog.Debug("error stating directory", "path", path, "error", statErr)
			return apischema.SubfoldersResponse{}, fmt.Errorf("bad_request:directory not found")
		}
		if !stat.IsDir() {
			return apischema.SubfoldersResponse{}, fmt.Errorf("bad_request:path is not a directory")
		}
	}

	// Fetch subfolders from indexer (it will handle path validation)
	folders, err := fetchSubfoldersFromIndexer(ctx, path)
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return apischema.SubfoldersResponse{}, fmt.Errorf("bad_request:indexer unavailable")
		}
		slog.Debug("error fetching subfolders from indexer", "path", path, "error", err)
		return apischema.SubfoldersResponse{}, fmt.Errorf("error fetching subfolders: %w", err)
	}

	return apischema.SubfoldersResponse{Subfolders: folders}, nil
}

// fetchSubfoldersFromIndexer queries the indexer daemon for direct child folders with sizes
func fetchSubfoldersFromIndexer(ctx context.Context, path string) ([]apischema.SubfolderData, error) {
	folders, err := indexer.Subfolders(ctx, path)
	if err != nil {
		if errors.Is(err, indexer.ErrUnavailable) {
			setIndexerAvailability(false)
			return nil, fmt.Errorf("%w: indexer subfolders request failed: %v", errIndexerUnavailable, err)
		}
		return nil, err
	}
	setIndexerAvailability(true)
	return subfoldersFromIndexer(folders), nil
}

func subfoldersFromIndexer(folders []indexerSubfolder) []apischema.SubfolderData {
	result := make([]apischema.SubfolderData, 0, len(folders))
	for _, folder := range folders {
		result = append(result, apischema.SubfolderData{
			Path: folder.Path, Size: folder.Size,
		})
	}
	return result
}

// searchFiles searches for files/directories in the indexer database.
func searchFiles(ctx context.Context, req apischema.FileSearchRequest) (apischema.SearchResponse, error) {
	if req.Query == "" {
		return apischema.SearchResponse{}, fmt.Errorf("bad_request:missing search query")
	}

	if strings.TrimSpace(req.Query) == "" {
		return apischema.SearchResponse{}, fmt.Errorf("bad_request:search query cannot be empty")
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
			return apischema.SearchResponse{}, fmt.Errorf("bad_request:indexer unavailable")
		}
		slog.Debug("error searching indexer", "query", req.Query, "base_path", basePath, "error", err)
		return apischema.SearchResponse{}, fmt.Errorf("error searching files: %w", err)
	}

	return searchResponseFromIndexer(req.Query, results), nil
}

type indexerSearchResult = indexerapi.EntryResult

func searchResponseFromIndexer(_ string, results []indexerSearchResult) apischema.SearchResponse {
	response := apischema.SearchResponse{Results: make([]apischema.SearchResult, 0, len(results))}
	for _, result := range results {
		response.Results = append(response.Results, searchResultFromIndexer(result))
	}
	return response
}

func searchResultFromIndexer(result indexerSearchResult) apischema.SearchResult {
	isDir := result.Type == "folder"
	isRegularFile := result.Type == "file"
	var canOpenAsText *bool
	if isRegularFile {
		canOpen := result.Size < services.MaxTextFileBytes
		canOpenAsText = &canOpen
	}
	return apischema.SearchResult{
		IsDir: isDir, IsRegularFile: isRegularFile,
		ModTime: formatResourceModTime(result.ModTime), Name: result.Name, Path: result.Path, Size: result.Size,
		CanOpenAsText: canOpenAsText,
	}
}

// searchInIndexer queries the indexer for files matching the search term
func searchInIndexer(ctx context.Context, query, limit, basePath string) ([]indexerSearchResult, error) {
	results, err := indexer.Search(ctx, query, limit, basePath)
	if err != nil {
		if errors.Is(err, indexer.ErrUnavailable) {
			setIndexerAvailability(false)
			return nil, fmt.Errorf("%w: indexer search request failed: %v", errIndexerUnavailable, err)
		}
		return nil, err
	}
	setIndexerAvailability(true)
	if results == nil {
		return []indexerSearchResult{}, nil
	}
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
func usersGroups(ctx context.Context) (apischema.UsersGroupsResponse, error) {
	users, err := getAllUsers(ctx)
	if err != nil {
		slog.Debug("error getting users", "error", err)
		return apischema.UsersGroupsResponse{}, fmt.Errorf("error getting users: %w", err)
	}

	groups, err := getAllGroups(ctx)
	if err != nil {
		slog.Debug("error getting groups", "error", err)
		return apischema.UsersGroupsResponse{}, fmt.Errorf("error getting groups: %w", err)
	}

	return apischema.UsersGroupsResponse{Users: users, Groups: groups}, nil
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
// These operations now use durable tasks plus built-in tasks.data streams.
