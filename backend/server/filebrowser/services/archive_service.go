package services

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/go_logger/logger"
)

// ArchiveService handles archive creation and extraction operations
type ArchiveService struct {
	metadataService *MetadataService
}

// NewArchiveService creates a new archive service
func NewArchiveService() *ArchiveService {
	return &ArchiveService{
		metadataService: NewMetadataService(),
	}
}

// ComputeArchiveSize calculates the estimated size of files/directories for archiving
func (s *ArchiveService) ComputeArchiveSize(fileList []string) (int64, error) {
	var estimatedSize int64
	for _, fname := range fileList {
		path := fname
		var err error
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

// CreateZip creates a zip archive from the provided file list
func (s *ArchiveService) CreateZip(tmpDirPath string, filenames ...string) error {
	file, err := os.OpenFile(tmpDirPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, PermFile)
	if err != nil {
		return err
	}
	defer file.Close()

	zipWriter := zip.NewWriter(file)
	defer zipWriter.Close()

	for _, fname := range filenames {
		if addErr := s.addFile(fname, nil, zipWriter, false); addErr != nil {
			logger.Errorf("Failed to add %s to ZIP: %v", fname, addErr)
			return addErr
		}
	}

	// Explicitly set file permissions to bypass umask
	err = os.Chmod(tmpDirPath, PermFile)
	if err != nil {
		return err
	}

	return nil
}

// CreateTarGz creates a tar.gz archive from the provided file list
func (s *ArchiveService) CreateTarGz(tmpDirPath string, filenames ...string) error {
	file, err := os.OpenFile(tmpDirPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, PermFile)
	if err != nil {
		return err
	}
	defer file.Close()

	gzWriter := gzip.NewWriter(file)
	defer gzWriter.Close()
	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	for _, fname := range filenames {
		if addErr := s.addFile(fname, tarWriter, nil, false); addErr != nil {
			logger.Errorf("Failed to add %s to TAR.GZ: %v", fname, addErr)
			return addErr
		}
	}

	// Explicitly set file permissions to bypass umask
	err = os.Chmod(tmpDirPath, PermFile)
	if err != nil {
		return err
	}

	return nil
}

// addFile adds a file or directory to an archive (zip or tar.gz)
func (s *ArchiveService) addFile(path string, tarWriter *tar.Writer, zipWriter *zip.Writer, flatten bool) error {
	// Direct filesystem access
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
			return s.addSingleFile(filePath, relPath, zipWriter, tarWriter)
		})
	} else {
		// For a single file, use the base name as the archive path
		return s.addSingleFile(realPath, baseName, zipWriter, tarWriter)
	}
}

// addSingleFile adds a single file to an archive
func (s *ArchiveService) addSingleFile(realPath, archivePath string, zipWriter *zip.Writer, tarWriter *tar.Writer) error {
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
