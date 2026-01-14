package services

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/go_logger/v2/logger"

	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

// ComputeArchiveSize calculates the estimated size of files/directories for archiving
func ComputeArchiveSize(fileList []string) (int64, error) {
	var estimatedSize int64
	for _, fname := range fileList {
		path := fname
		// Direct filesystem access
		realPath := filepath.Join(path)
		stat, err := os.Stat(realPath)
		if err != nil {
			return 0, err
		}
		if stat.IsDir() {
			// For directories, recursively calculate size
			var dirSize int64
			err := filepath.Walk(realPath, func(path string, info os.FileInfo, walkErr error) error {
				_ = path // path not needed for size calculation
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

// ComputeExtractSize estimates the number of bytes that will be written when extracting an archive.
func ComputeExtractSize(archivePath string) (int64, error) {
	realPath := filepath.Join(archivePath)
	lowerName := strings.ToLower(realPath)

	switch {
	case strings.HasSuffix(lowerName, ".zip"):
		reader, err := zip.OpenReader(realPath)
		if err != nil {
			return 0, err
		}
		defer reader.Close()

		var total int64
		for _, file := range reader.File {
			if file.FileInfo().IsDir() {
				continue
			}
			total += int64(file.UncompressedSize64)
		}
		return total, nil

	case strings.HasSuffix(lowerName, ".tar.gz"), strings.HasSuffix(lowerName, ".tgz"):
		file, err := os.Open(realPath)
		if err != nil {
			return 0, err
		}
		defer file.Close()

		gzipReader, err := gzip.NewReader(file)
		if err != nil {
			return 0, err
		}
		defer gzipReader.Close()

		tarReader := tar.NewReader(gzipReader)
		var total int64
		for {
			header, err := tarReader.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return 0, err
			}
			if header.FileInfo().IsDir() {
				continue
			}
			total += header.Size
		}
		return total, nil

	default:
		return 0, ipc.ErrUnsupportedFormat
	}
}

// CreateZip creates a zip archive from the provided file list.
// skipPath allows excluding the archive itself if it lives inside the source tree.
// opts is optional - pass nil if callbacks are not needed.
func CreateZip(tmpDirPath string, opts *ipc.OperationCallbacks, skipPath string, filenames ...string) error {
	// Check for cancellation before creating file
	if opts.IsCancelled() {
		return ipc.ErrAborted
	}

	file, err := os.OpenFile(tmpDirPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, PermFile)
	if err != nil {
		return err
	}
	fileOpen := true
	defer func() {
		if fileOpen {
			file.Close()
		}
	}()

	zipWriter := zip.NewWriter(file)

	for _, fname := range filenames {
		if opts.IsCancelled() {
			zipWriter.Close()
			file.Close()
			fileOpen = false
			os.Remove(tmpDirPath) // Clean up partial archive
			return ipc.ErrAborted
		}
		if addErr := addFile(fname, nil, zipWriter, false, opts, skipPath); addErr != nil {
			logger.Errorf("Failed to add %s to ZIP: %v", fname, addErr)
			zipWriter.Close()
			file.Close()
			fileOpen = false
			if addErr == ipc.ErrAborted {
				os.Remove(tmpDirPath) // Clean up on abort
			}
			return addErr
		}
	}

	// Must close zip writer first to finalize archive (writes central directory)
	if err := zipWriter.Close(); err != nil {
		return err
	}

	// Then close and sync the file before returning
	if err := file.Close(); err != nil {
		return err
	}
	fileOpen = false

	// Set file permissions after closing
	return os.Chmod(tmpDirPath, PermFile)
}

// CreateTarGz creates a tar.gz archive from the provided file list.
// skipPath allows excluding the archive itself if it lives inside the source tree.
// opts is optional - pass nil if callbacks are not needed.
func CreateTarGz(tmpDirPath string, opts *ipc.OperationCallbacks, skipPath string, filenames ...string) error {
	// Check for cancellation before creating file
	if opts.IsCancelled() {
		return ipc.ErrAborted
	}

	file, err := os.OpenFile(tmpDirPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, PermFile)
	if err != nil {
		return err
	}
	fileOpen := true
	defer func() {
		if fileOpen {
			file.Close()
		}
	}()

	gzWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzWriter)

	for _, fname := range filenames {
		if opts.IsCancelled() {
			tarWriter.Close()
			gzWriter.Close()
			file.Close()
			fileOpen = false
			os.Remove(tmpDirPath) // Clean up partial archive
			return ipc.ErrAborted
		}
		if addErr := addFile(fname, tarWriter, nil, false, opts, skipPath); addErr != nil {
			logger.Errorf("Failed to add %s to TAR.GZ: %v", fname, addErr)
			tarWriter.Close()
			gzWriter.Close()
			file.Close()
			fileOpen = false
			if addErr == ipc.ErrAborted {
				os.Remove(tmpDirPath) // Clean up on abort
			}
			return addErr
		}
	}

	// Close writers in order: tar -> gzip -> file
	if err := tarWriter.Close(); err != nil {
		gzWriter.Close()
		return err
	}
	if err := gzWriter.Close(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	fileOpen = false

	// Set file permissions after closing
	return os.Chmod(tmpDirPath, PermFile)
}

// ExtractArchive extracts supported archive types (zip, tar.gz, tgz) into the destination directory.
// opts is optional - pass nil if callbacks are not needed.
func ExtractArchive(archivePath, destination string, opts *ipc.OperationCallbacks) error {
	archivePath = filepath.Join(archivePath)
	destination = filepath.Join(destination)

	if err := os.MkdirAll(destination, PermDir); err != nil {
		return err
	}
	if err := os.Chmod(destination, PermDir); err != nil {
		return err
	}

	lowerName := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lowerName, ".zip"):
		return extractZip(archivePath, destination, opts)
	case strings.HasSuffix(lowerName, ".tar.gz"), strings.HasSuffix(lowerName, ".tgz"):
		return extractTarGz(archivePath, destination, opts)
	default:
		return ipc.ErrUnsupportedFormat
	}
}

func extractZip(archivePath, destination string, opts *ipc.OperationCallbacks) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, file := range reader.File {
		if opts.IsCancelled() {
			return ipc.ErrAborted
		}
		if err := extractZipEntry(file, destination, opts); err != nil {
			return err
		}
	}

	return nil
}

func extractZipEntry(file *zip.File, destination string, opts *ipc.OperationCallbacks) error {
	targetPath := filepath.Join(destination, file.Name)
	if !isWithinBase(destination, targetPath) {
		return fmt.Errorf("illegal file path in archive: %s", file.Name)
	}

	if file.FileInfo().IsDir() {
		if err := os.MkdirAll(targetPath, PermDir); err != nil {
			return err
		}
		if err := os.Chmod(targetPath, PermDir); err != nil {
			return err
		}
		opts.ReportComplete(targetPath)
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), PermDir); err != nil {
		return err
	}

	reader, err := file.Open()
	if err != nil {
		return err
	}
	defer reader.Close()

	writer, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, PermFile)
	if err != nil {
		return err
	}
	defer writer.Close()

	if err := copyWithCallbacks(writer, reader, opts); err != nil {
		return err
	}

	if err := os.Chmod(targetPath, PermFile); err != nil {
		return err
	}
	opts.ReportComplete(targetPath)
	return nil
}

