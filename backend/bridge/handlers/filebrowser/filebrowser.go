package filebrowser

import (
	"context"
	"encoding/json"
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
	"syscall"
	"time"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

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
		return map[string]any{"message": "created"}, nil
	}

	// For file uploads, we need body data from the request
	// This will be handled differently - IPC doesn't support binary data streaming
	// For now, we'll return an error since file uploads need HTTP streaming
	return nil, fmt.Errorf("bad_request:file upload requires HTTP streaming")
}

// resourcePatch performs patch operations (move, copy, rename)
// Args: [action, from, destination, overwrite?]
func resourcePatch(args []string) (any, error) {
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

	// Check parent dir exists
	parentDir := filepath.Dir(dst)
	_, statErr := os.Stat(parentDir)
	if statErr != nil {
		logger.Debugf("could not get parent dir: %v", statErr)
		return nil, fmt.Errorf("bad_request:parent directory not found")
	}

	overwrite := len(args) > 3 && args[3] == "true"

	realDest := filepath.Join(parentDir, filepath.Base(dst))
	realSrc := filepath.Join(src)

	srcInfo, err := os.Stat(realSrc)
	if err != nil {
		logger.Debugf("error getting source info: %v", err)
		return nil, fmt.Errorf("bad_request:source not found")
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

	switch action {
	case "copy":
		err := services.CopyFile(realSrc, realDest, overwrite)
		if err != nil {
			logger.Debugf("error copying resource: %v", err)
			return nil, fmt.Errorf("bad_request:%v", err)
		}
		// Notify indexer about the copied file/directory
		if info, err := os.Stat(realDest); err == nil {
			if err := addToIndexer(dst, info); err != nil {
				logger.Debugf("failed to update indexer after copy: %v", err)
			}
		}
	case "rename", "move":
		err := services.MoveFile(realSrc, realDest, overwrite)
		if err != nil {
			logger.Debugf("error moving/renaming resource: %v", err)
			return nil, fmt.Errorf("bad_request:%v", err)
		}
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

type indexerDirSizeResponse struct {
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Bytes int64  `json:"bytes"`
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
		return 0, fmt.Errorf("indexer dirsize request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("indexer dirsize returned status %s", resp.Status)
	}

	var payload indexerDirSizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode indexer dirsize response: %w", err)
	}

	if payload.Size != 0 {
		return payload.Size, nil
	}
	return payload.Bytes, nil
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
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Debugf("indexer add returned non-OK status: %s", resp.Status)
		return nil
	}

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
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Debugf("indexer delete returned non-OK status: %s", resp.Status)
		return nil
	}

	// Log successful indexer deletion
	logger.InfoKV("notified indexer of deleted entry", "path", normPath)

	return nil
}

