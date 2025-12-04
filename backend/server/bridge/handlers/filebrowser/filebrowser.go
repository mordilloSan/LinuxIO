package filebrowser

import (
	"archive/zip"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
	"github.com/mordilloSan/LinuxIO/backend/server/web"
)

type archiveCompressRequest struct {
	Paths       []string `json:"paths"`
	Destination string   `json:"destination"`
	ArchiveName string   `json:"archiveName"`
	Format      string   `json:"format"`
	RequestID   string   `json:"requestId"`
}

type archiveExtractRequest struct {
	ArchivePath string `json:"archivePath"`
	Destination string `json:"destination"`
}

// resourceGetHandler retrieves information about a resource via bridge with streaming
func resourceGetHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	encodedPath := c.Query("path")
	path, err := url.QueryUnescape(encodedPath)
	if err != nil {
		logger.Debugf("invalid path encoding: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path encoding"})
		return
	}

	getContent := c.Query("content") == "true"

	args := []string{path}
	if getContent {
		args = append(args, "", "true")
	}

	data, err := bridge.CallWithSession(sess, "filebrowser", "resource_get", args)
	if err != nil {
		logger.Debugf("bridge error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed"})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}

	if resp.Status != "ok" {
		status := http.StatusInternalServerError
		if strings.HasPrefix(resp.Error, "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(resp.Error, "bad_request:")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp.Output)
}

// resourceStatHandler returns extended metadata via bridge
func resourceStatHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	encodedPath := c.Query("path")
	path, err := url.QueryUnescape(encodedPath)
	if err != nil {
		logger.Debugf("invalid path encoding: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path encoding"})
		return
	}

	data, err := bridge.CallWithSession(sess, "filebrowser", "resource_stat", []string{path})
	if err != nil {
		logger.Debugf("bridge error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed"})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}

	if resp.Status != "ok" {
		status := http.StatusInternalServerError
		if strings.HasPrefix(resp.Error, "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(resp.Error, "bad_request:")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp.Output)
}

// dirSizeHandler calculates directory size via bridge
func dirSizeHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	encodedPath := c.Query("path")
	path, err := url.QueryUnescape(encodedPath)
	if err != nil {
		logger.Debugf("invalid path encoding: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path encoding"})
		return
	}

	data, err := bridge.CallWithSession(sess, "filebrowser", "dir_size", []string{path})
	if err != nil {
		logger.Debugf("bridge error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed"})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}

	if resp.Status != "ok" {
		status := http.StatusInternalServerError
		if strings.HasPrefix(resp.Error, "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(resp.Error, "bad_request:")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp.Output)
}

// resourceDeleteHandler deletes a resource via bridge
func resourceDeleteHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	encodedPath := c.Query("path")
	path, err := url.QueryUnescape(encodedPath)
	if err != nil {
		logger.Debugf("invalid path encoding: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path encoding"})
		return
	}

	data, err := bridge.CallWithSession(sess, "filebrowser", "resource_delete", []string{path})
	if err != nil {
		logger.Debugf("bridge error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed"})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}

	if resp.Status != "ok" {
		status := http.StatusInternalServerError
		if strings.HasPrefix(resp.Error, "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(resp.Error, "bad_request:")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.Status(http.StatusOK)
}

// resourcePostHandler creates or uploads a new resource
// Note: File uploads are handled directly at HTTP layer due to streaming
func resourcePostHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	path := c.Query("path")
	path, err := url.QueryUnescape(path)
	if err != nil {
		logger.Debugf("invalid path encoding: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path encoding"})
		return
	}

	isDir := strings.HasSuffix(path, "/")
	override := c.Query("override") == "true"

	args := []string{path}
	if override {
		args = append(args, "", "true")
	}

	// For file uploads, we handle directly without bridge
	if !isDir {
		resourcePostDirectHandler(c)
		return
	}

	// For directory creation, use bridge
	data, err := bridge.CallWithSession(sess, "filebrowser", "resource_post", args)
	if err != nil {
		logger.Debugf("bridge error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed"})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}

	if resp.Status != "ok" {
		status := http.StatusInternalServerError
		if strings.HasPrefix(resp.Error, "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(resp.Error, "bad_request:")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.Status(http.StatusOK)
}

// resourcePutHandler updates an existing file resource
// Note: File updates need direct HTTP handling due to streaming
func resourcePutHandler(c *gin.Context) {
	resourcePutDirectHandler(c)
}

// resourcePatchHandler performs patch operations (move, copy, rename)
func resourcePatchHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	action := c.Query("action")
	encodedFrom := c.Query("from")
	src, err := url.QueryUnescape(encodedFrom)
	if err != nil {
		logger.Debugf("invalid path encoding: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path encoding"})
		return
	}

	dst := c.Query("destination")
	dst, err = url.QueryUnescape(dst)
	if err != nil {
		logger.Debugf("invalid destination path encoding: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid destination path encoding"})
		return
	}

	args := []string{action, src, dst}
	data, err := bridge.CallWithSession(sess, "filebrowser", "resource_patch", args)
	if err != nil {
		logger.Debugf("bridge error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed"})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}

	if resp.Status != "ok" {
		status := http.StatusInternalServerError
		if strings.HasPrefix(resp.Error, "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(resp.Error, "bad_request:")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.Status(http.StatusOK)
}

// rawHandler serves raw file content or archives
func rawHandler(c *gin.Context) {
	files := c.Query("files")
	fileList := strings.Split(files, "||")
	rawFilesHandler(c, fileList)
}

// archiveCompressHandler builds an archive from provided paths.
func archiveCompressHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	var req archiveCompressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	if len(req.Paths) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "paths are required"})
		return
	}

	format := req.Format
	if format == "" {
		format = "zip"
	}

	destDir := req.Destination
	if destDir == "" {
		destDir = filepath.Dir(req.Paths[0])
	}

	archiveName := req.ArchiveName
	if archiveName == "" {
		archiveName = buildArchiveName(req.Paths, format)
	}
	archiveName = filepath.Base(archiveName)
	archiveName = ensureArchiveExtension(archiveName, format)

	progressKey := ""
	if strings.TrimSpace(req.RequestID) != "" {
		progressKey = fmt.Sprintf("%s:%s", sess.SessionID, strings.TrimSpace(req.RequestID))
	}

	args := []string{
		filepath.Join(destDir, archiveName),
		strings.Join(req.Paths, "||"),
		format,
	}
	if progressKey != "" {
		args = append(args, progressKey)
	}

	data, err := bridge.CallWithSession(sess, "filebrowser", "archive_create", args)
	if err != nil {
		logger.Debugf("bridge error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed"})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}

	if resp.Status != "ok" {
		status := http.StatusInternalServerError
		if strings.HasPrefix(resp.Error, "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(resp.Error, "bad_request:")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp.Output)
}

// archiveExtractHandler extracts a supported archive to a destination.
func archiveExtractHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	var req archiveExtractRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	if strings.TrimSpace(req.ArchivePath) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "archivePath is required"})
		return
	}

	dest := req.Destination
	if dest == "" {
		dest = defaultExtractPath(req.ArchivePath)
	}

	args := []string{req.ArchivePath, dest}
	data, err := bridge.CallWithSession(sess, "filebrowser", "archive_extract", args)
	if err != nil {
		logger.Debugf("bridge error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed"})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}

	if resp.Status != "ok" {
		status := http.StatusInternalServerError
		if strings.HasPrefix(resp.Error, "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(resp.Error, "bad_request:")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp.Output)
}

// ============================================================================
// DIRECT HANDLERS - For operations requiring HTTP streaming
// ============================================================================

// resourcePostDirectHandler handles file uploads directly (requires streaming)
func resourcePostDirectHandler(c *gin.Context) {
	path := c.Query("path")
	path, err := url.QueryUnescape(path)
	if err != nil {
		logger.Debugf("invalid path encoding: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path encoding"})
		return
	}

	override := c.Query("override") == "true"
	realPath := filepath.Join(path)

	// Check for file/folder conflicts before creation
	if stat, statErr := os.Stat(realPath); statErr == nil {
		existingIsDir := stat.IsDir()
		requestingDir := false // POST for files

		if existingIsDir != requestingDir && !override {
			c.JSON(http.StatusConflict, gin.H{"error": "resource already exists with different type"})
			return
		}

		if !existingIsDir && !override {
			c.JSON(http.StatusConflict, gin.H{"error": "resource already exists"})
			return
		}
	}

	// Handle Chunked Uploads
	chunkOffsetStr := c.GetHeader("X-File-Chunk-Offset")
	if chunkOffsetStr != "" {
		var offset int64
		offset, err = strconv.ParseInt(chunkOffsetStr, 10, 64)
		if err != nil {
			logger.Debugf("invalid chunk offset: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chunk offset"})
			return
		}

		var totalSize int64
		totalSizeStr := c.GetHeader("X-File-Total-Size")
		totalSize, err = strconv.ParseInt(totalSizeStr, 10, 64)
		if err != nil {
			logger.Debugf("invalid total size: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid total size"})
			return
		}

		// On the first chunk, check for conflicts
		if offset == 0 {
			if stat, statErr := os.Stat(realPath); statErr == nil {
				existingIsDir := stat.IsDir()
				if existingIsDir && !override {
					c.JSON(http.StatusConflict, gin.H{"error": "resource already exists with different type"})
					return
				}
			}
		}

		// Use a temporary file in the cache directory for chunks
		hasher := md5.New()
		hasher.Write([]byte(realPath))
		uploadID := hex.EncodeToString(hasher.Sum(nil))
		tempFilePath := filepath.Join("tmp", "uploads", uploadID)

		if err = os.MkdirAll(filepath.Dir(tempFilePath), 0755); err != nil {
			logger.Debugf("could not create temp dir: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create temp directory"})
			return
		}

		var outFile *os.File
		outFile, err = os.OpenFile(tempFilePath, os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			logger.Debugf("could not open temp file: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not open temp file"})
			return
		}
		defer outFile.Close()

		_, err = outFile.Seek(offset, 0)
		if err != nil {
			logger.Debugf("could not seek in temp file: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not seek in temp file"})
			return
		}

		var chunkSize int64
		chunkSize, err = io.Copy(outFile, c.Request.Body)
		if err != nil {
			logger.Debugf("could not write chunk to temp file: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not write chunk to temp file"})
			return
		}

		// Check if the file is complete
		if (offset + chunkSize) >= totalSize {
			outFile.Close()
			if err := finalizeUpload(tempFilePath, realPath, override); err != nil {
				logger.Debugf("could not finalize chunked upload: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to finalize upload"})
				return
			}
		}

		c.Status(http.StatusOK)
		return
	}

	if err := writeFileFromBody(realPath, c.Request.Body, override); err != nil {
		logger.Debugf("could not create file: %v", err)
		// Check if it's a permission error
		if errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "permission denied") {
			c.JSON(http.StatusForbidden, gin.H{"error": "permission denied"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create file"})
		return
	}

	c.Status(http.StatusCreated)
}

func writeFileFromBody(path string, body io.ReadCloser, override bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	flags := os.O_CREATE | os.O_WRONLY
	if override {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_EXCL
	}

	file, err := os.OpenFile(path, flags, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	if body == nil {
		return nil
	}

	if _, err := io.Copy(file, body); err != nil {
		return err
	}

	return nil
}

func finalizeUpload(tempFilePath, realPath string, override bool) error {
	if err := os.MkdirAll(filepath.Dir(realPath), 0755); err != nil {
		return err
	}

	if !override {
		if _, err := os.Stat(realPath); err == nil {
			return fmt.Errorf("destination already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	if err := os.Rename(tempFilePath, realPath); err != nil {
		return err
	}

	return nil
}

// resourcePutDirectHandler handles direct file updates (streaming)
func resourcePutDirectHandler(c *gin.Context) {
	encodedPath := c.Query("path")
	path, err := url.QueryUnescape(encodedPath)
	if err != nil {
		logger.Debugf("invalid path encoding: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path encoding"})
		return
	}

	if strings.HasSuffix(path, "/") {
		c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "PUT is not allowed for directories"})
		return
	}

	// Read the entire request body
	content, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.Debugf("error reading request body: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "error reading request body"})
		return
	}

	// Write content to file
	if err := os.WriteFile(path, content, 0644); err != nil {
		logger.Debugf("error writing file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "error writing file"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// rawFilesHandler serves raw file content or archives
func rawFilesHandler(c *gin.Context, fileList []string) {
	if len(fileList) == 0 || fileList[0] == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid files list"})
		return
	}

	paths := parseFileList(fileList)
	if len(paths) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid files list"})
		return
	}

	// Single file download
	if len(paths) == 1 {
		first := paths[0]
		stat, err := os.Stat(first)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			logger.Debugf("error stating file: %v", err)
			c.JSON(status, gin.H{"error": "file not found"})
			return
		}

		if !stat.IsDir() {
			fd, err := os.Open(first)
			if err != nil {
				logger.Debugf("error opening file: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "could not open file"})
				return
			}
			defer fd.Close()

			fileInfo, err := fd.Stat()
			if err != nil {
				logger.Debugf("error stating opened file: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read file info"})
				return
			}

			fileName := filepath.Base(first)
			setContentDisposition(c, fileName)
			c.Header("Cache-Control", "private")
			c.Header("X-Content-Type-Options", "nosniff")
			c.Header("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

			c.DataFromReader(http.StatusOK, fileInfo.Size(), "application/octet-stream", fd, map[string]string{})
			return
		}
	}

	// Build a zip archive for directories or multiple files
	sess := session.SessionFromContext(c)
	reqId := c.Query("reqId")

	var archivePath, archiveName string
	var err error

	// If we have a session and reqId, use progress tracking
	if sess != nil && reqId != "" {
		key := sess.SessionID + ":" + reqId
		logger.Debugf("[FileBrowser] Building archive with progress tracking: %s", key)

		archivePath, archiveName, err = buildArchiveWithProgress(paths, func(bytesProcessed, totalBytes int64) {
			if totalBytes == 0 {
				return
			}
			percent := float64(bytesProcessed) / float64(totalBytes) * 100.0

			web.GlobalProgressBroadcaster.Send(key, web.ProgressUpdate{
				Type:           "download_progress",
				Percent:        percent,
				BytesProcessed: bytesProcessed,
				TotalBytes:     totalBytes,
			})
		})

		if err == nil {
			// Send ready event
			web.GlobalProgressBroadcaster.Send(key, web.ProgressUpdate{
				Type:    "download_ready",
				Percent: 100.0,
			})
		}
	} else {
		// No progress tracking
		archivePath, archiveName, err = buildArchive(paths)
	}

	if err != nil {
		logger.Debugf("failed to build archive: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to prepare download"})
		return
	}
	defer os.Remove(archivePath)

	archiveFile, err := os.Open(archivePath)
	if err != nil {
		logger.Debugf("error opening archive: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not open archive"})
		return
	}
	defer archiveFile.Close()

	info, err := archiveFile.Stat()
	if err != nil {
		logger.Debugf("error stating archive: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not read archive info"})
		return
	}

	setContentDisposition(c, archiveName)
	c.Header("Cache-Control", "private")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Length", fmt.Sprintf("%d", info.Size()))

	c.DataFromReader(http.StatusOK, info.Size(), "application/zip", archiveFile, map[string]string{})
}