func extractTarGz(archivePath, destination string, opts *ipc.OperationCallbacks) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)

	for {
		if opts.IsCancelled() {
			return ipc.ErrAborted
		}

		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}

		if err := extractTarEntry(header, tarReader, destination, opts); err != nil {
			return err
		}
	}

	return nil
}

func extractTarEntry(header *tar.Header, tarReader *tar.Reader, destination string, opts *ipc.OperationCallbacks) error {
	targetPath := filepath.Join(destination, header.Name)
	if !isWithinBase(destination, targetPath) {
		return fmt.Errorf("illegal file path in archive: %s", header.Name)
	}

	switch header.Typeflag {
	case tar.TypeDir:
		if err := os.MkdirAll(targetPath, PermDir); err != nil {
			return err
		}
		if err := os.Chmod(targetPath, PermDir); err != nil {
			return err
		}
		opts.ReportComplete(targetPath)
		return nil
	case tar.TypeReg:
		if err := os.MkdirAll(filepath.Dir(targetPath), PermDir); err != nil {
			return err
		}
		outFile, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, PermFile)
		if err != nil {
			return err
		}
		if err := copyWithCallbacks(outFile, tarReader, opts); err != nil {
			outFile.Close()
			return err
		}
		if err := outFile.Close(); err != nil {
			return err
		}
		if err := os.Chmod(targetPath, PermFile); err != nil {
			return err
		}
		opts.ReportComplete(targetPath)
		return nil
	case tar.TypeSymlink, tar.TypeLink:
		// Skip symlinks/hardlinks for safety
		return nil
	default:
		return nil
	}
}

func isWithinBase(baseDir, targetPath string) bool {
	baseDir = filepath.Clean(baseDir)
	targetPath = filepath.Clean(targetPath)

	rel, err := filepath.Rel(baseDir, targetPath)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// addFile adds a file or directory to an archive (zip or tar.gz)
func addFile(path string, tarWriter *tar.Writer, zipWriter *zip.Writer, flatten bool, opts *ipc.OperationCallbacks, skipPath string) error {
	// Direct filesystem access
	realPath := filepath.Join(path)

	if skipPath != "" && filepath.Clean(realPath) == filepath.Clean(skipPath) {
		return nil
	}

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

			if opts.IsCancelled() {
				return ipc.ErrAborted
			}

			if skipPath != "" && filepath.Clean(filePath) == filepath.Clean(skipPath) {
				return nil
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
						Mode:     int64(PermDir),
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
			return addSingleFile(filePath, relPath, zipWriter, tarWriter, opts)
		})
	} else {
		// For a single file, use the base name as the archive path
		return addSingleFile(realPath, baseName, zipWriter, tarWriter, opts)
	}
}

// addSingleFile adds a single file to an archive
func addSingleFile(realPath, archivePath string, zipWriter *zip.Writer, tarWriter *tar.Writer, opts *ipc.OperationCallbacks) error {
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
		return copyWithCallbacks(tarWriter, file, opts)
	}

	if zipWriter != nil {
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = archivePath
		// Explicitly set compression method to Deflate for better compression
		header.Method = zip.Deflate
		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}
		return copyWithCallbacks(writer, file, opts)
	}

	return nil
}

// copyWithCallbacks writes from src to dst using the provided callbacks.
func copyWithCallbacks(dst io.Writer, src io.Reader, opts *ipc.OperationCallbacks) error {
	buf := make([]byte, 8*1024)
	for {
		if opts.IsCancelled() {
			return ipc.ErrAborted
		}
		n, rerr := src.Read(buf)
		if n > 0 {
			opts.ReportProgress(int64(n))
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}
