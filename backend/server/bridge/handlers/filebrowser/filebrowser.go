package filebrowser

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

	// For file uploads, stream via IPC to bridge
	resourcePostStreaming(c, sess, path, override)
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

	// Handle via IPC streaming
	resourcePutStreaming(c, sess, path)
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

	// Handle via IPC streaming
	rawFilesStreaming(c, sess, fileList)
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
// STREAMING HANDLERS - IPC chunking (no server filesystem operations)
// ============================================================================

// resourcePostStreaming handles file uploads by streaming chunks to bridge via IPC
func resourcePostStreaming(c *gin.Context, sess *session.Session, path string, override bool) {
	// Handle Chunked Uploads (large files)
	chunkOffsetStr := c.GetHeader("X-File-Chunk-Offset")
	if chunkOffsetStr != "" {
		handleChunkedUploadStreaming(c, sess, path, override)
		return
	}

	// Generate unique request ID for this upload
	requestID := uuid.New().String()

	// Read entire file into memory in chunks and send to bridge
	const chunkSize = 256 * 1024 // 256KB chunks
	buffer := make([]byte, chunkSize)
	offset := int64(0)

	for {
		n, err := c.Request.Body.Read(buffer)
		if n > 0 {
			// Validate chunk size doesn't exceed maximum
			if n > ipc.MaxChunkSize {
				logger.Debugf("chunk size %d exceeds maximum %d", n, ipc.MaxChunkSize)
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "chunk size exceeds maximum allowed"})
				return
			}

			// Encode chunk as base64
			payload := base64.StdEncoding.EncodeToString(buffer[:n])

			// Determine if this is the final chunk
			final := (err == io.EOF)

			// Build args: [destPath, override]
			args := []string{path}
			if override {
				args = append(args, "true")
			}

			// Send chunk to bridge via IPC streaming
			data, streamErr := bridge.CallWithSessionStreaming(
				sess,
				"filebrowser",
				"upload_chunk",
				args,
				requestID,
				offset,
				0, // Total unknown for non-chunked uploads
				payload,
				final,
			)

			if streamErr != nil {
				logger.Debugf("bridge streaming error: %v", streamErr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "upload failed"})
				return
			}

			// Parse response
			var resp ipc.Response
			if unmarshalErr := json.Unmarshal(data, &resp); unmarshalErr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
				return
			}

			if resp.Status != "ok" {
				status := http.StatusInternalServerError
				if strings.HasPrefix(resp.Error, "bad_request:") || strings.Contains(resp.Error, "already exists") {
					status = http.StatusBadRequest
				}
				c.JSON(status, gin.H{"error": resp.Error})
				return
			}

			// If final chunk, return success
			if final {
				c.JSON(http.StatusOK, resp.Output)
				return
			}

			offset += int64(n)
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Debugf("error reading request body: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read upload data"})
			return
		}
	}

	// No data read (empty file) – send a final empty chunk to create/touch the file.
	if offset == 0 {
		args := []string{path}
		if override {
			args = append(args, "true")
		}
		data, streamErr := bridge.CallWithSessionStreaming(
			sess,
			"filebrowser",
			"upload_chunk",
			args,
			requestID,
			0,
			0,
			"",
			true,
		)
		if streamErr != nil {
			logger.Debugf("bridge streaming error: %v", streamErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upload failed"})
			return
		}

		var resp ipc.Response
		if unmarshalErr := json.Unmarshal(data, &resp); unmarshalErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
			return
		}
		if resp.Status != "ok" {
			status := http.StatusInternalServerError
			if strings.HasPrefix(resp.Error, "bad_request:") || strings.Contains(resp.Error, "already exists") {
				status = http.StatusBadRequest
			}
			c.JSON(status, gin.H{"error": resp.Error})
			return
		}

		c.JSON(http.StatusOK, resp.Output)
	}
}

