package filebrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	"github.com/mordilloSan/go-logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/dbus"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

var (
	indexerAvailable      atomic.Bool
	errIndexerUnavailable = errors.New("indexer unavailable")
)

const indexerServiceName = "indexer.service"

func init() {
	indexerAvailable.Store(true)
}

func setIndexerAvailability(available bool) {
	indexerAvailable.Store(available)
}

func isIndexerEnabled() bool {
	return indexerAvailable.Load()
}

// resourceGet retrieves information about a resource
// Args: [path, "", getContent?] or [path]
func resourceGet(args []string) (any, error) {
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
		logger.Debugf("error getting file info: %v", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}

	return fileInfo, nil
}

// resourceStat returns extended metadata
// Args: [path]
func resourceStat(args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("bad_request:missing path")
	}

	path := args[0]

	fileInfo, err := services.FileInfoFaster(iteminfo.FileOptions{
		Path:   path,
		Expand: false,
	})
	if err != nil {
		logger.Debugf("error getting file stat info: %v", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}

	statData, err := iteminfo.CollectStatInfo(fileInfo.RealPath)
	if err != nil {
		logger.Debugf("error collecting stat info: %v", err)
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
func resourceDelete(args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("bad_request:missing path")
	}

	path := args[0]

	if path == "/" {
		return nil, fmt.Errorf("bad_request:cannot delete root")
	}

	fileInfo, err := services.FileInfoFaster(iteminfo.FileOptions{
		Path:   path,
		Expand: false,
	})
	if err != nil {
		logger.Debugf("error getting file info: %v", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}

	err = services.DeleteFiles(fileInfo.RealPath)
	if err != nil {
		logger.Debugf("error deleting file: %v", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}

	// Notify indexer about the deletion
	if err := deleteFromIndexer(path); err != nil {
		logger.Debugf("failed to update indexer after delete: %v", err)
		// Don't fail the operation if indexer update fails
	}

	return map[string]any{"message": "deleted"}, nil
}

// resourcePost creates or uploads a new resource
// Args: [path, override?, chunkOffset?, totalSize?, body]
func resourcePost(args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("bad_request:missing path")
	}

	path := args[0]
	path, err := url.QueryUnescape(path)
	if err != nil {
		return nil, fmt.Errorf("bad_request:invalid path encoding")
	}

	override := len(args) > 1 && args[1] == "true"

	isDir := strings.HasSuffix(path, "/")
	realPath := filepath.Join(path)

	// Check for file/folder conflicts before creation
	if stat, statErr := os.Stat(realPath); statErr == nil {
		existingIsDir := stat.IsDir()
		requestingDir := isDir

		if existingIsDir != requestingDir && !override {
			return nil, fmt.Errorf("bad_request:resource already exists with different type")
		}
	}

	// Handle directory creation
	if isDir {
		err = services.CreateDirectory(iteminfo.FileOptions{
			Path:   path,
			Expand: false,
		})
		if err != nil {
			logger.Debugf("error writing directory: %v", err)
			return nil, fmt.Errorf("bad_request:%v", err)
		}

		// Notify indexer about the new directory
		if info, statErr := os.Stat(realPath); statErr == nil {
			if indexErr := addToIndexer(path, info); indexErr != nil {
				logger.Debugf("failed to update indexer after directory create: %v", indexErr)
			}
		}

		return map[string]any{"message": "created"}, nil
	}

	// Handle empty file creation
	// File uploads with content use yamux streams (fb-upload), not this handler
	parentDir := filepath.Dir(realPath)
	if mkdirErr := os.MkdirAll(parentDir, services.PermDir); mkdirErr != nil {
		logger.Debugf("error creating parent directory: %v", mkdirErr)
		return nil, fmt.Errorf("bad_request:failed to create parent directory: %v", mkdirErr)
	}

	// Check if file exists
	if _, statErr := os.Stat(realPath); statErr == nil {
		if !override {
			return nil, fmt.Errorf("bad_request:file already exists")
		}
	}

	// Create empty file
	f, err := os.Create(realPath)
	if err != nil {
		logger.Debugf("error creating file: %v", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}
	f.Close()

	// Notify indexer about the new file
	if info, err := os.Stat(realPath); err == nil {
		if err := addToIndexer(path, info); err != nil {
			logger.Debugf("failed to update indexer after file create: %v", err)
		}
	}

	return map[string]any{"message": "created"}, nil
}

// resourcePatchWithProgress performs patch operations with progress feedback
// Args: [action, from, destination, overwrite?]
func resourcePatchWithProgress(ctx context.Context, args []string, emit ipc.Events) (any, error) {
	if len(args) < 3 {
		return nil, fmt.Errorf("bad_request:missing action, from, or destination")
	}

	action := args[0]
	src, err := url.QueryUnescape(args[1])
	if err != nil {
		return nil, fmt.Errorf("bad_request:invalid source path encoding")
	}

	dst, err := url.QueryUnescape(args[2])
	if err != nil {
		return nil, fmt.Errorf("bad_request:invalid destination path encoding")
	}

	if dst == "/" || src == "/" {
		return nil, fmt.Errorf("bad_request:cannot modify root directory")
	}

	// Strip trailing slashes from dst for proper parent directory calculation
	// filepath.Dir("/a/b/c/") incorrectly returns "/a/b/c" instead of "/a/b"
	dstClean := strings.TrimRight(dst, "/")

	// Check parent dir exists
	parentDir := filepath.Dir(dstClean)
	_, statErr := os.Stat(parentDir)
	if statErr != nil {
		logger.Debugf("parent directory not found: %s (error: %v)", parentDir, statErr)
		return nil, fmt.Errorf("bad_request:parent directory not found")
	}

	overwrite := len(args) > 3 && args[3] == "true"

	// Reconstruct destination path from parent and base name
	// Preserve trailing slash for directories
	baseName := filepath.Base(dstClean)
	realDest := filepath.Join(parentDir, baseName)
	if strings.HasSuffix(dst, "/") && !strings.HasSuffix(realDest, "/") {
		realDest += "/"
	}
	realSrc := filepath.Join(src)

	srcInfo, err := os.Stat(realSrc)
	if err != nil {
		logger.Debugf("error getting source info: %v", err)
		return nil, fmt.Errorf("bad_request:source not found")
	}

	// If copying to the same location, generate a unique name
	if realSrc == realDest && action == "copy" {
		realDest = generateUniquePath(realDest, srcInfo.IsDir())
	}

	destInfo, destErr := os.Stat(realDest)
	destExists := destErr == nil
	if destErr != nil && !os.IsNotExist(destErr) {
		logger.Debugf("error stating destination: %v", destErr)
		return nil, fmt.Errorf("bad_request:could not stat destination")
	}

	if destExists {
		if realSrc == realDest {
			return nil, fmt.Errorf("bad_request:source and destination are the same")
		}
		if !overwrite {
			return nil, fmt.Errorf("bad_request:destination exists")
		}
		if srcInfo.IsDir() != destInfo.IsDir() {
			return nil, fmt.Errorf("bad_request:destination exists with different type")
		}
	}

	// Compute total size for progress
	totalSize, err := services.ComputeCopySize(realSrc)
	if err != nil {
		logger.Debugf("failed to compute size: %v", err)
		totalSize = 0
	}

	// Send initial progress
	logger.Infof("[FBHandler] Starting %s operation: %s -> %s (size=%d)", action, realSrc, realDest, totalSize)
	_ = emit.Progress(FileProgress{
		Total: totalSize,
		Phase: "preparing",
	})

	// Create progress callbacks
	var bytesProcessed int64
	var lastProgress int64
	progressInterval := int64(2 * 1024 * 1024) // 2MB

	opts := &ipc.OperationCallbacks{
		Progress: func(n int64) {
			bytesProcessed += n
			if totalSize > 0 && (bytesProcessed-lastProgress >= progressInterval || bytesProcessed >= totalSize) {
				pct := int(bytesProcessed * 100 / totalSize)
				if pct > 100 {
					pct = 100
				}
				phase := "copying"
				if action == "move" || action == "rename" {
					phase = "moving"
				}
				logger.Debugf("[FBHandler] Progress: %d/%d bytes (%d%%) - %s", bytesProcessed, totalSize, pct, phase)
				_ = emit.Progress(FileProgress{
					Bytes: bytesProcessed,
					Total: totalSize,
					Pct:   pct,
					Phase: phase,
				})
				lastProgress = bytesProcessed
			}
		},
		Cancel: func() bool {
			// Check if context is cancelled
			select {
			case <-ctx.Done():
				return true
			default:
				return false
			}
		},
	}

	switch action {
	case "copy":
		err := services.CopyFileWithCallbacks(realSrc, realDest, overwrite, opts)
		if err != nil {
			logger.Debugf("error copying resource: %v", err)
			return nil, fmt.Errorf("bad_request:%v", err)
		}
		logger.Infof("[FBHandler] Copy complete: %s -> %s (bytes=%d)", realSrc, realDest, bytesProcessed)
		// Notify indexer about the copied file/directory
		if info, err := os.Stat(realDest); err == nil {
			if err := addToIndexer(dst, info); err != nil {
				logger.Debugf("failed to update indexer after copy: %v", err)
			}
		}
	case "rename", "move":
		err := services.MoveFileWithCallbacks(realSrc, realDest, overwrite, opts)
		if err != nil {
			logger.Debugf("error moving/renaming resource: %v", err)
			return nil, fmt.Errorf("bad_request:%v", err)
		}
		logger.Infof("[FBHandler] Move complete: %s -> %s (bytes=%d)", realSrc, realDest, bytesProcessed)
		// Notify indexer about the move: delete source, add destination
		if err := deleteFromIndexer(src); err != nil {
			logger.Debugf("failed to update indexer after move (delete source): %v", err)
		}
		if info, err := os.Stat(realDest); err == nil {
			if err := addToIndexer(dst, info); err != nil {
				logger.Debugf("failed to update indexer after move (add destination): %v", err)
			}
		}
	default:
		return nil, fmt.Errorf("bad_request:unsupported action: %s", action)
	}

	return map[string]any{"message": "operation completed"}, nil
}

// generateUniquePath generates a unique path by appending a suffix like " (copy)" or " (copy 2)"
func generateUniquePath(path string, isDir bool) string {
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
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		return newPath
	}

	// Try "name (copy 2).ext", "name (copy 3).ext", etc.
	for i := 2; i < 1000; i++ {
		newPath = filepath.Join(dir, fmt.Sprintf("%s (copy %d)%s", name, i, ext))
		if _, err := os.Stat(newPath); os.IsNotExist(err) {
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
func addToIndexer(path string, info os.FileInfo) error {
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

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://unix/add", strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("failed to build indexer add request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		// Log but don't fail the operation if indexer is unavailable
		logger.Debugf("indexer add request failed (indexer may be offline): %v", err)
		setIndexerAvailability(false)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Debugf("indexer add returned non-OK status: %s", resp.Status)
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
	logger.InfoKV("notified indexer of added/updated entry",
		"path", entry.Path,
		"type", fileType,
		"size", entry.Size)

	return nil
}

// deleteFromIndexer notifies the indexer daemon about a deleted file/directory.
// This updates the cached directory sizes in the indexer.
func deleteFromIndexer(path string) error {
	if !isIndexerEnabled() {
		return nil
	}

	normPath := normalizeIndexerPath(path)
	deleteURL := fmt.Sprintf("http://unix/delete?path=%s", url.QueryEscape(normPath))

	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, deleteURL, nil)
	if err != nil {
		return fmt.Errorf("failed to build indexer delete request: %w", err)
	}

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		// Log but don't fail the operation if indexer is unavailable
		logger.Debugf("indexer delete request failed (indexer may be offline): %v", err)
		setIndexerAvailability(false)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Debugf("indexer delete returned non-OK status: %s", resp.Status)
		if resp.StatusCode >= http.StatusInternalServerError {
			setIndexerAvailability(false)
		}
		return nil
	}

	setIndexerAvailability(true)

	// Log successful indexer deletion
	logger.InfoKV("notified indexer of deleted entry", "path", normPath)

	return nil
}

// CheckIndexerAvailability checks if the indexer daemon is running via systemd.
// Returns true if the service is active.
func CheckIndexerAvailability() (bool, error) {
	info, err := dbus.GetServiceInfo(indexerServiceName)
	if err != nil {
		setIndexerAvailability(false)
		return false, err
	}

	activeState, ok := info["ActiveState"].(string)
	if !ok || activeState == "" {
		setIndexerAvailability(false)
		return false, fmt.Errorf("indexer service state unavailable")
	}

	subState, _ := info["SubState"].(string)
	if activeState != "active" || subState != "running" {
		setIndexerAvailability(false)
		if subState != "" {
			return false, fmt.Errorf("indexer service not running: %s (%s)", activeState, subState)
		}
		return false, fmt.Errorf("indexer service not running: %s", activeState)
	}

	setIndexerAvailability(true)
	logger.Infof("indexer service available")

	return true, nil
}

type indexerDirSizeResponse struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Bytes int64  `json:"bytes"`
}

// fetchDirSizeFromIndexer queries the indexer daemon over its Unix socket for a cached directory size.
func fetchDirSizeFromIndexer(path string) (int64, error) {
	normPath := normalizeIndexerPath(path)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://unix/dirsize", nil)
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

// dirSize calculates the total size of a directory recursively
// Args: [path]
func dirSize(args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("bad_request:missing path")
	}

	path := args[0]
	realPath := filepath.Join(path)

	// Check if path exists and is a directory
	stat, err := os.Stat(realPath)
	if err != nil {
		logger.Debugf("error stating directory: %v", err)
		return nil, fmt.Errorf("bad_request:directory not found")
	}

	if !stat.IsDir() {
		return nil, fmt.Errorf("bad_request:path is not a directory")
	}

	// Get directory size from the indexer daemon (precomputed)
	size, err := fetchDirSizeFromIndexer(path)
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return nil, fmt.Errorf("bad_request:indexer unavailable")
		}
		logger.Debugf("error fetching directory size from indexer: %v", err)
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
func subfolders(args []string) (any, error) {
	path := "/"
	if len(args) > 0 && args[0] != "" {
		path = args[0]
	}

	// Validate path exists and is a directory if not root.
	if path != "/" {
		realPath := filepath.Join(path)
		stat, err := os.Stat(realPath)
		if err != nil {
			logger.Debugf("error stating directory: %v", err)
			return nil, fmt.Errorf("bad_request:directory not found")
		}
		if !stat.IsDir() {
			return nil, fmt.Errorf("bad_request:path is not a directory")
		}
	}

	// Fetch subfolders from indexer (it will handle path validation)
	folders, err := fetchSubfoldersFromIndexer(path)
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return nil, fmt.Errorf("bad_request:indexer unavailable")
		}
		logger.Debugf("error fetching subfolders from indexer: %v", err)
		return nil, fmt.Errorf("error fetching subfolders: %w", err)
	}

	return map[string]any{
		"path":       path,
		"subfolders": folders,
		"count":      len(folders),
	}, nil
}

