package filebrowser

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/adapters/fs/files"
	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/adapters/fs/fileutils"
	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/common/utils"
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

func addFile(path string, d *requestContext, tarWriter *tar.Writer, zipWriter *zip.Writer, flatten bool) error {
	// Direct filesystem access
	_, err := files.FileInfoFaster(utils.FileOptions{
		Username: d.user.Username,
		Path:     path,
		Expand:   false,
	})
	if err != nil {
		return err
	}
	realPath := filepath.Join(path)
	info, err := os.Stat(realPath)
	if err != nil {
		return err
	}

	// Get the base name of the top-level folder or file
	baseName := filepath.Base(realPath)

	if info.IsDir() {
		// Walk through directory contents
		return filepath.Walk(realPath, func(filePath string, fileInfo os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Calculate the relative path
			relPath, err := filepath.Rel(realPath, filePath) // Use realPath directly
			if err != nil {
				return err
			}

			// Normalize for tar: convert \ to /
			relPath = filepath.ToSlash(relPath)

			// Skip adding `.` (current directory)
			if relPath == "." {
				return nil
			}

			// Prepend base folder name unless flatten is true
			if !flatten {
				relPath = filepath.Join(baseName, relPath)
				relPath = filepath.ToSlash(relPath) // Ensure normalized separators
			}

			if fileInfo.IsDir() {
				if tarWriter != nil {
					header := &tar.Header{
						Name:     relPath + "/",
						Mode:     int64(fileutils.PermDir),
						Typeflag: tar.TypeDir,
						ModTime:  fileInfo.ModTime(),
					}
					return tarWriter.WriteHeader(header)
				}
				if zipWriter != nil {
					_, err := zipWriter.Create(relPath + "/")
					return err
				}
				return nil
			}
			return addSingleFile(filePath, relPath, zipWriter, tarWriter)
		})
	} else {
		// For a single file, use the base name as the archive path
		return addSingleFile(realPath, baseName, zipWriter, tarWriter)
	}
}

func addSingleFile(realPath, archivePath string, zipWriter *zip.Writer, tarWriter *tar.Writer) error {
	file, err := os.Open(realPath)
	if err != nil {
		// If we get "is a directory" error, this is likely a symlink to a directory
		// that wasn't properly detected. Skip it gracefully.
		if strings.Contains(err.Error(), "is a directory") {
			return nil
		}
		return err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return err
	}

	// Double-check if this is actually a directory (in case of symlinks)
	if info.IsDir() {
		return nil
	}

	if tarWriter != nil {
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(archivePath)
		if err = tarWriter.WriteHeader(header); err != nil {
			return err
		}
		_, err = io.Copy(tarWriter, file)
		return err
	}

	if zipWriter != nil {
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = archivePath
		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}
		_, err = io.Copy(writer, file)
		return err
	}

	return nil
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
	estimatedSize, err := computeArchiveSize(fileList)
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

	archiveData := filepath.Join("tmp", utils.InsecureRandomIdentifier(10))
	if extension == ".zip" {
		archiveData = archiveData + ".zip"
		err = createZip(d, archiveData, fileList...)
	} else {
		archiveData = archiveData + ".tar.gz"
		err = createTarGz(d, archiveData, fileList...)
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

func computeArchiveSize(fileList []string) (int64, error) {
	var estimatedSize int64
	for _, fname := range fileList {
		splitFile := strings.Split(fname, "::")
		if len(splitFile) != 2 {
			return http.StatusBadRequest, fmt.Errorf("invalid file in files request: %v", fileList[0])
		}
		source := splitFile[0]
		if source != "" {
			if _, err := url.PathUnescape(source); err != nil {
				return http.StatusBadRequest, fmt.Errorf("invalid source encoding: %v", err)
			}
		}
		path := splitFile[1]
		var err error
		// Direct filesystem access
		realPath := filepath.Join(source, path)
		stat, err := os.Stat(realPath)
		if err != nil {
			return 0, err
		}
		if stat.IsDir() {
			// For directories, recursively calculate size
			var dirSize int64
			err := filepath.Walk(realPath, func(path string, info os.FileInfo, walkErr error) error {
				if walkErr != nil {
					return nil // Skip errors
				}
				if !info.IsDir() {
					dirSize += info.Size()
				}
				return nil
			})
			if err != nil {
				return 0, err
			}
			estimatedSize += dirSize
		} else {
			estimatedSize += stat.Size()
		}
	}
	return estimatedSize, nil
}

func createZip(d *requestContext, tmpDirPath string, filenames ...string) error {
	file, err := os.OpenFile(tmpDirPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, fileutils.PermFile)
	if err != nil {
		return err
	}
	defer file.Close()

	zipWriter := zip.NewWriter(file)
	defer zipWriter.Close()

	for _, fname := range filenames {
		if addErr := addFile(fname, d, nil, zipWriter, false); addErr != nil {
			logger.Errorf("Failed to add %s to ZIP: %v", fname, addErr)
			return addErr
		}
	}

	// Explicitly set file permissions to bypass umask
	err = os.Chmod(tmpDirPath, fileutils.PermFile)
	if err != nil {
		return err
	}

	return nil
}

func createTarGz(d *requestContext, tmpDirPath string, filenames ...string) error {
	file, err := os.OpenFile(tmpDirPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, fileutils.PermFile)
	if err != nil {
		return err
	}
	defer file.Close()

	gzWriter := gzip.NewWriter(file)
	defer gzWriter.Close()
	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	for _, fname := range filenames {
		if addErr := addFile(fname, d, tarWriter, nil, false); addErr != nil {
			logger.Errorf("Failed to add %s to TAR.GZ: %v", fname, addErr)
			return addErr
		}
	}

	// Explicitly set file permissions to bypass umask
	err = os.Chmod(tmpDirPath, fileutils.PermFile)
	if err != nil {
		return err
	}

	return nil
}
