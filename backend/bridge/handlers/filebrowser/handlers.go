package filebrowser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func FilebrowserHandlers() map[string]ipc.HandlerFunc {
	return map[string]ipc.HandlerFunc{
		"resource_get":    resourceGet,
		"resource_stat":   resourceStat,
		"resource_delete": resourceDelete,
		"resource_post":   resourcePost,
		"resource_put":    resourcePut,
		"resource_patch":  resourcePatch,
		"raw_files":       rawFiles,
		"dir_size":        dirSize,
	}
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

// resourcePut updates an existing file resource
// Args: [path]
func resourcePut(args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("bad_request:missing path")
	}

	path := args[0]
	path, err := url.QueryUnescape(path)
	if err != nil {
		return nil, fmt.Errorf("bad_request:invalid path encoding")
	}

	if strings.HasSuffix(path, "/") {
		return nil, fmt.Errorf("bad_request:PUT is not allowed for directories")
	}

	// PUT also needs body streaming - will be handled at HTTP layer
	return nil, fmt.Errorf("bad_request:file update requires HTTP streaming")
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

	realDest := filepath.Join(parentDir, filepath.Base(dst))
	realSrc := filepath.Join(src)

	switch action {
	case "copy":
		err := services.CopyFile(realSrc, realDest)
		if err != nil {
			logger.Debugf("error copying resource: %v", err)
			return nil, fmt.Errorf("bad_request:%v", err)
		}
	case "rename", "move":
		err := services.MoveFile(realSrc, realDest)
		if err != nil {
			logger.Debugf("error moving/renaming resource: %v", err)
			return nil, fmt.Errorf("bad_request:%v", err)
		}
	default:
		return nil, fmt.Errorf("bad_request:unsupported action: %s", action)
	}

	return map[string]any{"message": "operation completed"}, nil
}

// rawFiles serves raw file content or archives
// Args: [files, inline?, algo?]
func rawFiles(args []string) (any, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("bad_request:missing files parameter")
	}

	fileList := strings.Split(args[0], "||")
	if len(fileList) == 0 || fileList[0] == "" {
		return nil, fmt.Errorf("bad_request:invalid files list")
	}

	firstFilePath := fileList[0]
	fileName := filepath.Base(firstFilePath)
	realPath := filepath.Join(firstFilePath)

	stat, err := os.Stat(realPath)
	if err != nil {
		logger.Debugf("error stating file: %v", err)
		return nil, fmt.Errorf("bad_request:file not found")
	}

	isDir := stat.IsDir()

	// Compute estimated download size
	estimatedSize, err := services.ComputeArchiveSize(fileList)
	if err != nil {
		logger.Debugf("error computing archive size: %v", err)
		return nil, fmt.Errorf("error computing archive size: %w", err)
	}

	// Single file download
	if len(fileList) == 1 && !isDir {
		fd, pathErr := os.Open(realPath)
		if pathErr != nil {
			logger.Debugf("error opening file: %v", pathErr)
			return nil, fmt.Errorf("error opening file: %w", pathErr)
		}
		defer fd.Close()

		fileInfo, fileErr := fd.Stat()
		if fileErr != nil {
			logger.Debugf("error stating opened file: %v", fileErr)
			return nil, fmt.Errorf("error stating opened file: %w", fileErr)
		}

		sizeInMB := estimatedSize / 1024 / 1024
		if sizeInMB > 500 {
			logger.Debugf("Downloading large (%d MB) file: %v", sizeInMB, fileName)
		}

		// Return file content as base64 or handle streaming at HTTP layer
		return map[string]any{
			"type":    "file",
			"name":    fileName,
			"size":    fileInfo.Size(),
			"path":    realPath,
			"content": "stream", // Signal to HTTP layer to stream
		}, nil
	}

	// Archive handling
	algo := "zip"
	if len(args) > 2 && args[2] != "" {
		algo = args[2]
	}

	var extension string
	switch algo {
	case "zip", "true", "":
		extension = ".zip"
	case "tar.gz":
		extension = ".tar.gz"
	default:
		return nil, fmt.Errorf("bad_request:format not implemented")
	}

	baseDirName := filepath.Base(filepath.Dir(firstFilePath))
	if baseDirName == "" || baseDirName == "/" {
		baseDirName = "download"
	}
	if len(fileList) == 1 && isDir {
		baseDirName = filepath.Base(realPath)
	}
	fileName = baseDirName + extension

	tempFile, err := os.CreateTemp("tmp", "archive-*")
	if err != nil {
		logger.Debugf("error creating temporary archive file: %v", err)
		return nil, fmt.Errorf("error creating temporary archive: %w", err)
	}
	tempName := tempFile.Name()
	tempFile.Close()
	_ = os.Remove(tempName)

	archiveData := tempName
	if extension == ".zip" {
		archiveData = archiveData + ".zip"
		err = services.CreateZip(archiveData, fileList...)
	} else {
		archiveData = archiveData + ".tar.gz"
		err = services.CreateTarGz(archiveData, fileList...)
	}
	if err != nil {
		logger.Debugf("error creating archive: %v", err)
		return nil, fmt.Errorf("error creating archive: %w", err)
	}

	fd, err := os.Open(archiveData)
	if err != nil {
		logger.Debugf("error opening archive: %v", err)
		return nil, fmt.Errorf("error opening archive: %w", err)
	}
	defer fd.Close()

	fileInfo, err := fd.Stat()
	if err != nil {
		os.Remove(archiveData)
		logger.Debugf("error stating archive: %v", err)
		return nil, fmt.Errorf("error stating archive: %w", err)
	}

	sizeInMB := fileInfo.Size() / 1024 / 1024
	if sizeInMB > 500 {
		logger.Debugf("Downloading large (%d MB) archive: %v", sizeInMB, fileName)
	}

	return map[string]any{
		"type":    "archive",
		"name":    fileName,
		"size":    fileInfo.Size(),
		"path":    archiveData,
		"content": "stream", // Signal to HTTP layer to stream
		"algo":    algo,
	}, nil
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
	return "/" + strings.Trim(path, "/")
}

// fetchDirSizeFromIndexer queries the indexer daemon over its Unix socket for a cached directory size.
func fetchDirSizeFromIndexer(path string) (int64, error) {
	normPath := normalizeIndexerPath(path)

	transport := &http.Transport{
		// Dial over the unix domain socket exposed by the indexer systemd service.
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := &net.Dialer{
				Timeout:   2 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return dialer.DialContext(ctx, "unix", "/var/run/indexer.sock")
		},
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://unix/dirsize", nil)
	if err != nil {
		return 0, fmt.Errorf("failed to build indexer request: %w", err)
	}
	q := req.URL.Query()
	q.Set("path", normPath)
	req.URL.RawQuery = q.Encode()

	resp, err := client.Do(req)
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
