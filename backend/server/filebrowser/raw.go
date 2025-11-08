package filebrowser

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/services"
)

func setContentDisposition(c *gin.Context, fileName string) {
	if c.Query("inline") == "true" {
		c.Header("Content-Disposition", "inline; filename*=utf-8''"+url.PathEscape(fileName))
	} else {
		// As per RFC6266 section 4.3
		c.Header("Content-Disposition", "attachment; filename*=utf-8''"+url.PathEscape(fileName))
	}
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
	d, err := newRequestContext(c)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	files := c.Query("files")
	fileList := strings.Split(files, "||")
	rawFilesHandler(c, d, fileList)
}

func rawFilesHandler(c *gin.Context, d *requestContext, fileList []string) {
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
			logger.Debugf("user %v is downloading large (%d MB) file: %v", d.user.Username, sizeInMB, fileName)
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
		logger.Debugf("user %v is downloading large (%d MB) file: %v", d.user.Username, sizeInMB, fileName)
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
