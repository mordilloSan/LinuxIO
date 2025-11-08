package filebrowser

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/adapters/fs/files"
	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/adapters/fs/fileutils"
	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/common/utils"
	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/indexing/iteminfo"
)

// validateMoveOperation checks if a move/rename operation is valid at the HTTP level
// It prevents moving a directory into itself or its subdirectories
func validateMoveOperation(src, dst string, isSrcDir bool) error {
	// Clean and normalize paths
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	// If source is a directory, check if destination is within source
	if isSrcDir {
		// Get the parent directory of the destination
		dstParent := filepath.Dir(dst)

		// Check if destination parent is the source directory or a subdirectory of it
		if strings.HasPrefix(dstParent+string(filepath.Separator), src+string(filepath.Separator)) || dstParent == src {
			return fmt.Errorf("cannot move directory '%s' to a location within itself: '%s'", src, dst)
		}
	}

	// Check if destination parent directory exists
	dstParent := filepath.Dir(dst)
	if dstParent != "." && dstParent != "/" {
		if _, err := os.Stat(dstParent); os.IsNotExist(err) {
			return fmt.Errorf("destination directory does not exist: '%s'", dstParent)
		}
	}

	return nil
}

type resourceStatData struct {
	Mode        string `json:"mode"`
	Owner       string `json:"owner"`
	Group       string `json:"group"`
	Size        int64  `json:"size"`
	Modified    string `json:"modified"`
	Raw         string `json:"raw"`
	Permissions string `json:"permissions"`
	Path        string `json:"path"`
	RealPath    string `json:"realPath"`
	Name        string `json:"name"`
}

// resourceGetHandler retrieves information about a resource.
// @Summary Get resource information
// @Description Returns metadata and optionally file contents for a specified resource path.
// @Tags Resources
// @Accept json
// @Produce json
// @Param path query string true "Path to the resource"
// @Param source query string true "Source name for the desired source, default is used if not provided"
// @Param content query string false "Include file content if true"
// @Success 200 {object} iteminfo.FileInfo "Resource metadata"
// @Failure 404 {object} map[string]string "Resource not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/resources [get]
func resourceGetHandler(c *gin.Context) {
	d, err := newRequestContext(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	encodedPath := c.Query("path")
	rawSource := c.Query("source")
	// Decode the URL-encoded path
	path, err := url.QueryUnescape(encodedPath)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid path encoding: %v", err)})
		return
	}
	if rawSource != "" {
		if _, err = url.QueryUnescape(rawSource); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid source encoding: %v", err)})
			return
		}
	}
	getContent := c.Query("content") == "true"
	fileInfo, err := files.FileInfoFaster(utils.FileOptions{
		Username: d.user.Username,
		Path:     path,
		Expand:   true,
		Content:  getContent,
	})
	if err != nil {
		logger.Debugf("error getting file info: %v", err)
		status := statusFromError(err)
		c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, fileInfo)
}