func parseFileList(entries []string) []string {
	var paths []string
	for _, raw := range entries {
		if raw == "" {
			continue
		}
		parts := strings.SplitN(raw, "::", 2)
		path := parts[len(parts)-1]
		if path == "" {
			continue
		}
		paths = append(paths, filepath.Clean(path))
	}
	return paths
}

// ProgressCallback is called during archive building with bytes processed and total bytes
type ProgressCallback func(bytesProcessed, totalBytes int64)

// calculatePathSize calculates the total size of a file or directory
func calculatePathSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	if !info.IsDir() {
		return info.Size(), nil
	}

	total := int64(0)
	err = filepath.WalkDir(path, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		fileInfo, err2 := d.Info()
		if err2 != nil {
			return err2
		}
		total += fileInfo.Size()
		return nil
	})
	return total, err
}

// progressTrackingWriter wraps a zip.Writer and tracks bytes written
type progressTrackingWriter struct {
	writer         *zip.Writer
	bytesProcessed int64
	totalBytes     int64
	onProgress     ProgressCallback
	mu             sync.Mutex
}

func (ptw *progressTrackingWriter) addBytes(n int64) {
	ptw.mu.Lock()
	defer ptw.mu.Unlock()
	ptw.bytesProcessed += n
	if ptw.onProgress != nil && ptw.totalBytes > 0 {
		ptw.onProgress(ptw.bytesProcessed, ptw.totalBytes)
	}
}

