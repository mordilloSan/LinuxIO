package services

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

func closeWithLog(name string, closer io.Closer) {
	if err := closer.Close(); err != nil {
		slog.Debug("failed to close archive resource", "component", "filebrowser", "subsystem", "archive", "path", name, "error", err)
	}
}

func removeWithLog(root *fsroot.FSRoot, path string) {
	if err := root.Root.Remove(path); err != nil && !os.IsNotExist(err) {
		slog.Debug("failed to remove archive path", "component", "filebrowser", "subsystem", "archive", "path", path, "error", err)
	}
}

// ComputeArchiveSize calculates the estimated size of files/directories for archiving
func ComputeArchiveSize(fileList []string) (int64, error) {
	root, err := fsroot.Open()
	if err != nil {
		return 0, err
	}
	defer root.Close()

	var estimatedSize int64
	for _, fname := range fileList {
		realPath := cleanAbsPath(fname)
		stat, resolvedPath, err := statWithSymlinkResolution(root, realPath)
		if err != nil {
			return 0, err
		}

		if stat.IsDir() {
			dirSize, walkErr := estimateDirSize(root, resolvedPath)
			if walkErr != nil {
				return 0, walkErr
			}
			estimatedSize += dirSize
		} else {
			estimatedSize += stat.Size()
		}
	}

	return estimatedSize, nil
}

func estimateDirSize(root *fsroot.FSRoot, path string) (int64, error) {
	var total int64
	err := root.WalkDir(path, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return nil
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return nil
		}
		total += entryInfo.Size()
		return nil
	})
	return total, err
}

func resolveLinkTargetPath(linkPath, target string) string {
	if filepath.IsAbs(target) {
		return cleanAbsPath(target)
	}
	return cleanAbsPath(filepath.Join(filepath.Dir(linkPath), target))
}

func statWithSymlinkResolution(root *fsroot.FSRoot, path string) (os.FileInfo, string, error) {
	cleanPath := cleanAbsPath(path)

	info, err := root.Root.Stat(relPath(cleanPath))
	if err == nil {
		return info, cleanPath, nil
	}

	linkInfo, lstatErr := root.Root.Lstat(relPath(cleanPath))
	if lstatErr != nil || linkInfo.Mode()&os.ModeSymlink == 0 {
		return nil, cleanPath, err
	}

	target, readlinkErr := root.Root.Readlink(relPath(cleanPath))
	if readlinkErr != nil {
		return nil, cleanPath, readlinkErr
	}

	resolved := resolveLinkTargetPath(cleanPath, target)
	info, statErr := root.Root.Stat(relPath(resolved))
	if statErr != nil {
		return nil, cleanPath, statErr
	}

	return info, resolved, nil
}

// ComputeExtractSize estimates the number of bytes that will be written when extracting an archive.
func ComputeExtractSize(archivePath string) (int64, error) {
	root, err := fsroot.Open()
	if err != nil {
		return 0, err
	}
	defer root.Close()

	archivePath = cleanAbsPath(archivePath)
	lowerName := strings.ToLower(archivePath)

	switch {
	case strings.HasSuffix(lowerName, ".zip"):
		return computeZipExtractSize(root, archivePath)
	case strings.HasSuffix(lowerName, ".tar.gz"), strings.HasSuffix(lowerName, ".tgz"):
		return computeTarGzExtractSize(root, archivePath)
	default:
		return 0, ipc.ErrUnsupportedFormat
	}
}

func computeZipExtractSize(root *fsroot.FSRoot, archivePath string) (int64, error) {
	archiveFile, err := root.Root.Open(relPath(archivePath))
	if err != nil {
		return 0, err
	}
	defer archiveFile.Close()

	stat, err := archiveFile.Stat()
	if err != nil {
		return 0, err
	}

	reader, err := zip.NewReader(archiveFile, stat.Size())
	if err != nil {
		return 0, err
	}

	var total int64
	for _, file := range reader.File {
		if !file.FileInfo().IsDir() {
			total += int64(file.UncompressedSize64)
		}
	}
	return total, nil
}