// fetchSubfoldersFromIndexer queries the indexer daemon for direct child folders with sizes
func fetchSubfoldersFromIndexer(path string) ([]subfoldersResponse, error) {
	normPath := normalizeIndexerPath(path)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://unix/subfolders", nil)
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
func searchFiles(args []string) (any, error) {
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

	results, err := searchInIndexer(query, limit, basePath)
	if err != nil {
		if errors.Is(err, errIndexerUnavailable) {
			return nil, fmt.Errorf("bad_request:indexer unavailable")
		}
		logger.Debugf("error searching indexer: %v", err)
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
		path, _ := result["path"].(string)
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
func searchInIndexer(query, limit, basePath string) ([]map[string]any, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://unix/search", nil)
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

// resourceChmod changes file or directory permissions and can optionally update ownership.
// Args: [path, mode, owner?, group?, recursive?]
func resourceChmod(args []string) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("bad_request:missing path or mode")
	}

	path := args[0]
	modeStr := args[1]
	owner := ""
	group := ""
	recursive := false

	switch len(args) {
	case 3:
		if args[2] == "true" || args[2] == "false" {
			recursive = args[2] == "true"
		} else {
			owner = args[2]
		}
	case 4:
		owner = args[2]
		group = args[3]
	case 5:
		owner = args[2]
		group = args[3]
		recursive = args[4] == "true"
	}

	// Parse the mode string (e.g., "0755", "755")
	var mode int64
	var err error
	if strings.HasPrefix(modeStr, "0") {
		mode, err = strconv.ParseInt(modeStr, 8, 32)
	} else {
		mode, err = strconv.ParseInt(modeStr, 8, 32)
	}
	if err != nil {
		return nil, fmt.Errorf("bad_request:invalid mode: %v", err)
	}

	realPath := filepath.Join(path)

	err = services.ChangePermissions(realPath, os.FileMode(mode), recursive)
	if err != nil {
		logger.Debugf("error changing permissions: %v", err)
		return nil, fmt.Errorf("bad_request:%v", err)
	}

	if strings.TrimSpace(owner) != "" || strings.TrimSpace(group) != "" {
		uid, err := resolveUserID(owner)
		if err != nil {
			logger.Debugf("error resolving owner: %v", err)
			return nil, fmt.Errorf("bad_request:%v", err)
		}

		gid, err := resolveGroupID(group)
		if err != nil {
			logger.Debugf("error resolving group: %v", err)
			return nil, fmt.Errorf("bad_request:%v", err)
		}

		if err := services.ChangeOwnership(realPath, uid, gid, recursive); err != nil {
			logger.Debugf("error changing ownership: %v", err)
			return nil, fmt.Errorf("bad_request:%v", err)
		}
	}

	return map[string]any{
		"message": "permissions changed",
		"path":    path,
		"mode":    fmt.Sprintf("%04o", mode),
		"owner":   owner,
		"group":   group,
	}, nil
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
func usersGroups() (any, error) {
	users, err := getAllUsers()
	if err != nil {
		logger.Debugf("error getting users: %v", err)
		return nil, fmt.Errorf("error getting users: %w", err)
	}

	groups, err := getAllGroups()
	if err != nil {
		logger.Debugf("error getting groups: %v", err)
		return nil, fmt.Errorf("error getting groups: %w", err)
	}

	return map[string]any{
		"users":  users,
		"groups": groups,
	}, nil
}

func getAllUsers() ([]string, error) {
	content, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return nil, err
	}

	users := []string{}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
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

func getAllGroups() ([]string, error) {
	content, err := os.ReadFile("/etc/group")
	if err != nil {
		return nil, err
	}

	groups := []string{}
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
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

// NOTE: fileUploadFromTemp removed - uploads now use yamux streams (fb-upload)

// fileUpdateFromTemp replaces an existing file (or creates it) using data staged in a temp file.
// Args: [tempFilePath, destinationPath]
func fileUpdateFromTemp(args []string) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("bad_request:missing temp file path or destination")
	}

	tempFilePath := args[0]
	destPath := args[1]

	tempStat, err := os.Stat(tempFilePath)
	if err != nil {
		return nil, fmt.Errorf("bad_request:temp file not found: %v", err)
	}
	if tempStat.IsDir() {
		return nil, fmt.Errorf("bad_request:temp path is a directory")
	}

	realDest := filepath.Join(destPath)

	destStat, err := os.Stat(realDest)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to stat destination: %v", err)
	}
	if err == nil && destStat.IsDir() {
		return nil, fmt.Errorf("bad_request:destination is a directory")
	}

	if err := os.MkdirAll(filepath.Dir(realDest), services.PermDir); err != nil {
		return nil, fmt.Errorf("failed to create parent directory: %v", err)
	}

	desiredMode := services.PermFile
	var uid, gid int
	hasOwner := false
	if destStat != nil {
		desiredMode = destStat.Mode()
		if st, ok := destStat.Sys().(*syscall.Stat_t); ok {
			uid = int(st.Uid)
			gid = int(st.Gid)
			hasOwner = true
		}
	}

	if err := replaceFileFromTemp(tempFilePath, realDest, desiredMode, hasOwner, uid, gid); err != nil {
		return nil, err
	}

	// Notify indexer about the updated file
	if finalInfo, err := os.Stat(realDest); err == nil {
		if err := addToIndexer(destPath, finalInfo); err != nil {
			logger.Debugf("failed to update indexer after file update: %v", err)
			// Don't fail the operation if indexer update fails
		}
	}

	return map[string]any{"message": "file updated", "path": destPath}, nil
}

func replaceFileFromTemp(tempPath, destPath string, mode os.FileMode, restoreOwner bool, uid, gid int) error {
	// Attempt an atomic replace first.
	if err := os.Rename(tempPath, destPath); err == nil {
		if err := os.Chmod(destPath, mode); err != nil {
			return fmt.Errorf("failed to set permissions: %v", err)
		}
		if restoreOwner {
			if err := os.Chown(destPath, uid, gid); err != nil {
				logger.Debugf("failed to restore ownership for %s: %v", destPath, err)
			}
		}
		return nil
	}

	// Cross-device fallback: copy into a temp file in the destination directory, then rename.
	tmpFile, err := os.CreateTemp(filepath.Dir(destPath), "linuxio-update-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to prepare temporary file: %v", err)
	}
	tmpPath := tmpFile.Name()
	cleanup := true
	defer func() {
		_ = tmpFile.Close()
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := copyIntoFile(tempPath, tmpFile); err != nil {
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to flush temporary file: %v", err)
	}
	if err := tmpFile.Chmod(mode); err != nil {
		return fmt.Errorf("failed to set permissions on temporary file: %v", err)
	}
	if restoreOwner {
		if err := os.Chown(tmpPath, uid, gid); err != nil {
			logger.Debugf("failed to set ownership on temporary file %s: %v", tmpPath, err)
		}
	}

	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary file: %v", err)
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		return fmt.Errorf("failed to replace destination: %v", err)
	}
	cleanup = false

	return nil
}

func copyIntoFile(srcPath string, dst *os.File) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("failed to open temp file: %v", err)
	}
	defer src.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to write file data: %v", err)
	}
	return nil
}

// NOTE: fileDownloadToTemp and archiveDownloadSetup removed
// Downloads now use yamux streams (fb-download, fb-archive)
