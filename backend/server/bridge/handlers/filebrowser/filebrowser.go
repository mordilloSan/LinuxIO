package filebrowser

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
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

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge/stream"
	"github.com/mordilloSan/LinuxIO/backend/server/web"
)

type archiveCompressRequest struct {
	Paths       []string `json:"paths"`
	Destination string   `json:"destination"`
	ArchiveName string   `json:"archiveName"`
	Format      string   `json:"format"`
	RequestID   string   `json:"requestId"`
	Override    bool     `json:"override"`
}

type archiveExtractRequest struct {
	ArchivePath string `json:"archivePath"`
	Destination string `json:"destination"`
	RequestID   string `json:"requestId"`
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

	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "filebrowser", "resource_get", args, &result); err != nil {
		logger.Debugf("bridge error: %v", err)
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
		errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, result)
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

	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "filebrowser", "resource_stat", []string{path}, &result); err != nil {
		logger.Debugf("bridge error: %v", err)
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
		errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, result)
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

	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "filebrowser", "dir_size", []string{path}, &result); err != nil {
		logger.Debugf("bridge error: %v", err)
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
		errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, result)
}

// subfoldersHandler gets direct child folders with sizes via bridge/indexer
func subfoldersHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	encodedPath := c.Query("path")
	path := "/"
	if encodedPath != "" {
		var err error
		path, err = url.QueryUnescape(encodedPath)
		if err != nil {
			logger.Debugf("invalid path encoding: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid path encoding"})
			return
		}
	}

	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "filebrowser", "subfolders", []string{path}, &result); err != nil {
		logger.Debugf("bridge error: %v", err)
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
		errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, result)
}