// handleChunkedUploadStreaming handles large file uploads with X-File-Chunk-* headers
func handleChunkedUploadStreaming(c *gin.Context, sess *session.Session, path string, override bool) {
	chunkOffsetStr := c.GetHeader("X-File-Chunk-Offset")
	totalSizeStr := c.GetHeader("X-File-Total-Size")

	offset, err := strconv.ParseInt(chunkOffsetStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid chunk offset"})
		return
	}

	totalSize, err := strconv.ParseInt(totalSizeStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid total size"})
		return
	}

	// Validate total size doesn't exceed maximum
	if totalSize > ipc.MaxFileSize {
		logger.Debugf("total size %d exceeds maximum %d", totalSize, ipc.MaxFileSize)
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "file size exceeds maximum allowed"})
		return
	}

	// Generate consistent requestID based on path hash (same as before)
	hasher := md5.New()
	hasher.Write([]byte(path))
	requestID := hex.EncodeToString(hasher.Sum(nil))

	// Read chunk data
	chunkData, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read chunk"})
		return
	}

	// Validate chunk size doesn't exceed maximum
	if len(chunkData) > ipc.MaxChunkSize {
		logger.Debugf("chunk size %d exceeds maximum %d", len(chunkData), ipc.MaxChunkSize)
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "chunk size exceeds maximum allowed"})
		return
	}

	// Encode as base64
	payload := base64.StdEncoding.EncodeToString(chunkData)

	// Determine if final chunk
	final := (offset + int64(len(chunkData))) >= totalSize

	// Build args
	args := []string{path}
	if override {
		args = append(args, "true")
	}

	// Send chunk to bridge
	data, err := bridge.CallWithSessionStreaming(
		sess,
		"filebrowser",
		"upload_chunk",
		args,
		requestID,
		offset,
		totalSize,
		payload,
		final,
	)

	if err != nil {
		logger.Debugf("bridge streaming error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "upload failed"})
		return
	}

	// Parse response
	var resp ipc.Response
	if unmarshalErr := json.Unmarshal(data, &resp); unmarshalErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
		return
	}

	if resp.Status != "ok" {
		status := http.StatusInternalServerError
		if strings.Contains(resp.Error, "already exists") {
			status = http.StatusBadRequest
		}
		c.JSON(status, gin.H{"error": resp.Error})
		return
	}

	// Return success
	c.JSON(http.StatusOK, resp.Output)
}

// resourcePutStreaming handles file updates by streaming to bridge via IPC
func resourcePutStreaming(c *gin.Context, sess *session.Session, path string) {
	// Generate unique request ID
	requestID := uuid.New().String()

	// Read and stream file content in chunks
	const chunkSize = 256 * 1024 // 256KB chunks
	buffer := make([]byte, chunkSize)
	offset := int64(0)

	for {
		n, err := c.Request.Body.Read(buffer)
		if n > 0 {
			// Validate chunk size doesn't exceed maximum
			if n > ipc.MaxChunkSize {
				logger.Debugf("chunk size %d exceeds maximum %d", n, ipc.MaxChunkSize)
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "chunk size exceeds maximum allowed"})
				return
			}

			// Encode chunk as base64
			payload := base64.StdEncoding.EncodeToString(buffer[:n])

			// Determine if this is the final chunk
			final := (err == io.EOF)

			// Build args: [destPath] (PUT always overrides)
			args := []string{path}

			// Send chunk to bridge via IPC streaming
			data, streamErr := bridge.CallWithSessionStreaming(
				sess,
				"filebrowser",
				"upload_chunk",
				args,
				requestID,
				offset,
				0, // Total unknown for PUT
				payload,
				final,
			)

			if streamErr != nil {
				logger.Debugf("bridge streaming error: %v", streamErr)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
				return
			}

			// Parse response
			var resp ipc.Response
			if errResponse := json.Unmarshal(data, &resp); errResponse != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
				return
			}

			if resp.Status != "ok" {
				status := http.StatusInternalServerError
				if strings.Contains(resp.Error, "not found") {
					status = http.StatusNotFound
				}
				c.JSON(status, gin.H{"error": resp.Error})
				return
			}

			// If final chunk, return success
			if final {
				c.JSON(http.StatusOK, resp.Output)
				return
			}

			offset += int64(n)
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Debugf("error reading request body: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read update data"})
			return
		}
	}

	// No data read (empty file) – send a final empty chunk to touch/replace the file.
	if offset == 0 {
		args := []string{path}
		data, streamErr := bridge.CallWithSessionStreaming(
			sess,
			"filebrowser",
			"upload_chunk",
			args,
			requestID,
			0,
			0,
			"",
			true,
		)

		if streamErr != nil {
			logger.Debugf("bridge streaming error: %v", streamErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "update failed"})
			return
		}

		var resp ipc.Response
		if unmarshalErr := json.Unmarshal(data, &resp); unmarshalErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
			return
		}

		if resp.Status != "ok" {
			status := http.StatusInternalServerError
			if strings.Contains(resp.Error, "not found") {
				status = http.StatusNotFound
			}
			c.JSON(status, gin.H{"error": resp.Error})
			return
		}

		c.JSON(http.StatusOK, resp.Output)
	}
}