// buildArchiveWithProgress builds a zip archive and reports progress
func buildArchiveWithProgress(paths []string, onProgress ProgressCallback) (string, string, error) {
	archiveFile, err := os.CreateTemp("", "linuxio-download-*.zip")
	if err != nil {
		return "", "", err
	}

	// Calculate total size first
	totalBytes := int64(0)
	for _, path := range paths {
		size, err := calculatePathSize(path)
		if err != nil {
			logger.Warnf("Failed to calculate size for %s: %v", path, err)
		}
		totalBytes += size
	}

	zipWriter := zip.NewWriter(archiveFile)
	tracker := &progressTrackingWriter{
		writer:     zipWriter,
		totalBytes: totalBytes,
		onProgress: onProgress,
	}

	for _, path := range paths {
		if err := addPathToArchiveWithProgress(tracker, path); err != nil {
			zipWriter.Close()
			archiveFile.Close()
			return "", "", err
		}
	}

	if err := zipWriter.Close(); err != nil {
		archiveFile.Close()
		return "", "", err
	}
	if err := archiveFile.Close(); err != nil {
		return "", "", err
	}

	zipName := "download.zip"
	if len(paths) == 1 {
		base := filepath.Base(strings.TrimSuffix(paths[0], string(os.PathSeparator)))
		if base != "" {
			zipName = fmt.Sprintf("%s.zip", base)
		}
	}

	return archiveFile.Name(), zipName, nil
}