// searchHandler searches for files via bridge/indexer
func searchHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	query := c.Query("q")
	if strings.TrimSpace(query) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "search query is required"})
		return
	}

	limit := c.DefaultQuery("limit", "100")
	basePath := c.DefaultQuery("base", "/")

	args := []string{query, limit, basePath}
	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "filebrowser", "search", args, &result); err != nil {
		logger.Debugf("bridge error: %v", err)
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
		errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, result)
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

	if err := bridge.CallTypedWithSession(sess, "filebrowser", "resource_delete", []string{path}, nil); err != nil {
		logger.Debugf("bridge error: %v", err)
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
		errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
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
	requestID := strings.TrimSpace(c.Query("requestId"))

	// For directory creation, use bridge
	if isDir {
		args := []string{path}
		if override {
			args = append(args, "", "true")
		}

		if err := bridge.CallTypedWithSession(sess, "filebrowser", "resource_post", args, nil); err != nil {
			logger.Debugf("bridge error: %v", err)
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "bad_request:") {
				status = http.StatusBadRequest
			}
			errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
			errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
			c.JSON(status, gin.H{"error": errMsg})
			return
		}

		c.Status(http.StatusOK)
		return
	}

	// For file uploads, handle via temp file then bridge
	resourcePostViaTemp(c, sess, path, override, requestID)
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
	if err := bridge.CallTypedWithSession(sess, "filebrowser", "resource_patch", args, nil); err != nil {
		logger.Debugf("bridge error: %v", err)
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
		errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
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
	reqID := strings.TrimSpace(c.Query("reqId"))
	progressKey := ""
	if reqID != "" {
		progressKey = fmt.Sprintf("%s:%s", sess.SessionID, reqID)
	}

	// Handle via bridge - get temp file path
	rawFilesViaTemp(c, sess, fileList, progressKey)
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

	paths := sanitizeAbsPaths(req.Paths)
	if len(paths) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid paths"})
		return
	}
	req.Paths = paths

	format := req.Format
	if format == "" {
		format = "zip"
	}

	destDir := req.Destination
	if destDir == "" {
		destDir = filepath.Dir(req.Paths[0])
	}
	destDir, err := sanitizeAbsDir(destDir)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	archiveName := req.ArchiveName
	if archiveName == "" {
		archiveName = buildArchiveName(req.Paths, format)
	}
	archiveName = filepath.Base(archiveName)
	archiveName = ensureArchiveExtension(archiveName, format)

	destPath := filepath.Join(destDir, archiveName)
	if filepath.Dir(destPath) != destDir {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid destination"})
		return
	}
	if _, err := os.Stat(destPath); err == nil {
		if !req.Override {
			c.JSON(http.StatusConflict, gin.H{
				"error": fmt.Sprintf("archive already exists: %s", archiveName),
			})
			return
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		logger.Debugf("failed to stat destination: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": fmt.Sprintf("unable to validate destination: %v", err),
		})
		return
	}

	progressKey := ""
	if strings.TrimSpace(req.RequestID) != "" {
		progressKey = fmt.Sprintf("%s:%s", sess.SessionID, strings.TrimSpace(req.RequestID))
	}
	opCtx, finish := operationContext(c, progressKey, nil)
	defer finish()

	args := []string{
		destPath,
		strings.Join(req.Paths, "||"),
		format,
	}
	if req.Override {
		args = append(args, "true")
	} else {
		args = append(args, "false")
	}
	var result json.RawMessage
	if err := stream.CallWithProgress(opCtx, sess, "filebrowser", "archive_create", args, progressKey, &result); err != nil {
		logger.Debugf("bridge error: %v", err)
		if errors.Is(err, context.Canceled) {
			c.Status(499)
			return
		}
		status := http.StatusInternalServerError
		errMsg := err.Error()
		if strings.Contains(errMsg, "bridge error: bad_request:") {
			status = http.StatusBadRequest
			errMsg = strings.TrimPrefix(errMsg, "bridge error: bad_request:")
		} else {
			errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
		}
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, result)
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
	progressKey := ""
	if strings.TrimSpace(req.RequestID) != "" {
		progressKey = fmt.Sprintf("%s:%s", sess.SessionID, strings.TrimSpace(req.RequestID))
	}
	opCtx, finish := operationContext(c, progressKey, nil)
	defer finish()
	var result json.RawMessage
	if err := stream.CallWithProgress(opCtx, sess, "filebrowser", "archive_extract", args, progressKey, &result); err != nil {
		logger.Debugf("bridge error: %v", err)
		if errors.Is(err, context.Canceled) {
			c.Status(499)
			return
		}
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
		errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, result)
}

// ============================================================================
// TEMP FILE HANDLERS - Bridge-based operations using temp files
// ============================================================================

type uploadProgressTracker struct {
	key         string
	serverTotal int64
	serverDone  int64
}

func newUploadProgressTracker(key string, serverTotal int64) *uploadProgressTracker {
	if key == "" || serverTotal <= 0 {
		return nil
	}
	return &uploadProgressTracker{
		key:         key,
		serverTotal: serverTotal,
	}
}

func (t *uploadProgressTracker) emit(typ string) {
	if t == nil || t.serverTotal <= 0 {
		return
	}
	if t.serverDone > t.serverTotal {
		t.serverDone = t.serverTotal
	}
	percent := float64(t.serverDone) / float64(t.serverTotal) * 100
	if percent > 100 {
		percent = 100
	}

	web.GlobalProgressBroadcaster.Send(t.key, web.ProgressUpdate{
		Type:           typ,
		Percent:        percent,
		BytesProcessed: t.serverDone,
		TotalBytes:     t.serverTotal,
	})
}

func (t *uploadProgressTracker) addServerBytes(n int64) {
	if t == nil || n <= 0 {
		return
	}
	t.serverDone += n
	t.emit("upload_progress")
}

func operationContext(c *gin.Context, progressKey string, onCancel func()) (context.Context, func()) {
	ctx := c.Request.Context()
	if progressKey == "" {
		return ctx, func() {}
	}

	ctx, cancel := context.WithCancel(ctx)
	cancelFn := cancel
	if onCancel != nil {
		cancelFn = func() {
			onCancel()
			cancel()
		}
	}
	cleanup := web.GlobalOperationCanceller.Register(progressKey, cancelFn)
	return ctx, func() {
		cleanup()
		cancel()
	}
}