// resourceStatHandler returns extended metadata.
// @Summary Get extended file information
// @Description Gets all stats for the requested resource and returns the parsed output.
// @Tags Resources
// @Accept json
// @Produce json
// @Param path query string true "Path to the resource"
// @Param source query string true "Source name for the desired source, default is used if not provided"
// @Success 200 {object} resourceStatData "Extended resource metadata"
// @Failure 404 {object} map[string]string "Resource not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/resources/stat [get]
func resourceStatHandler(c *gin.Context) {
	d, err := newRequestContext(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	encodedPath := c.Query("path")
	rawSource := c.Query("source")

	path, err := url.QueryUnescape(encodedPath)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid path encoding: %v", err)})
		return
	}
	if rawSource != "" {
		if _, err = url.QueryUnescape(rawSource); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid source encoding: %v", err)})
			return
		}
	}

	fileInfo, err := files.FileInfoFaster(utils.FileOptions{
		Username: d.user.Username,
		Path:     path,
		Expand:   false,
	})
	if err != nil {
		status := statusFromError(err)
		logger.Debugf("error getting file stat info: %v", err)
		c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
		return
	}

	statData, err := collectStatInfo(fileInfo.RealPath)
	if err != nil {
		logger.Debugf("error collecting stat info: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	statData.Path = path
	statData.Name = fileInfo.Name
	if statData.Size == 0 {
		statData.Size = fileInfo.Size
	}

	c.JSON(http.StatusOK, statData)
}

// resourceDeleteHandler deletes a resource at a specified path.
// @Summary Delete a resource
// @Description Deletes a resource located at the specified path.
// @Tags Resources
// @Accept json
// @Produce json
// @Param path query string true "Path to the resource"
// @Param source query string true "Source name for the desired source, default is used if not provided"
// @Success 200 "Resource deleted successfully"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Resource not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/resources [delete]
func resourceDeleteHandler(c *gin.Context) {
	d, err := newRequestContext(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	encodedPath := c.Query("path")
	rawSource := c.Query("source")
	if rawSource != "" {
		if _, err = url.QueryUnescape(rawSource); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid source encoding: %v", err)})
			return
		}
	}
	// Decode the URL-encoded path
	path, err := url.QueryUnescape(encodedPath)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid path encoding: %v", err)})
		return
	}
	if path == "/" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "cannot delete root"})
		return
	}
	fileInfo, err := files.FileInfoFaster(utils.FileOptions{
		Username: d.user.Username,
		Path:     path,
		Expand:   false,
	})
	if err != nil {
		status := statusFromError(err)
		logger.Debugf("error getting file info: %v", err)
		c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
		return
	}

	err = files.DeleteFiles(fileInfo.RealPath)
	if err != nil {
		status := statusFromError(err)
		logger.Debugf("error deleting file: %v", err)
		c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

// resourcePostHandler creates or uploads a new resource.
// @Summary Create or upload a resource
// @Description Creates a new resource or uploads a file at the specified path. Supports file uploads and directory creation.
// @Tags Resources
// @Accept json
// @Produce json
// @Param path query string true "url encoded destination path where to place the files inside the destination source, a directory must end in / to create a directory"
// @Param source query string true "Name for the desired filebrowser destination source name, default is used if not provided"
// @Param override query bool false "Override existing file if true"
// @Success 200 "Resource created successfully"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Resource not found"
// @Failure 409 {object} map[string]string "Conflict - Resource already exists"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/resources [post]
func resourcePostHandler(c *gin.Context) {
	d, err := newRequestContext(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	path := c.Query("path")
	rawSource := c.Query("source")
	if rawSource != "" {
		if _, err = url.QueryUnescape(rawSource); err != nil {
			logger.Debugf("invalid source encoding: %v", err)
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid source encoding: %v", err)})
			return
		}
	}
	path, err = url.QueryUnescape(path)
	if err != nil {
		logger.Debugf("invalid path encoding: %v", err)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid path encoding: %v", err)})
		return
	}
	// Determine if this is a directory or file based on trailing slash
	isDir := strings.HasSuffix(path, "/")

	fileOpts := utils.FileOptions{
		Username: d.user.Username,
		Path:     path,
		Expand:   false,
	}
	// Direct filesystem access
	realPath := filepath.Join(path)

	// Check for file/folder conflicts before creation
	if stat, statErr := os.Stat(realPath); statErr == nil {
		// Path exists, check for type conflicts
		existingIsDir := stat.IsDir()
		requestingDir := isDir

		// If type mismatch (file vs folder or folder vs file) and not overriding
		if existingIsDir != requestingDir && c.Query("override") != "true" {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "resource already exists with different type"})
			return
		}
	}

	// Directories creation on POST.
	if isDir {
		err = files.WriteDirectory(fileOpts)
		if err != nil {
			logger.Debugf("error writing directory: %v", err)
			status := statusFromError(err)
			c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusOK)
		return
	}

	// Handle Chunked Uploads
	chunkOffsetStr := c.GetHeader("X-File-Chunk-Offset")
	if chunkOffsetStr != "" {
		var offset int64
		offset, err = strconv.ParseInt(chunkOffsetStr, 10, 64)
		if err != nil {
			logger.Debugf("invalid chunk offset: %v", err)
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid chunk offset: %v", err)})
			return
		}

		var totalSize int64
		totalSizeStr := c.GetHeader("X-File-Total-Size")
		totalSize, err = strconv.ParseInt(totalSizeStr, 10, 64)
		if err != nil {
			logger.Debugf("invalid total size: %v", err)
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid total size: %v", err)})
			return
		}
		// On the first chunk, check for conflicts or handle override
		if offset == 0 {
			// Check for file/folder conflicts for chunked uploads
			if stat, statErr := os.Stat(realPath); statErr == nil {
				existingIsDir := stat.IsDir()
				requestingDir := false // Files are never directories

				// If type mismatch (existing dir vs requesting file) and not overriding
				if existingIsDir != requestingDir && c.Query("override") != "true" {
					logger.Debugf("Type conflict detected in chunked: existing is dir=%v, requesting dir=%v at path=%v", existingIsDir, requestingDir, realPath)
					c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "resource already exists with different type"})
					return
				}
			}

			var fileInfo *iteminfo.ExtendedFileInfo
			fileInfo, err = files.FileInfoFaster(fileOpts)
			if err == nil { // File exists
				if c.Query("override") != "true" {
					logger.Debugf("resource already exists: %v", fileInfo.RealPath)
					c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "resource already exists"})
					return
				}
			}
		}

		// Use a temporary file in the cache directory for chunks.
		// Create a unique name for the temporary file to avoid collisions.
		hasher := md5.New()
		hasher.Write([]byte(realPath))
		uploadID := hex.EncodeToString(hasher.Sum(nil))
		tempFilePath := filepath.Join("tmp", "uploads", uploadID)

		if err = os.MkdirAll(filepath.Dir(tempFilePath), fileutils.PermDir); err != nil {
			logger.Debugf("could not create temp dir: %v", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("could not create temp dir: %v", err)})
			return
		}
		// Create or open the temporary file
		var outFile *os.File
		outFile, err = os.OpenFile(tempFilePath, os.O_CREATE|os.O_WRONLY, fileutils.PermFile)
		if err != nil {
			logger.Debugf("could not open temp file: %v", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("could not open temp file: %v", err)})
			return
		}
		defer outFile.Close()

		// Seek to the correct offset to write the chunk
		_, err = outFile.Seek(offset, 0)
		if err != nil {
			logger.Debugf("could not seek in temp file: %v", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("could not seek in temp file: %v", err)})
			return
		}

		// Write the request body (the chunk) to the file
		var chunkSize int64
		chunkSize, err = io.Copy(outFile, c.Request.Body)
		if err != nil {
			logger.Debugf("could not write chunk to temp file: %v", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("could not write chunk to temp file: %v", err)})
			return
		}
		// check if the file is complete
		if (offset + chunkSize) >= totalSize {
			// close file before moving
			outFile.Close()
			// Move the completed file from the temp location to the final destination
			err = fileutils.MoveFile(tempFilePath, realPath)
			if err != nil {
				logger.Debugf("could not move temp file to destination: %v", err)
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("could not move temp file to destination: %v", err)})
				return
			}
		}

		c.Status(http.StatusOK)
		return
	}

	// Check for file/folder conflicts for non-chunked uploads
	if stat, statErr := os.Stat(realPath); statErr == nil {
		existingIsDir := stat.IsDir()
		requestingDir := false // Files are never directories

		// If type mismatch (existing dir vs requesting file) and not overriding
		if existingIsDir != requestingDir && c.Query("override") != "true" {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "resource already exists with different type"})
			return
		}
	}

	if c.Query("override") != "true" {
		c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "resource already exists"})
		return
	}

	err = files.WriteFile(fileOpts, c.Request.Body)
	if err != nil {
		logger.Debugf("error writing file: %v", err)
		status := statusFromError(err)
		c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

// resourcePutHandler updates an existing file resource.
// @Summary Update a file resource
// @Description Updates an existing file at the specified path.
// @Tags Resources
// @Accept json
// @Produce json
// @Param path query string true "Destination path where to place the files inside the destination source"
// @Param source query string true "Source name for the desired source, default is used if not provided"
// @Success 200 "Resource updated successfully"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Resource not found"
// @Failure 405 {object} map[string]string "Method not allowed"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/resources [put]
func resourcePutHandler(c *gin.Context) {
	d, err := newRequestContext(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	rawSource := c.Query("source")
	if rawSource != "" {
		if _, err = url.QueryUnescape(rawSource); err != nil {
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid source encoding: %v", err)})
			return
		}
	}

	encodedPath := c.Query("path")

	// Decode the URL-encoded path
	path, err := url.QueryUnescape(encodedPath)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid path encoding: %v", err)})
		return
	}
	// Only allow PUT for files.
	if strings.HasSuffix(path, "/") {
		c.AbortWithStatusJSON(http.StatusMethodNotAllowed, gin.H{"error": "PUT is not allowed for directories"})
		return
	}

	fileOpts := utils.FileOptions{
		Username: d.user.Username,
		Path:     path,
		Expand:   false,
	}

	// Check access control for the target path
	err = files.WriteFile(fileOpts, c.Request.Body)
	status := statusFromError(err)
	if err != nil {
		logger.Debugf("error writing file: %v", err)
		c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
		return
	}
	c.Status(status)
}

// resourcePatchHandler performs a patch operation (e.g., move, rename) on a resource.
// @Summary Patch resource (move/rename)
// @Description Moves or renames a resource to a new destination.
// @Tags Resources
// @Accept json
// @Produce json
// @Param from query string true "Path from resource in <source_name>::<index_path> format"
// @Param destination query string true "Destination path for the resource"
// @Param action query string true "Action to perform (copy, rename)"
// @Param overwrite query bool false "Overwrite if destination exists"
// @Param rename query bool false "Rename if destination exists"
// @Success 200 "Resource moved/renamed successfully"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Resource not found"
// @Failure 409 {object} map[string]string "Conflict - Destination exists"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/resources [patch]
func resourcePatchHandler(c *gin.Context) {

	action := c.Query("action")

	encodedFrom := c.Query("from")
	// Decode the URL-encoded path
	src, err := url.QueryUnescape(encodedFrom)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid path encoding: %v", err)})
		return
	}
	dst := c.Query("destination")
	dst, err = url.QueryUnescape(dst)
	if err != nil {
		status := statusFromError(err)
		logger.Debugf("error unescaping destination: %v", err)
		c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
		return
	}

	splitSrc := strings.Split(src, "::")
	if len(splitSrc) <= 1 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid source path: %v", src)})
		return
	}
	src = splitSrc[1]

	splitDst := strings.Split(dst, "::")
	if len(splitDst) <= 1 {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid destination path: %v", dst)})
		return
	}
	dst = splitDst[1]

	if dst == "/" || src == "/" {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "cannot modify root directory"})
		return
	}

	// Direct filesystem access - check target dir exists
	parentDir := filepath.Join(filepath.Dir(dst))
	_, statErr := os.Stat(parentDir)
	if statErr != nil {
		logger.Debugf("could not get real path for parent dir: %v %v", filepath.Dir(dst), statErr)
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "parent directory not found"})
		return
	}
	realDest := filepath.Join(parentDir, filepath.Base(dst))

	realSrc := filepath.Join(src)
	stat, err := os.Stat(realSrc)
	if err != nil {
		logger.Debugf("could not stat source: %v", err)
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "source not found"})
		return
	}
	isSrcDir := stat.IsDir()

	// Check access control for both source and destination paths
	rename := c.Query("rename") == "true"
	if rename {
		realDest = addVersionSuffix(realDest)
	}

	// Validate move/rename operation to prevent circular references
	if action == "rename" || action == "move" {
		if err = validateMoveOperation(realSrc, realDest, isSrcDir); err != nil {
			logger.Debugf("invalid move operation: %v", err)
			c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}

	err = patchAction(patchActionParams{
		action:   action,
		src:      realSrc,
		dst:      realDest,
		isSrcDir: isSrcDir,
	})
	if err != nil {
		logger.Debugf("could not run patch action. src=%v dst=%v err=%v", realSrc, realDest, err)
		status := statusFromError(err)
		c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

func addVersionSuffix(source string) string {
	counter := 1
	dir, name := path.Split(source)
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for {
		if _, err := os.Stat(source); err != nil {
			break
		}
		renamed := fmt.Sprintf("%s(%d)%s", base, counter, ext)
		source = path.Join(dir, renamed)
		counter++
	}
	return source
}

type patchActionParams struct {
	action   string
	src      string
	dst      string
	isSrcDir bool
}

func patchAction(params patchActionParams) error {
	switch params.action {
	case "copy":
		err := files.CopyResource(params.isSrcDir, params.src, params.dst)
		if err != nil {
			logger.Debugf("error copying resource: %v", err)
		}
		return err
	case "rename", "move":
		err := files.MoveResource(params.isSrcDir, params.src, params.dst)
		if err != nil {
			logger.Debugf("error moving/renaming resource: %v", err)
		}
		return err
	default:
		err := fmt.Errorf("unsupported action: %s", params.action)
		logger.Debugf("unsupported patch action: %v", err)
		return err
	}
}

// statusFromError maps an error to an appropriate HTTP status code
func statusFromError(err error) int {
	if err == nil {
		return http.StatusOK
	}

	// Check for OS-level errors
	if os.IsPermission(err) {
		return http.StatusForbidden
	}
	if os.IsNotExist(err) {
		return http.StatusNotFound
	}
	if os.IsExist(err) {
		return http.StatusConflict
	}

	// Default to internal server error for unknown errors
	return http.StatusInternalServerError
}
