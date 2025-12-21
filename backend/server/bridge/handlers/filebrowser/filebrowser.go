package filebrowser

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/mordilloSan/LinuxIO/backend/server/bridge"
)

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
// NOTE: archiveCompressHandler removed - compression now uses yamux streams (fb-compress)
// NOTE: archiveExtractHandler removed - extraction now uses yamux streams (fb-extract)

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
