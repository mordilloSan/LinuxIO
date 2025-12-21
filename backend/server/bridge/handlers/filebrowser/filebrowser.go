package filebrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
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

// resourcePostHandler creates a new directory.
// File uploads are handled via yamux streams (fb-upload).
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

	// Allow creating directories and empty files via HTTP POST
	// File uploads with content must use yamux streams (fb-upload)
	if !isDir && c.Request.ContentLength > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file uploads use stream protocol, not HTTP"})
		return
	}

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

// NOTE: rawHandler removed - downloads now use yamux streams (fb-download, fb-archive)

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
			if strings.Contains(strings.ToLower(errMsg), "destination exists") {
				status = http.StatusConflict
			}
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

// NOTE: HTTP upload handlers removed - uploads now use yamux streams (fb-upload)

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

// NOTE: rawFilesViaTemp removed - downloads now use yamux streams (fb-download, fb-archive)

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