// resourcePostViaTemp handles file uploads via temp file then bridge
func resourcePostViaTemp(c *gin.Context, sess *session.Session, path string, override bool, requestID string) {
	// Handle Chunked Uploads
	chunkOffsetStr := c.GetHeader("X-File-Chunk-Offset")
	if chunkOffsetStr != "" {
		handleChunkedUpload(c, sess, path, override, requestID)
		return
	}

	if status, msg, ok := ensureUploadAllowed(sess, path, override); !ok {
		c.JSON(status, gin.H{"error": msg})
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

	progressKey := ""
	if requestID != "" {
		progressKey = fmt.Sprintf("%s:%s", sess.SessionID, requestID)
	}
	ctx, finish := operationContext(c, progressKey, func() {
		_ = c.Request.Body.Close()
	})
	defer finish()

	var tracker *uploadProgressTracker
	if progressKey != "" && c.Request.ContentLength > 0 {
		tracker = newUploadProgressTracker(progressKey, c.Request.ContentLength)
	}

	// Write request body to temp file
	if _, err := copyRequestBodyWithProgress(ctx, tempFile, c.Request.Body, tracker); err != nil {
		tempFile.Close()
		if errors.Is(err, context.Canceled) {
			c.Status(499)
			return
		}
		logger.Debugf("could not write to temp file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not write upload data"})
		return
	}
	tempFile.Close()

	// Call bridge to move temp file to final destination
	args := []string{tempPath, path}
	if override {
		args = append(args, "true")
	}

	if err := stream.CallWithProgress(ctx, sess, "filebrowser", "file_upload_from_temp", args, progressKey, nil); err != nil {
		logger.Debugf("bridge error: %v", err)
		if errors.Is(err, context.Canceled) {
			c.Status(499)
			return
		}
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "bad_request:") {
			status = http.StatusBadRequest
		}
		if strings.Contains(err.Error(), "already exists") {
			status = http.StatusConflict
		}
		errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
		errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.Status(http.StatusCreated)
}

// handleChunkedUpload handles chunked file uploads
func handleChunkedUpload(c *gin.Context, sess *session.Session, path string, override bool, requestID string) {
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

	progressKey := ""
	if requestID != "" {
		progressKey = fmt.Sprintf("%s:%s", sess.SessionID, requestID)
	}
	ctx, finish := operationContext(c, progressKey, func() {
		_ = c.Request.Body.Close()
	})
	defer finish()

	if offset == 0 {
		if status, msg, ok := ensureUploadAllowed(sess, path, override); !ok {
			c.JSON(status, gin.H{"error": msg})
			return
		}
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

	chunkSize, err := copyRequestBodyWithProgress(ctx, outFile, c.Request.Body, nil)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			c.Status(499)
			return
		}
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

		progressKey := ""
		if requestID != "" {
			progressKey = fmt.Sprintf("%s:%s", sess.SessionID, requestID)
		}
		if err := stream.CallWithProgress(ctx, sess, "filebrowser", "file_upload_from_temp", args, progressKey, nil); err != nil {
			logger.Debugf("bridge error: %v", err)
			if errors.Is(err, context.Canceled) {
				c.Status(499)
				return
			}
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "bad_request:") {
				status = http.StatusBadRequest
			}
			errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
			errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
			c.JSON(status, gin.H{"error": errMsg})
			return
		}
	}

	c.Status(http.StatusOK)
}

func copyRequestBodyWithProgress(ctx context.Context, dst *os.File, src io.Reader, tracker *uploadProgressTracker) (int64, error) {
	buf := make([]byte, 512*1024)
	var total int64
	for {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return total, ctx.Err()
			default:
			}
		}
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return total, werr
			}
			if tracker != nil {
				tracker.addServerBytes(int64(n))
			}
			total += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (cr *contextReader) Read(p []byte) (int, error) {
	if cr.ctx != nil {
		select {
		case <-cr.ctx.Done():
			return 0, cr.ctx.Err()
		default:
		}
	}
	return cr.reader.Read(p)
}

