package filebrowser

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/services"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/server/web"
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
		"archive_create":  archiveCreate,
		"archive_extract": archiveExtract,
		"chmod":           resourceChmod,
		"users_groups":    usersGroups,
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
		err = services.CreateZip(archiveData, nil, archiveData, fileList...)
	} else {
		archiveData = archiveData + ".tar.gz"
		err = services.CreateTarGz(archiveData, nil, archiveData, fileList...)
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
	// Strip trailing slashes - indexer is sensitive to them
	normalized := strings.TrimRight(path, "/")
	// Ensure leading slash
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/" + normalized
	}
	return normalized
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

// archiveCreate builds an archive on disk from provided files.
// Args: [destinationPath, fileList, algo?]
func archiveCreate(args []string) (any, error) {
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

	progressKey := ""
	if len(args) > 3 && args[3] != "" {
		progressKey = args[3]
	}

	if progressKey != "" {
		defer web.GlobalProgressBroadcaster.Unregister(progressKey)
	}

	var progressCb services.ProgressCallback
	var totalSize int64
	var processed int64
	var lastPercent float64

	if progressKey != "" {
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
				web.GlobalProgressBroadcaster.Send(progressKey, web.ProgressUpdate{
					Type:           "compression_progress",
					Percent:        percent,
					BytesProcessed: processed,
					TotalBytes:     totalSize,
				})
			}

			web.GlobalProgressBroadcaster.Send(progressKey, web.ProgressUpdate{
				Type:           "compression_progress",
				Percent:        0,
				BytesProcessed: 0,
				TotalBytes:     totalSize,
			})
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

	if progressKey != "" {
		web.GlobalProgressBroadcaster.Send(progressKey, web.ProgressUpdate{
			Type:           "compression_complete",
			Percent:        100,
			BytesProcessed: totalSize,
			TotalBytes:     totalSize,
		})
	}

	return map[string]any{
		"path":   targetPath,
		"format": algo,
	}, nil
}

// archiveExtract extracts supported archives to a destination folder.
// Args: [archivePath, destination?]
func archiveExtract(args []string) (any, error) {
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

	if err := services.ExtractArchive(archivePath, destination); err != nil {
		logger.Debugf("error extracting archive: %v", err)
		return nil, fmt.Errorf("error extracting archive: %w", err)
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