func computeTarGzExtractSize(root *fsroot.FSRoot, archivePath string) (int64, error) {
	file, err := root.Root.Open(relPath(archivePath))
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
		if !header.FileInfo().IsDir() {
			total += header.Size
		}
	}
	return total, nil
}

// CreateZip creates a zip archive from the provided file list.
// skipPath allows excluding the archive itself if it lives inside the source tree.
// opts is optional - pass nil if callbacks are not needed.
func CreateZip(tmpDirPath string, opts *ipc.OperationCallbacks, skipPath string, filenames ...string) error {
	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	tmpDirPath = cleanAbsPath(tmpDirPath)
	skipPath = cleanAbsPath(skipPath)

	// Check for cancellation before creating file
	if opts.IsCancelled() {
		return ipc.ErrAborted
	}

	file, err := root.Root.OpenFile(relPath(tmpDirPath), os.O_RDWR|os.O_CREATE|os.O_TRUNC, PermFile)
	if err != nil {
		return err
	}
	fileOpen := true
	defer func() {
		if fileOpen {
			closeWithLog("zip output file", file)
		}
	}()

	zipWriter := zip.NewWriter(file)

	for _, fname := range filenames {
		if opts.IsCancelled() {
			closeWithLog("zip writer", zipWriter)
			closeWithLog("zip output file", file)
			fileOpen = false
			removeWithLog(root, relPath(tmpDirPath))
			return ipc.ErrAborted
		}
		if addErr := addFile(root, fname, nil, zipWriter, false, opts, skipPath); addErr != nil {
			closeWithLog("zip writer", zipWriter)
			closeWithLog("zip output file", file)
			fileOpen = false
			if addErr == ipc.ErrAborted {
				removeWithLog(root, relPath(tmpDirPath))
			} else {
				slog.Error("failed to add file to zip", "component", "filebrowser", "subsystem", "archive", "path", fname, "error", addErr)
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
	return root.Root.Chmod(relPath(tmpDirPath), PermFile)
}

// CreateTarGz creates a tar.gz archive from the provided file list.
// skipPath allows excluding the archive itself if it lives inside the source tree.
// opts is optional - pass nil if callbacks are not needed.
func CreateTarGz(tmpDirPath string, opts *ipc.OperationCallbacks, skipPath string, filenames ...string) error {
	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	tmpDirPath = cleanAbsPath(tmpDirPath)
	skipPath = cleanAbsPath(skipPath)

	// Check for cancellation before creating file
	if opts.IsCancelled() {
		return ipc.ErrAborted
	}

	file, err := root.Root.OpenFile(relPath(tmpDirPath), os.O_RDWR|os.O_CREATE|os.O_TRUNC, PermFile)
	if err != nil {
		return err
	}
	fileOpen := true
	defer func() {
		if fileOpen {
			closeWithLog("tar.gz output file", file)
		}
	}()

	gzWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzWriter)

	for _, fname := range filenames {
		if opts.IsCancelled() {
			closeWithLog("tar writer", tarWriter)
			closeWithLog("gzip writer", gzWriter)
			closeWithLog("tar.gz output file", file)
			fileOpen = false
			removeWithLog(root, relPath(tmpDirPath))
			return ipc.ErrAborted
		}
		if addErr := addFile(root, fname, tarWriter, nil, false, opts, skipPath); addErr != nil {
			closeWithLog("tar writer", tarWriter)
			closeWithLog("gzip writer", gzWriter)
			closeWithLog("tar.gz output file", file)
			fileOpen = false
			if addErr == ipc.ErrAborted {
				removeWithLog(root, relPath(tmpDirPath))
			} else {
				slog.Error("failed to add file to tar.gz", "component", "filebrowser", "subsystem", "archive", "path", fname, "error", addErr)
			}
			return addErr
		}
	}

	// Close writers in order: tar -> gzip -> file
	if err := tarWriter.Close(); err != nil {
		closeWithLog("gzip writer", gzWriter)
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
	return root.Root.Chmod(relPath(tmpDirPath), PermFile)
}

// ExtractArchive extracts supported archive types (zip, tar.gz, tgz) into the destination directory.
// opts is optional - pass nil if callbacks are not needed.
func ExtractArchive(archivePath, destination string, opts *ipc.OperationCallbacks) error {
	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	archivePath = cleanAbsPath(archivePath)
	destination = cleanAbsPath(destination)

	if err := root.Root.MkdirAll(relPath(destination), PermDir); err != nil {
		return err
	}
	if err := root.Root.Chmod(relPath(destination), PermDir); err != nil {
		return err
	}

	lowerName := strings.ToLower(archivePath)
	switch {
	case strings.HasSuffix(lowerName, ".zip"):
		return extractZip(root, archivePath, destination, opts)
	case strings.HasSuffix(lowerName, ".tar.gz"), strings.HasSuffix(lowerName, ".tgz"):
		return extractTarGz(root, archivePath, destination, opts)
	default:
		return ipc.ErrUnsupportedFormat
	}
}

func extractZip(root *fsroot.FSRoot, archivePath, destination string, opts *ipc.OperationCallbacks) error {
	archiveFile, err := root.Root.Open(relPath(archivePath))
	if err != nil {
		return err
	}
	defer archiveFile.Close()

	stat, err := archiveFile.Stat()
	if err != nil {
		return err
	}

	reader, err := zip.NewReader(archiveFile, stat.Size())
	if err != nil {
		return err
	}

	for _, file := range reader.File {
		if opts.IsCancelled() {
			return ipc.ErrAborted
		}
		if err := extractZipEntry(root, file, destination, opts); err != nil {
			return err
		}
	}

	return nil
}

func extractZipEntry(root *fsroot.FSRoot, file *zip.File, destination string, opts *ipc.OperationCallbacks) error {
	targetPath := filepath.Clean(filepath.Join(destination, file.Name))
	if !isWithinBase(destination, targetPath) {
		return fmt.Errorf("illegal file path in archive: %s", file.Name)
	}

	if file.FileInfo().IsDir() {
		if err := root.Root.MkdirAll(relPath(targetPath), PermDir); err != nil {
			return err
		}
		if err := root.Root.Chmod(relPath(targetPath), PermDir); err != nil {
			return err
		}
		opts.ReportComplete(targetPath)
		return nil
	}

	if err := root.Root.MkdirAll(relPath(filepath.Dir(targetPath)), PermDir); err != nil {
		return err
	}

	reader, err := file.Open()
	if err != nil {
		return err
	}
	defer reader.Close()

	writer, err := root.Root.OpenFile(relPath(targetPath), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, PermFile)
	if err != nil {
		return err
	}
	defer writer.Close()

	if err := copyWithCallbacks(writer, reader, opts); err != nil {
		return err
	}

	if err := root.Root.Chmod(relPath(targetPath), PermFile); err != nil {
		return err
	}
	opts.ReportComplete(targetPath)
	return nil
}

func extractTarGz(root *fsroot.FSRoot, archivePath, destination string, opts *ipc.OperationCallbacks) error {
	file, err := root.Root.Open(relPath(archivePath))
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

		if err := extractTarEntry(root, header, tarReader, destination, opts); err != nil {
			return err
		}
	}

	return nil
}

func extractTarEntry(root *fsroot.FSRoot, header *tar.Header, tarReader *tar.Reader, destination string, opts *ipc.OperationCallbacks) error {
	targetPath := filepath.Clean(filepath.Join(destination, header.Name))
	if !isWithinBase(destination, targetPath) {
		return fmt.Errorf("illegal file path in archive: %s", header.Name)
	}

	switch header.Typeflag {
	case tar.TypeDir:
		if err := root.Root.MkdirAll(relPath(targetPath), PermDir); err != nil {
			return err
		}
		if err := root.Root.Chmod(relPath(targetPath), PermDir); err != nil {
			return err
		}
		opts.ReportComplete(targetPath)
		return nil
	case tar.TypeReg:
		if err := root.Root.MkdirAll(relPath(filepath.Dir(targetPath)), PermDir); err != nil {
			return err
		}
		outFile, err := root.Root.OpenFile(relPath(targetPath), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, PermFile)
		if err != nil {
			return err
		}
		if err := copyWithCallbacks(outFile, tarReader, opts); err != nil {
			closeWithLog("extracted output file", outFile)
			return err
		}
		if err := outFile.Close(); err != nil {
			return err
		}
		if err := root.Root.Chmod(relPath(targetPath), PermFile); err != nil {
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
func addFile(root *fsroot.FSRoot, path string, tarWriter *tar.Writer, zipWriter *zip.Writer, flatten bool, opts *ipc.OperationCallbacks, skipPath string) error {
	realPath := cleanAbsPath(path)

	if skipPath != "" && filepath.Clean(realPath) == filepath.Clean(skipPath) {
		return nil
	}

	info, resolvedPath, err := statWithSymlinkResolution(root, realPath)
	if err != nil {
		return err
	}

	// Get the base name of the top-level folder or file
	baseName := filepath.Base(realPath)

	if info.IsDir() {
		return addDirectory(root, resolvedPath, baseName, tarWriter, zipWriter, flatten, opts, skipPath)
	}

	// For a single file, use the base name as the archive path
	return addSingleFile(root, realPath, baseName, zipWriter, tarWriter, opts)
}

func addDirectory(root *fsroot.FSRoot, resolvedPath, baseName string, tarWriter *tar.Writer, zipWriter *zip.Writer, flatten bool, opts *ipc.OperationCallbacks, skipPath string) error {
	rootWalkRel := relPath(resolvedPath)
	return root.WalkDir(resolvedPath, func(walkRel string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if opts.IsCancelled() {
			return ipc.ErrAborted
		}

		filePath := cleanAbsPath("/" + strings.TrimPrefix(walkRel, "/"))
		if shouldSkipArchivePath(rootWalkRel, walkRel, filePath, skipPath) {
			if entry.IsDir() && skipPath != "" && filepath.Clean(filePath) == filepath.Clean(skipPath) {
				return fs.SkipDir
			}
			return nil
		}

		relArchivePath, err := relativeArchivePath(resolvedPath, filePath, baseName, flatten)
		if err != nil {
			return err
		}
		if relArchivePath == "" {
			return nil
		}

		if entry.IsDir() {
			return addArchiveDirectoryEntry(entry, relArchivePath, tarWriter, zipWriter)
		}
		return addSingleFile(root, filePath, relArchivePath, zipWriter, tarWriter, opts)
	})
}

func shouldSkipArchivePath(rootWalkRel, walkRel, filePath, skipPath string) bool {
	if walkRel == rootWalkRel {
		return true
	}
	if skipPath == "" || filepath.Clean(filePath) != filepath.Clean(skipPath) {
		return false
	}
	return true
}

func relativeArchivePath(resolvedPath, filePath, baseName string, flatten bool) (string, error) {
	relArchivePath, err := filepath.Rel(resolvedPath, filePath)
	if err != nil {
		return "", err
	}

	relArchivePath = filepath.ToSlash(relArchivePath)
	if relArchivePath == "." {
		return "", nil
	}
	if flatten {
		return relArchivePath, nil
	}
	return filepath.ToSlash(filepath.Join(baseName, relArchivePath)), nil
}

func addArchiveDirectoryEntry(entry fs.DirEntry, relArchivePath string, tarWriter *tar.Writer, zipWriter *zip.Writer) error {
	entryInfo, err := entry.Info()
	if err != nil {
		return err
	}

	if tarWriter != nil {
		header := &tar.Header{
			Name:     relArchivePath + "/",
			Mode:     int64(PermDir),
			Typeflag: tar.TypeDir,
			ModTime:  entryInfo.ModTime(),
		}
		return tarWriter.WriteHeader(header)
	}
	if zipWriter != nil {
		_, err := zipWriter.Create(relArchivePath + "/")
		return err
	}
	return nil
}

// addSingleFile adds a single file to an archive
func addSingleFile(root *fsroot.FSRoot, realPath, archivePath string, zipWriter *zip.Writer, tarWriter *tar.Writer, opts *ipc.OperationCallbacks) error {
	openPath := cleanAbsPath(realPath)
	if _, resolvedPath, err := statWithSymlinkResolution(root, openPath); err == nil {
		openPath = resolvedPath
	}

	file, err := root.Root.Open(relPath(openPath))
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