func ensureUploadAllowed(sess *session.Session, path string, override bool) (int, string, bool) {
	if override {
		return 0, "", true
	}

	var raw json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "filebrowser", "resource_stat", []string{path}, &raw); err != nil {
		if isNotExistBridgeError(err) {
			return 0, "", true
		}
		status := http.StatusInternalServerError
		errMsg := err.Error()
		if strings.Contains(errMsg, "bridge error: bad_request:") {
			status = http.StatusBadRequest
			errMsg = strings.TrimPrefix(errMsg, "bridge error: bad_request:")
		} else {
			errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
		}
		return status, errMsg, false
	}

	return http.StatusConflict, "file already exists", false
}

func isNotExistBridgeError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "no such file or directory") ||
		strings.Contains(lower, "does not exist") ||
		strings.Contains(lower, "not found")
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
	if err := bridge.CallTypedWithSession(sess, "filebrowser", "file_update_from_temp", args, nil); err != nil {
		logger.Debugf("bridge error: %v", err)
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
		errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

// rawFilesViaTemp handles downloads via temp files from bridge
func rawFilesViaTemp(c *gin.Context, sess *session.Session, fileList []string, progressKey string) {
	if len(fileList) == 0 || fileList[0] == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid files list"})
		return
	}
	ctx, finish := operationContext(c, progressKey, nil)
	defer finish()

	paths := parseFileList(fileList)
	if len(paths) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid files list"})
		return
	}

	var tempPath, fileName string
	var fileSize int64

	if len(paths) > 1 {
		// Multiple files or directory - create archive via bridge
		args := []string{strings.Join(paths, "||"), "zip"}
		var result struct {
			TempPath    string `json:"tempPath"`
			ArchiveName string `json:"archiveName"`
			Size        int64  `json:"size"`
		}
		if err := stream.CallWithProgress(ctx, sess, "filebrowser", "archive_download_setup", args, progressKey, &result); err != nil {
			logger.Debugf("bridge error: %v", err)
			if errors.Is(err, context.Canceled) {
				c.Status(499)
				return
			}
			status := http.StatusInternalServerError
			if strings.Contains(err.Error(), "bad_request:") {
				status = http.StatusBadRequest
			}
			errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
			errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
			c.JSON(status, gin.H{"error": errMsg})
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
		// Single file download via bridge; if it's a directory, fall back to archive_download_setup.
		args := []string{paths[0]}
		var result struct {
			TempPath string `json:"tempPath"`
			FileName string `json:"fileName"`
			Size     int64  `json:"size"`
		}
		if err := stream.CallWithProgress(ctx, sess, "filebrowser", "file_download_to_temp", args, progressKey, &result); err != nil {
			logger.Debugf("bridge error: %v", err)
			if errors.Is(err, context.Canceled) {
				c.Status(499)
				return
			}

			if strings.Contains(err.Error(), "path is a directory") {
				// Directory download -> build archive instead.
				args := []string{paths[0], "zip"}
				var archiveResult struct {
					TempPath    string `json:"tempPath"`
					ArchiveName string `json:"archiveName"`
					Size        int64  `json:"size"`
				}
				if downloadErr := stream.CallWithProgress(ctx, sess, "filebrowser", "archive_download_setup", args, progressKey, &archiveResult); downloadErr != nil {
					logger.Debugf("bridge error: %v", downloadErr)
					if errors.Is(downloadErr, context.Canceled) {
						c.Status(499)
						return
					}
					status := http.StatusInternalServerError
					if strings.Contains(downloadErr.Error(), "bad_request:") {
						status = http.StatusBadRequest
					}
					errMsg := strings.TrimPrefix(downloadErr.Error(), "bridge error: bad_request:")
					errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
					c.JSON(status, gin.H{"error": errMsg})
					return
				}
				if archiveResult.TempPath == "" || archiveResult.ArchiveName == "" {
					c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid response format"})
					return
				}
				tempPath = archiveResult.TempPath
				fileName = archiveResult.ArchiveName
				fileSize = archiveResult.Size
			} else {
				status := http.StatusInternalServerError
				if strings.Contains(err.Error(), "bad_request:") {
					status = http.StatusBadRequest
				}
				if strings.Contains(err.Error(), "not found") {
					status = http.StatusNotFound
				}
				errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
				errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
				c.JSON(status, gin.H{"error": errMsg})
				return
			}
		} else {
			if result.TempPath == "" || result.FileName == "" {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid response format"})
				return
			}

			tempPath = result.TempPath
			fileName = result.FileName
			fileSize = result.Size
		}
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

	reader := io.Reader(fd)
	if ctx != nil {
		reader = &contextReader{ctx: ctx, reader: fd}
	}
	c.DataFromReader(http.StatusOK, fileSize, contentType, reader, map[string]string{})
}

func parseFileList(entries []string) []string {
	return sanitizeAbsPaths(entries)
}

func sanitizeAbsPaths(entries []string) []string {
	var paths []string
	for _, raw := range entries {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		// Prefer treating the whole entry as a path (do not break absolute paths that contain "::").
		if clean, ok := sanitizeAbsPath(raw); ok {
			paths = append(paths, clean)
			continue
		}

		// Support legacy "label::/abs/path" entries.
		if strings.Contains(raw, "::") {
			parts := strings.SplitN(raw, "::", 2)
			if len(parts) == 2 {
				if clean, ok := sanitizeAbsPath(strings.TrimSpace(parts[1])); ok {
					paths = append(paths, clean)
					continue
				}
			}
		}
	}
	return paths
}

func sanitizeAbsPath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	if strings.ContainsRune(path, '\x00') {
		return "", false
	}

	clean := filepath.Clean(path)
	if clean == "." || clean == "" || !filepath.IsAbs(clean) {
		return "", false
	}
	return clean, true
}

func sanitizeAbsDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("destination is required")
	}
	if strings.ContainsRune(dir, '\x00') {
		return "", fmt.Errorf("invalid destination")
	}

	clean, ok := sanitizeAbsPath(dir)
	if !ok {
		return "", fmt.Errorf("invalid destination")
	}

	info, err := os.Stat(clean)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("destination directory not found")
		}
		return "", fmt.Errorf("unable to validate destination: %v", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("destination is not a directory")
	}

	return clean, nil
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

	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "filebrowser", "chmod", args, &result); err != nil {
		logger.Debugf("bridge error: %v", err)
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
		errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, result)
}

// usersGroupsHandler returns all users and groups on the system
func usersGroupsHandler(c *gin.Context) {
	sess := session.SessionFromContext(c)
	if sess == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no session"})
		return
	}

	var result json.RawMessage
	if err := bridge.CallTypedWithSession(sess, "filebrowser", "users_groups", []string{}, &result); err != nil {
		logger.Debugf("bridge error: %v", err)
		status := http.StatusInternalServerError
		if strings.Contains(err.Error(), "bad_request:") {
			status = http.StatusBadRequest
		}
		errMsg := strings.TrimPrefix(err.Error(), "bridge error: bad_request:")
		errMsg = strings.TrimPrefix(errMsg, "bridge error: ")
		c.JSON(status, gin.H{"error": errMsg})
		return
	}

	c.JSON(http.StatusOK, result)
}

// setContentDisposition sets the Content-Disposition HTTP header for downloads
func setContentDisposition(c *gin.Context, fileName string) {
	if c.Query("inline") == "true" {
		c.Header("Content-Disposition", "inline; filename*=utf-8''"+url.PathEscape(fileName))
	} else {
		c.Header("Content-Disposition", "attachment; filename*=utf-8''"+url.PathEscape(fileName))
	}
}