// checkIndexerStatus checks if the indexer daemon is running by calling GET /status.
// Returns true if the indexer is available and responsive.
func checkIndexerStatus() (bool, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://unix/status", nil)
	if err != nil {
		return false, fmt.Errorf("failed to build indexer status request: %w", err)
	}

	resp, err := indexerHTTPClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("indexer status returned status %s", resp.Status)
	}

	var status struct {
		Status        string `json:"status"`
		NumDirs       int64  `json:"num_dirs,omitempty"`
		NumFiles      int64  `json:"num_files,omitempty"`
		TotalSize     int64  `json:"total_size,omitempty"`
		LastIndexed   string `json:"last_indexed,omitempty"`
		TotalIndexes  int    `json:"total_indexes,omitempty"`
		TotalEntries  int64  `json:"total_entries,omitempty"`
		DatabaseSize  int64  `json:"database_size,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return false, fmt.Errorf("failed to decode indexer status: %w", err)
	}

	// Status should be "idle" or "running"
	if status.Status != "idle" && status.Status != "running" {
		return false, fmt.Errorf("unexpected indexer status: %s", status.Status)
	}

	// Log successful indexer connection with details
	if status.Status == "idle" {
		logger.InfoKV("indexer daemon available",
			"status", status.Status,
			"num_dirs", status.NumDirs,
			"num_files", status.NumFiles,
			"total_size", status.TotalSize,
			"last_indexed", status.LastIndexed)
	} else {
		logger.InfoKV("indexer daemon available", "status", status.Status)
	}

	return true, nil
}

// indexerStatus checks if the indexer daemon is running and returns its status.
// Args: []
func indexerStatus(_ []string) (any, error) {
	available, err := checkIndexerStatus()
	if err != nil {
		return map[string]any{
			"available": false,
			"error":     err.Error(),
		}, nil
	}

	return map[string]any{
		"available": available,
	}, nil
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
		logger.Debugf("error fetching directory size from indexer: %v", err)
		return nil, fmt.Errorf("error fetching directory size: %w", err)
	}

	return map[string]any{
		"path": path,
		"size": size,
	}, nil
}

// archiveCreate builds an archive on disk from provided files.
// Args: [destinationPath, fileList, algo?]
func archiveCreate(ctx *ipc.RequestContext, args []string) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("bad_request:missing destination or files")
	}

	destination := args[0]
	fileListRaw := args[1]
	files := strings.Split(fileListRaw, "||")
	if len(files) == 0 || files[0] == "" {
		return nil, fmt.Errorf("bad_request:invalid files list")
	}

	algo := "zip"
	if len(args) > 2 && args[2] != "" {
		algo = args[2]
	}
	overwrite := len(args) > 3 && args[3] == "true"

	var extension string
	switch algo {
	case "zip", "true", "":
		extension = ".zip"
	case "tar.gz":
		extension = ".tar.gz"
	default:
		return nil, fmt.Errorf("bad_request:format not implemented")
	}

	targetPath := filepath.Join(destination)
	lowerTarget := strings.ToLower(targetPath)

	switch extension {
	case ".zip":
		if !strings.HasSuffix(lowerTarget, ".zip") {
			targetPath = targetPath + ".zip"
		}
	case ".tar.gz":
		if !(strings.HasSuffix(lowerTarget, ".tar.gz") || strings.HasSuffix(lowerTarget, ".tgz")) {
			targetPath = targetPath + ".tar.gz"
		}
	}

	if info, statErr := os.Stat(targetPath); statErr == nil {
		if !overwrite {
			return nil, fmt.Errorf("bad_request:destination exists")
		}
		if info.IsDir() {
			return nil, fmt.Errorf("bad_request:destination exists with different type")
		}
		if rmErr := os.Remove(targetPath); rmErr != nil {
			return nil, fmt.Errorf("error preparing archive destination: %w", rmErr)
		}
	} else if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("error checking archive destination: %w", statErr)
	}

	var progressCb services.ProgressCallback
	var totalSize int64
	var processed int64
	var lastPercent float64

	streamProgress := ctx != nil && ctx.HasStream()
	sendProgress := func(status string, percent float64) {
		if !streamProgress {
			return
		}
		payload := map[string]any{
			"percent":        percent,
			"bytesProcessed": processed,
			"totalBytes":     totalSize,
		}
		if err := ctx.SendStreamJSON(status, payload); err != nil {
			logger.Debugf("archive_create stream send failed: %v", err)
		}
	}

	if streamProgress {
		size, err := services.ComputeArchiveSize(files)
		if err != nil {
			logger.Debugf("error computing archive size for progress: %v", err)
		} else if size > 0 {
			totalSize = size
			progressCb = func(n int64) {
				processed += n
				if processed > totalSize {
					processed = totalSize
				}
				percent := float64(processed) / float64(totalSize) * 100
				if percent > 100 {
					percent = 100
				}
				if percent < lastPercent+0.5 && percent < 100 {
					return
				}
				lastPercent = percent
				sendProgress("compression_progress", percent)
			}

			processed = 0
			lastPercent = 0
			sendProgress("compression_progress", 0)
		}
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), services.PermDir); err != nil {
		logger.Debugf("error preparing archive destination: %v", err)
		return nil, fmt.Errorf("error preparing archive destination: %w", err)
	}

	var err error
	switch extension {
	case ".zip":
		err = services.CreateZip(targetPath, progressCb, targetPath, files...)
	case ".tar.gz":
		err = services.CreateTarGz(targetPath, progressCb, targetPath, files...)
	}
	if err != nil {
		logger.Debugf("error creating archive: %v", err)
		return nil, fmt.Errorf("error creating archive: %w", err)
	}

	if streamProgress && totalSize > 0 {
		processed = totalSize
		sendProgress("compression_complete", 100)
	}

	// Notify indexer about the created archive file
	if info, err := os.Stat(targetPath); err == nil {
		if err := addToIndexer(destination, info); err != nil {
			logger.Debugf("failed to update indexer after archive create: %v", err)
		}
	}

	return map[string]any{
		"path":   targetPath,
		"format": algo,
	}, nil
}

// archiveExtract extracts supported archives to a destination folder.
// Args: [archivePath, destination?]
func archiveExtract(ctx *ipc.RequestContext, args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("bad_request:missing archive path")
	}

	archivePath := filepath.Join(args[0])
	destination := ""
	if len(args) > 1 && args[1] != "" {
		destination = filepath.Join(args[1])
	} else {
		destination = defaultExtractDestination(archivePath)
	}

	streamProgress := ctx != nil && ctx.HasStream()
	var progressCb services.ProgressCallback
	var totalSize int64
	var processed int64
	var lastPercent float64

	sendProgress := func(status string, percent float64) {
		if !streamProgress {
			return
		}
		payload := map[string]any{
			"percent":        percent,
			"bytesProcessed": processed,
			"totalBytes":     totalSize,
		}
		if err := ctx.SendStreamJSON(status, payload); err != nil {
			logger.Debugf("archive_extract stream send failed: %v", err)
		}
	}

	if streamProgress {
		size, err := services.ComputeExtractSize(archivePath)
		if err != nil {
			logger.Debugf("error computing extract size for progress: %v", err)
		} else if size > 0 {
			totalSize = size
			progressCb = func(n int64) {
				processed += n
				if processed > totalSize {
					processed = totalSize
				}
				percent := float64(processed) / float64(totalSize) * 100
				if percent > 100 {
					percent = 100
				}
				if percent < lastPercent+0.5 && percent < 100 {
					return
				}
				lastPercent = percent
				sendProgress("extraction_progress", percent)
			}

			processed = 0
			lastPercent = 0
			sendProgress("extraction_progress", 0)
		}
	}

	if err := services.ExtractArchive(archivePath, destination, progressCb); err != nil {
		logger.Debugf("error extracting archive: %v", err)
		return nil, fmt.Errorf("error extracting archive: %w", err)
	}

	if streamProgress && totalSize > 0 {
		processed = totalSize
		sendProgress("extraction_complete", 100)
	}

	// Notify indexer about all extracted files
	// Walk the destination directory and add all files/directories to indexer
	if err := filepath.Walk(destination, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip errors, continue walking
		}
		// Convert absolute path to the path format used in the args
		relPath := path
		if err := addToIndexer(relPath, info); err != nil {
			logger.Debugf("failed to update indexer for extracted file %s: %v", path, err)
		}
		return nil
	}); err != nil {
		logger.Debugf("failed to walk extracted directory for indexer update: %v", err)
	}

	return map[string]any{
		"destination": destination,
	}, nil
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
func usersGroups(args []string) (any, error) {
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

// fileUploadFromTemp moves a file from a temp location to the final destination.
// Args: [tempFilePath, destinationPath, override?]
func fileUploadFromTemp(ctx *ipc.RequestContext, args []string) (any, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("bad_request:missing temp file path or destination")
	}

	tempFilePath := args[0]
	destPath := args[1]
	override := len(args) > 2 && args[2] == "true"

	// Validate temp file exists
	tempStat, err := os.Stat(tempFilePath)
	if err != nil {
		return nil, fmt.Errorf("bad_request:temp file not found: %v", err)
	}
	if tempStat.IsDir() {
		return nil, fmt.Errorf("bad_request:temp path is a directory")
	}

	// Clean destination path
	realDest := filepath.Join(destPath)

	// Check for conflicts
	if stat, statErr := os.Stat(realDest); statErr == nil {
		if stat.IsDir() {
			return nil, fmt.Errorf("bad_request:destination is a directory")
		}
		if !override {
			return nil, fmt.Errorf("bad_request:file already exists")
		}
	}

	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(realDest), services.PermDir); err != nil {
		return nil, fmt.Errorf("failed to create parent directory: %v", err)
	}

	streamProgress := ctx != nil && ctx.HasStream()
	totalSize := tempStat.Size()
	var processed int64
	var lastPercent float64

	sendProgress := func(status string, percent float64) {
		if !streamProgress {
			return
		}
		payload := map[string]any{
			"percent":        percent,
			"bytesProcessed": processed,
			"totalBytes":     totalSize,
		}
		if err := ctx.SendStreamJSON(status, payload); err != nil {
			logger.Debugf("file_upload_from_temp stream send failed: %v", err)
		}
	}

	updateProgress := func(delta int64) {
		if !streamProgress || totalSize <= 0 {
			return
		}
		processed += delta
		if processed > totalSize {
			processed = totalSize
		}
		percent := float64(processed) / float64(totalSize) * 100
		if percent > 100 {
			percent = 100
		}
		if percent < lastPercent+0.5 && percent < 100 {
			return
		}
		lastPercent = percent
		sendProgress("upload_progress", percent)
	}

	copyWithProgress := func(src, dst string) error {
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		defer in.Close()

		out, err := os.OpenFile(dst, os.O_RDWR|os.O_CREATE|os.O_TRUNC, services.PermFile)
		if err != nil {
			return err
		}
		defer out.Close()

		buf := make([]byte, 512*1024)
		for {
			n, rerr := in.Read(buf)
			if n > 0 {
				if _, werr := out.Write(buf[:n]); werr != nil {
					return werr
				}
				updateProgress(int64(n))
			}
			if rerr == io.EOF {
				break
			}
			if rerr != nil {
				return rerr
			}
		}
		if err := out.Sync(); err != nil {
			return err
		}
		return nil
	}

	// Move temp file to destination
	if err := os.Rename(tempFilePath, realDest); err != nil {
		// If rename fails (cross-device), copy and delete with progress reporting
		if streamProgress && totalSize > 0 {
			sendProgress("upload_progress", 0)
		}
		if err := copyWithProgress(tempFilePath, realDest); err != nil {
			return nil, fmt.Errorf("failed to copy file: %v", err)
		}
		_ = os.Remove(tempFilePath)
	} else {
		processed = totalSize
	}

	// Set proper permissions
	if err := os.Chmod(realDest, services.PermFile); err != nil {
		logger.Debugf("failed to set permissions: %v", err)
	}

	if streamProgress {
		if totalSize == 0 {
			processed = 0
		} else if processed == 0 {
			processed = totalSize
		}
		sendProgress("upload_complete", 100)
	}

	// Notify indexer about the new file
	if finalInfo, err := os.Stat(realDest); err == nil {
		if err := addToIndexer(destPath, finalInfo); err != nil {
			logger.Debugf("failed to update indexer after upload: %v", err)
			// Don't fail the operation if indexer update fails
		}
	}

	return map[string]any{"message": "file uploaded", "path": destPath}, nil
}

// fileUpdateFromTemp replaces an existing file (or creates it) using data staged in a temp file.
// Args: [tempFilePath, destinationPath]
func fileUpdateFromTemp(ctx *ipc.RequestContext, args []string) (any, error) {
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

// fileDownloadToTemp copies a file to a temp location for the server to stream
// Args: [filePath]
func fileDownloadToTemp(ctx *ipc.RequestContext, args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("bad_request:missing file path")
	}

	filePath := args[0]
	realPath := filepath.Join(filePath)

	// Validate file exists
	stat, err := os.Stat(realPath)
	if err != nil {
		return nil, fmt.Errorf("bad_request:file not found: %v", err)
	}
	if stat.IsDir() {
		return nil, fmt.Errorf("bad_request:path is a directory, use archive_download_setup instead")
	}

	// Create temp file
	tempFile, err := os.CreateTemp("", "linuxio-download-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %v", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()

	streamProgress := ctx != nil && ctx.HasStream()
	send := func(status string, percent float64, processed int64) {
		if !streamProgress {
			return
		}
		payload := map[string]any{
			"percent":        percent,
			"bytesProcessed": processed,
			"totalBytes":     stat.Size(),
		}
		if sendErr := ctx.SendStreamJSON(status, payload); sendErr != nil {
			logger.Debugf("file_download_to_temp stream send failed: %v", sendErr)
		}
	}
	if streamProgress {
		send("download_progress", 0, 0)
	}

	// Copy file to temp location
	if err := services.CopyFile(realPath, tempPath, true); err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("failed to copy file: %v", err)
	}

	fileName := filepath.Base(realPath)
	if streamProgress {
		send("download_ready", 100, stat.Size())
	}

	return map[string]any{
		"tempPath": tempPath,
		"fileName": fileName,
		"size":     stat.Size(),
	}, nil
}

// archiveDownloadSetup creates an archive in a temp location for download
// Args: [fileList (|| separated), algo?]
func archiveDownloadSetup(ctx *ipc.RequestContext, args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("bad_request:missing file list")
	}

	fileListRaw := args[0]
	files := strings.Split(fileListRaw, "||")
	if len(files) == 0 || files[0] == "" {
		return nil, fmt.Errorf("bad_request:invalid files list")
	}

	algo := "zip"
	if len(args) > 1 && args[1] != "" {
		algo = args[1]
	}

	var extension string
	switch algo {
	case "zip", "true", "":
		extension = ".zip"
	case "tar.gz":
		extension = ".tar.gz"
	default:
		return nil, fmt.Errorf("bad_request:unsupported format")
	}

	// Create temp file for archive
	tempFile, err := os.CreateTemp("", "linuxio-archive-*"+extension)
	if err != nil {
		return nil, fmt.Errorf("failed to create temp archive: %v", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()

	streamProgress := ctx != nil && ctx.HasStream()
	var totalSize int64
	var processed int64
	var lastPercent float64
	send := func(status string, percent float64) {
		if !streamProgress {
			return
		}
		payload := map[string]any{
			"percent":        percent,
			"bytesProcessed": processed,
			"totalBytes":     totalSize,
		}
		if sendErr := ctx.SendStreamJSON(status, payload); sendErr != nil {
			logger.Debugf("archive_download_setup stream send failed: %v", sendErr)
		}
	}

	var progressCb services.ProgressCallback
	if streamProgress {
		size, sizeErr := services.ComputeArchiveSize(files)
		if sizeErr != nil {
			logger.Debugf("error computing archive size for download progress: %v", sizeErr)
		} else if size > 0 {
			totalSize = size
			progressCb = func(n int64) {
				processed += n
				if processed > totalSize {
					processed = totalSize
				}
				percent := float64(processed) / float64(totalSize) * 100
				if percent > 100 {
					percent = 100
				}
				if percent < lastPercent+0.5 && percent < 100 {
					return
				}
				lastPercent = percent
				send("download_progress", percent)
			}

			processed = 0
			lastPercent = 0
			send("download_progress", 0)
		}
	}

	// Create archive
	switch extension {
	case ".zip":
		err = services.CreateZip(tempPath, progressCb, tempPath, files...)
	case ".tar.gz":
		err = services.CreateTarGz(tempPath, progressCb, tempPath, files...)
	}

	if err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("failed to create archive: %v", err)
	}

	// Get archive info
	stat, err := os.Stat(tempPath)
	if err != nil {
		_ = os.Remove(tempPath)
		return nil, fmt.Errorf("failed to stat archive: %v", err)
	}
	if streamProgress {
		if totalSize == 0 {
			totalSize = stat.Size()
		}
		processed = totalSize
		send("download_ready", 100)
	}

	// Determine archive name
	archiveName := "download" + extension
	if len(files) == 1 {
		base := filepath.Base(strings.TrimSuffix(files[0], string(os.PathSeparator)))
		if base != "" {
			archiveName = base + extension
		}
	}

	return map[string]any{
		"tempPath":    tempPath,
		"archiveName": archiveName,
		"size":        stat.Size(),
		"format":      algo,
	}, nil
}