func buildArchive(paths []string) (string, string, error) {
	return buildArchiveWithProgress(paths, nil)
}

func addPathToArchiveWithProgress(tracker *progressTrackingWriter, path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	baseName := filepath.Base(strings.TrimSuffix(path, string(os.PathSeparator)))
	if !info.IsDir() {
		return addFileToArchiveWithProgress(tracker, path, baseName)
	}

	// Ensure empty directories are preserved
	if err := addDirEntry(tracker.writer, baseName); err != nil {
		return err
	}

	return filepath.WalkDir(path, func(curr string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if curr == path {
			return nil
		}

		rel, err := filepath.Rel(path, curr)
		if err != nil {
			return err
		}

		entryPath := filepath.ToSlash(filepath.Join(baseName, rel))
		if d.IsDir() {
			return addDirEntry(tracker.writer, entryPath)
		}

		return addFileToArchiveWithProgress(tracker, curr, entryPath)
	})
}

func addDirEntry(zw *zip.Writer, name string) error {
	if name == "" {
		return nil
	}
	if !strings.HasSuffix(name, "/") {
		name += "/"
	}

	_, err := zw.Create(name)
	return err
}

func addFileToArchiveWithProgress(tracker *progressTrackingWriter, path string, zipPath string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.ToSlash(zipPath)
	header.Method = zip.Deflate

	writer, err := tracker.writer.CreateHeader(header)
	if err != nil {
		return err
	}

	fd, err := os.Open(path)
	if err != nil {
		return err
	}
	defer fd.Close()

	// Copy with progress tracking
	written, err := io.Copy(writer, fd)
	if err != nil {
		return err
	}

	// Report progress
	tracker.addBytes(written)
	return nil
}

