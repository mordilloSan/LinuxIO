package filebrowser

import (
	"crypto/md5"
	"encoding/hex"
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

	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/services"
)

// ============================================================================
// HTTP HANDLERS - All resource endpoints consolidated here
// ============================================================================

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
	fileInfo, err := services.FileInfoFaster(iteminfo.FileOptions{
		Path:    path,
		Expand:  true,
		Content: getContent,
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
// @Success 200 {object} iteminfo.ResourceStatData "Extended resource metadata"
// @Failure 404 {object} map[string]string "Resource not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/resources/stat [get]
func resourceStatHandler(c *gin.Context) {
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

	fileInfo, err := services.FileInfoFaster(iteminfo.FileOptions{
		Path:   path,
		Expand: false,
	})
	if err != nil {
		status := statusFromError(err)
		logger.Debugf("error getting file stat info: %v", err)
		c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
		return
	}

	statData, err := iteminfo.CollectStatInfo(fileInfo.RealPath)
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
	encodedPath := c.Query("path")
	rawSource := c.Query("source")
	if rawSource != "" {
		if _, err := url.QueryUnescape(rawSource); err != nil {
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
	fileInfo, err := services.FileInfoFaster(iteminfo.FileOptions{
		Path:   path,
		Expand: false,
	})
	if err != nil {
		status := statusFromError(err)
		logger.Debugf("error getting file info: %v", err)
		c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
		return
	}

	err = services.DeleteFiles(fileInfo.RealPath)
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
	path := c.Query("path")
	path, err := url.QueryUnescape(path)
	if err != nil {
		logger.Debugf("invalid path encoding: %v", err)
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid path encoding: %v", err)})
		return
	}
	// Determine if this is a directory or file based on trailing slash
	isDir := strings.HasSuffix(path, "/")

	fileOpts := iteminfo.FileOptions{
		Path:   path,
		Expand: false,
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
		err = services.CreateDirectory(fileOpts)
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
			fileInfo, err = services.FileInfoFaster(fileOpts)
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

		if err = os.MkdirAll(filepath.Dir(tempFilePath), services.PermDir); err != nil {
			logger.Debugf("could not create temp dir: %v", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("could not create temp dir: %v", err)})
			return
		}
		// Create or open the temporary file
		var outFile *os.File
		outFile, err = os.OpenFile(tempFilePath, os.O_CREATE|os.O_WRONLY, services.PermFile)
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
			err = services.MoveFile(tempFilePath, realPath)
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

		// File exists with same type (both are files), check override
		if !existingIsDir && c.Query("override") != "true" {
			c.AbortWithStatusJSON(http.StatusConflict, gin.H{"error": "resource already exists"})
			return
		}
	}

	err = services.WriteContentInFile(fileOpts, c.Request.Body)
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
// @Param path query string true "Destination path where to place the files"
// @Success 200 "Resource updated successfully"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Resource not found"
// @Failure 405 {object} map[string]string "Method not allowed"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/resources [put]
func resourcePutHandler(c *gin.Context) {
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

	fileOpts := iteminfo.FileOptions{
		Path:   path,
		Expand: false,
	}

	// Check access control for the target path
	err = services.WriteContentInFile(fileOpts, c.Request.Body)
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
// @Param from query string true "Source path to move/rename from"
// @Param destination query string true "Destination path for the resource"
// @Param action query string true "Action to perform (copy, rename, move)"
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

	err = patchAction(patchActionParams{
		action: action,
		src:    realSrc,
		dst:    realDest,
	})
	if err != nil {
		logger.Debugf("could not run patch action. src=%v dst=%v err=%v", realSrc, realDest, err)
		status := statusFromError(err)
		c.AbortWithStatusJSON(status, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusOK)
}

// rawHandler serves the raw content of a file, multiple files, or directory in various formats.
// @Summary Get raw content of a file, multiple files, or directory
// @Description Returns the raw content of a file, multiple files, or a directory. Supports downloading files as archives in various formats.
// @Tags Resources
// @Accept json
// @Produce json
// @Param files query string true "a list of file paths separated by '||' (required)"
// @Param inline query bool false "If true, sets 'Content-Disposition' to 'inline'. Otherwise, defaults to 'attachment'."
// @Param algo query string false "Compression algorithm for archiving multiple files or directories. Options: 'zip' and 'tar.gz'. Default is 'zip'."
// @Success 200 {file} file "Raw file or directory content, or archive for multiple files"
// @Failure 400 {object} map[string]string "Invalid request path"
// @Failure 404 {object} map[string]string "File or directory not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /api/raw [get]
func rawHandler(c *gin.Context) {
	user, err := newSessionUser(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	files := c.Query("files")
	fileList := strings.Split(files, "||")
	rawFilesHandler(c, user, fileList)
}

func rawFilesHandler(c *gin.Context, user User, fileList []string) {
	var err error
	firstFilePath := fileList[0]
	fileName := filepath.Base(firstFilePath)
	// Direct filesystem access
	realPath := filepath.Join(firstFilePath)
	stat, err := os.Stat(realPath)
	if err != nil {
		logger.Debugf("error stating file: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	isDir := stat.IsDir()
	// Compute estimated download size
	estimatedSize, err := services.ComputeArchiveSize(fileList)
	if err != nil {
		logger.Debugf("error computing archive size: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// ** Single file download with Content-Length **
	if len(fileList) == 1 && !isDir {
		fd, err2 := os.Open(realPath)
		if err2 != nil {
			logger.Debugf("error opening file: %v", err2)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err2.Error()})
			return
		}
		defer fd.Close()

		// Get file size
		fileInfo, err2 := fd.Stat()
		if err2 != nil {
			logger.Debugf("error stating opened file: %v", err2)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err2.Error()})
			return
		}

		// Set headers
		setContentDisposition(c, fileName)
		c.Header("Cache-Control", "private")
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
		sizeInMB := estimatedSize / 1024 / 1024
		// if larger than 500 MB, log it
		if sizeInMB > 500 {
			logger.Debugf("user %v is downloading large (%d MB) file: %v", user.GetUsername(), sizeInMB, fileName)
		}
		// serve content allows for range requests.
		// video scrubbing, etc.
		c.DataFromReader(http.StatusOK, fileInfo.Size(), "application/octet-stream", fd, map[string]string{})
		return
	}

	// ** Archive (ZIP/TAR.GZ) handling **
	algo := c.Query("algo")
	var extension string
	switch algo {
	case "zip", "true", "":
		extension = ".zip"
	case "tar.gz":
		extension = ".tar.gz"
	default:
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "format not implemented"})
		return
	}

	baseDirName := filepath.Base(filepath.Dir(firstFilePath))
	if baseDirName == "" || baseDirName == "/" {
		baseDirName = "download"
	}
	if len(fileList) == 1 && isDir {
		baseDirName = filepath.Base(realPath)
	}
	fileName = url.PathEscape(baseDirName + extension)

	tempFile, err := os.CreateTemp("tmp", "archive-*")
	if err != nil {
		logger.Debugf("error creating temporary archive file: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
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
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// stream archive to response
	fd, err := os.Open(archiveData)
	if err != nil {
		logger.Debugf("error opening archive: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer fd.Close()

	// Get file size
	fileInfo, err := fd.Stat()
	if err != nil {
		os.Remove(archiveData) // Remove the file if stat fails
		logger.Debugf("error stating archive: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	sizeInMB := fileInfo.Size() / 1024 / 1024
	if sizeInMB > 500 {
		logger.Debugf("user %v is downloading large (%d MB) file: %v", user.GetUsername(), sizeInMB, fileName)
	}

	// Set headers AFTER computing actual archive size
	c.Header("Content-Disposition", "attachment; filename*=utf-8''"+fileName)
	c.Header("Content-Length", fmt.Sprintf("%d", fileInfo.Size()))
	c.Header("Content-Type", "application/octet-stream")

	// Stream the file
	_, err = io.Copy(c.Writer, fd)
	os.Remove(archiveData) // Remove the file after streaming
	if err != nil {
		logger.Errorf("failed to copy archive data to response: %v", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
}
