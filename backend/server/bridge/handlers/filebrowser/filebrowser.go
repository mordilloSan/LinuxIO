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

	// Set NDJSON streaming headers
	c.Header("Content-Type", "application/x-ndjson")
	c.Header("Transfer-Encoding", "chunked")

	// Stream responses from bridge
	err = bridge.CallWithSessionStream(sess, "filebrowser", "resource_get", args,
		func(chunk []byte) error {
			// Send chunk as NDJSON (one JSON object per line)
			if _, writeErr := c.Writer.Write(chunk); writeErr != nil {
				return writeErr
			}
			if _, writeErr := c.Writer.WriteString("\n"); writeErr != nil {
				return writeErr
			}
			c.Writer.Flush()
			return nil
		})

	if err != nil {
		logger.Debugf("bridge streaming error: %v", err)
		// Headers already sent, can't send error response
	}
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
			// TODO: Move the completed file from temp location to final destination
		}

		c.Status(http.StatusOK)
		return
	}

	c.Status(http.StatusOK)
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

	c.Status(http.StatusOK)
}

// rawFilesHandler serves raw file content or archives
func rawFilesHandler(c *gin.Context, fileList []string) {
	if len(fileList) == 0 || fileList[0] == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid files list"})
		return
	}

	firstFilePath := fileList[0]
	realPath := filepath.Join(firstFilePath)

	stat, err := os.Stat(realPath)
	if err != nil {
		logger.Debugf("error stating file: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "file not found"})
		return
	}

	isDir := stat.IsDir()

	// Single file download
	if len(fileList) == 1 && !isDir {
		fd, err := os.Open(realPath)
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

		fileName := filepath.Base(firstFilePath)
		setContentDisposition(c, fileName)
		c.Header("Cache-Control", "private")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))

		c.DataFromReader(http.StatusOK, fileInfo.Size(), "application/octet-stream", fd, map[string]string{})
		return
	}

	// For archives, create temp file and stream
	// This is a simplified version - in production you'd want proper error handling
	c.JSON(http.StatusOK, map[string]any{"message": "archive download not yet implemented"})
}

// setContentDisposition sets the Content-Disposition HTTP header for downloads
func setContentDisposition(c *gin.Context, fileName string) {
	if c.Query("inline") == "true" {
		c.Header("Content-Disposition", "inline; filename*=utf-8''"+url.PathEscape(fileName))
	} else {
		c.Header("Content-Disposition", "attachment; filename*=utf-8''"+url.PathEscape(fileName))
	}
}
