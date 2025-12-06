package filebrowser

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
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

	// For directory creation, use bridge
	if isDir {
		args := []string{path}
		if override {
			args = append(args, "", "true")
		}

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
		return
	}

	// For file uploads, handle via temp file then bridge
	resourcePostViaTemp(c, sess, path, override)
}

// resourcePutHandler updates an existing file resource
func resourcePutHandler(c *gin.Context) {
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

	if strings.HasSuffix(path, "/") {
		c.JSON(http.StatusMethodNotAllowed, gin.H{"error": "PUT is not allowed for directories"})
		return
	}

	// Handle via temp file then bridge
	resourcePutViaTemp(c, sess, path)
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
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	files := c.Query("files")
	fileList := strings.Split(files, "||")

	// Handle via bridge - get temp file path
	rawFilesViaTemp(c, sess, fileList)
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
// TEMP FILE HANDLERS - Bridge-based operations using temp files
// ============================================================================

// resourcePostViaTemp handles file uploads via temp file then bridge
func resourcePostViaTemp(c *gin.Context, sess *session.Session, path string, override bool) {
	// Handle Chunked Uploads
	chunkOffsetStr := c.GetHeader("X-File-Chunk-Offset")
	if chunkOffsetStr != "" {
		handleChunkedUpload(c, sess, path, override)
		return
	}

	// Create temp file for upload
	tempFile, err := os.CreateTemp("", "linuxio-upload-*.tmp")
	if err != nil {
		logger.Debugf("could not create temp file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create temp file"})
		return
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath) // Cleanup in case of error

	// Write request body to temp file
	_, err = io.Copy(tempFile, c.Request.Body)
	tempFile.Close()
	if err != nil {
		logger.Debugf("could not write to temp file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not write upload data"})
		return
	}

	// Call bridge to move temp file to final destination
	args := []string{tempPath, path}
	if override {
		args = append(args, "true")
	}

	data, err := bridge.CallWithSession(sess, "filebrowser", "file_upload_from_temp", args)
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
		if strings.Contains(resp.Error, "already exists") {
			status = http.StatusConflict
		}
		errMsg := strings.TrimPrefix(resp.Error, "bad_request:")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.Status(http.StatusCreated)
}

// handleChunkedUpload handles chunked file uploads
func handleChunkedUpload(c *gin.Context, sess *session.Session, path string, override bool) {
	chunkOffsetStr := c.GetHeader("X-File-Chunk-Offset")
	totalSizeStr := c.GetHeader("X-File-Total-Size")

	offset, err := strconv.ParseInt(chunkOffsetStr, 10, 64)
	if err != nil {
		logger.Debugf("invalid chunk offset: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chunk offset"})
		return
	}

	totalSize, err := strconv.ParseInt(totalSizeStr, 10, 64)
	if err != nil {
		logger.Debugf("invalid total size: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid total size"})
		return
	}

	// Use a temp file based on path hash
	hasher := md5.New()
	hasher.Write([]byte(path))
	uploadID := hex.EncodeToString(hasher.Sum(nil))
	tempFilePath := filepath.Join("tmp", "uploads", uploadID)

	err = os.MkdirAll(filepath.Dir(tempFilePath), 0755)
	if err != nil {
		logger.Debugf("could not create temp dir: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create temp directory"})
		return
	}

	outFile, err := os.OpenFile(tempFilePath, os.O_CREATE|os.O_WRONLY, 0644)
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

	chunkSize, err := io.Copy(outFile, c.Request.Body)
	if err != nil {
		logger.Debugf("could not write chunk to temp file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not write chunk to temp file"})
		return
	}

	// If upload is complete, move to final destination via bridge
	if (offset + chunkSize) >= totalSize {
		outFile.Close()

		args := []string{tempFilePath, path}
		if override {
			args = append(args, "true")
		}

		data, err := bridge.CallWithSession(sess, "filebrowser", "file_upload_from_temp", args)
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
	}

	c.Status(http.StatusOK)
}

// resourcePutViaTemp handles file updates via temp file then bridge
func resourcePutViaTemp(c *gin.Context, sess *session.Session, path string) {
	// Create temp file
	tempFile, err := os.CreateTemp("", "linuxio-update-*.tmp")
	if err != nil {
		logger.Debugf("could not create temp file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create temp file"})
		return
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath) // Cleanup in case of error

	// Write request body to temp file
	_, err = io.Copy(tempFile, c.Request.Body)
	tempFile.Close()
	if err != nil {
		logger.Debugf("could not write to temp file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not write update data"})
		return
	}

	// Call bridge to update file from temp
	args := []string{tempPath, path}
	data, err := bridge.CallWithSession(sess, "filebrowser", "file_update_from_temp", args)
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

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// rawFilesViaTemp handles downloads via temp files from bridge
func rawFilesViaTemp(c *gin.Context, sess *session.Session, fileList []string) {
	if len(fileList) == 0 || fileList[0] == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid files list"})
		return
	}

	paths := parseFileList(fileList)
	if len(paths) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid files list"})
		return
	}

	// Determine if this is a single file or needs archiving
	var needsArchive bool
	if len(paths) > 1 {
		needsArchive = true
	} else if len(paths) == 1 {
		// Check if single path is a directory
		// We'll let the bridge determine this
		needsArchive = false
	}

	var tempPath, fileName string
	var fileSize int64

	if needsArchive || len(paths) > 1 {
		// Multiple files or directory - create archive via bridge
		args := []string{strings.Join(paths, "||"), "zip"}
		data, err := bridge.CallWithSession(sess, "filebrowser", "archive_download_setup", args)
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

		var result struct {
			TempPath    string `json:"tempPath"`
			ArchiveName string `json:"archiveName"`
			Size        int64  `json:"size"`
		}
		if err := json.Unmarshal(resp.Output, &result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid response format"})
			return
		}
		if result.TempPath == "" || result.ArchiveName == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid response format"})
			return
		}

		tempPath = result.TempPath
		fileName = result.ArchiveName
		fileSize = result.Size
	} else {
		// Single file download via bridge
		args := []string{paths[0]}
		data, err := bridge.CallWithSession(sess, "filebrowser", "file_download_to_temp", args)
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
			if strings.Contains(resp.Error, "not found") {
				status = http.StatusNotFound
			}
			errMsg := strings.TrimPrefix(resp.Error, "bad_request:")
			c.JSON(status, gin.H{"error": errMsg})
			return
		}

		var result struct {
			TempPath string `json:"tempPath"`
			FileName string `json:"fileName"`
			Size     int64  `json:"size"`
		}
		if err := json.Unmarshal(resp.Output, &result); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid response format"})
			return
		}
		if result.TempPath == "" || result.FileName == "" {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid response format"})
			return
		}

		tempPath = result.TempPath
		fileName = result.FileName
		fileSize = result.Size
	}

	// Open temp file and stream to client
	defer os.Remove(tempPath)

	fd, err := os.Open(tempPath)
	if err != nil {
		logger.Debugf("error opening temp file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not open download file"})
		return
	}
	defer fd.Close()

	setContentDisposition(c, fileName)
	c.Header("Cache-Control", "private")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Length", fmt.Sprintf("%d", fileSize))

	contentType := "application/octet-stream"
	if strings.HasSuffix(fileName, ".zip") {
		contentType = "application/zip"
	}

	c.DataFromReader(http.StatusOK, fileSize, contentType, fd, map[string]string{})
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

// Helper functions for archive naming
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