func buildArchiveName(paths []string, format string) string {
	if len(paths) == 0 {
		return ensureArchiveExtension("archive", format)
	}

	name := filepath.Base(strings.TrimRight(paths[0], "/"))
	if name == "" || name == "." || name == "/" {
		name = "archive"
	}
	if len(paths) > 1 {
		name = "archive"
	}
	return ensureArchiveExtension(name, format)
}

func ensureArchiveExtension(name, format string) string {
	lower := strings.ToLower(name)
	switch format {
	case "tar.gz":
		if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
			return name
		}
		name = strings.TrimSuffix(name, ".tar")
		name = strings.TrimSuffix(name, ".gz")
		return name + ".tar.gz"
	default:
		if strings.HasSuffix(lower, ".zip") {
			return name
		}
		return name + ".zip"
	}
}

func defaultExtractPath(archivePath string) string {
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

// chmodHandler changes file or directory permissions
func chmodHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	type chmodRequest struct {
		Path      string `json:"path"`
		Mode      string `json:"mode"`
		Owner     string `json:"owner"`
		Group     string `json:"group"`
		Recursive bool   `json:"recursive"`
	}

	var req chmodRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request payload"})
		return
	}

	if strings.TrimSpace(req.Path) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "path is required"})
		return
	}

	if strings.TrimSpace(req.Mode) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mode is required"})
		return
	}

	args := []string{req.Path, req.Mode, req.Owner, req.Group}
	if req.Recursive {
		args = append(args, "true")
	}

	data, err := bridge.CallWithSession(sess, "filebrowser", "chmod", args)
	if err != nil {
		logger.Debugf("bridge error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed"})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}

	if resp.Status != "ok" {
		status := http.StatusInternalServerError
		if strings.HasPrefix(resp.Error, "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(resp.Error, "bad_request:")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp.Output)
}

// usersGroupsHandler returns all users and groups on the system
func usersGroupsHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	data, err := bridge.CallWithSession(sess, "filebrowser", "users_groups", []string{})
	if err != nil {
		logger.Debugf("bridge error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "bridge call failed"})
		return
	}

	var resp ipc.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}

	if resp.Status != "ok" {
		status := http.StatusInternalServerError
		if strings.HasPrefix(resp.Error, "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(resp.Error, "bad_request:")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	if resp.Output == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "empty bridge output"})
		return
	}

	c.JSON(http.StatusOK, resp.Output)
}

// setContentDisposition sets the Content-Disposition HTTP header for downloads
func setContentDisposition(c *gin.Context, fileName string) {
	if c.Query("inline") == "true" {
		c.Header("Content-Disposition", "inline; filename*=utf-8''"+url.PathEscape(fileName))
	} else {
		c.Header("Content-Disposition", "attachment; filename*=utf-8''"+url.PathEscape(fileName))
	}
}