// rawFilesStreaming handles file downloads by streaming chunks from bridge via IPC
func rawFilesStreaming(c *gin.Context, sess *session.Session, fileList []string) {
	if len(fileList) == 0 || fileList[0] == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid files list"})
		return
	}

	paths := parseFileList(fileList)
	if len(paths) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid files list"})
		return
	}

	// For multiple files or archives, still need to create archive via bridge first
	// Then stream the archive file
	var filePath, fileName string
	var totalSize int64

	if len(paths) > 1 {
		// Multiple files - create archive via bridge
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
			c.JSON(status, gin.H{"error": strings.TrimPrefix(resp.Error, "bad_request:")})
			return
		}

		// Extract temp path from response (bridge created archive)
		result, ok := resp.Output.(map[string]any)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid response format"})
			return
		}

		tempPath, _ := result["tempPath"].(string)
		archiveName, _ := result["archiveName"].(string)
		size, _ := result["size"].(float64)

		filePath = tempPath
		fileName = archiveName
		totalSize = int64(size)
	} else {
		// Single file
		filePath = paths[0]
		fileName = filepath.Base(filePath)

		// Get file size via stat
		args := []string{filePath}
		data, err := bridge.CallWithSession(sess, "filebrowser", "resource_stat", args)
		if err != nil {
			logger.Debugf("bridge stat error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to stat file"})
			return
		}

		var resp ipc.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid bridge response"})
			return
		}

		if resp.Status != "ok" {
			c.JSON(http.StatusNotFound, gin.H{"error": "file not found"})
			return
		}

		result, ok := resp.Output.(map[string]any)
		if ok {
			if sizeFloat, ok := result["size"].(float64); ok {
				totalSize = int64(sizeFloat)
			}
		}
	}

	// Set headers for file download
	contentType := "application/octet-stream"
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", fileName))
	c.Header("Content-Type", contentType)
	c.Header("Content-Length", fmt.Sprintf("%d", totalSize))

	// Stream file from bridge in chunks
	requestID := uuid.New().String()
	offset := int64(0)
	c.Status(http.StatusOK)

	for {
		// Request chunk from bridge
		args := []string{filePath}
		data, err := bridge.CallWithSessionStreaming(
			sess,
			"filebrowser",
			"download_chunk",
			args,
			requestID,
			offset,
			0,
			"",
			false,
		)

		if err != nil {
			logger.Debugf("bridge download chunk error: %v", err)
			return
		}

		var resp ipc.Response
		if err := json.Unmarshal(data, &resp); err != nil {
			logger.Debugf("invalid bridge response: %v", err)
			return
		}

		if resp.Status != "ok" {
			logger.Debugf("bridge error: %s", resp.Error)
			return
		}

		// Decode payload
		if resp.Payload != "" {
			chunkData, err := base64.StdEncoding.DecodeString(resp.Payload)
			if err != nil {
				logger.Debugf("base64 decode error: %v", err)
				return
			}

			// Write chunk to response
			if _, err := c.Writer.Write(chunkData); err != nil {
				logger.Debugf("write error: %v", err)
				return
			}

			offset += int64(len(chunkData))
		}

		// Check if final chunk
		if resp.Final {
			break
		}
	}

	c.Writer.Flush()
}

// ============================================================================
// HELPER FUNCTIONS
// ============================================================================

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
