package services

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
	"github.com/mordilloSan/LinuxIO/backend/common/ipc"
)

var (
	PermFile os.FileMode = 0o664 // rw-rw-r-- (owner read+write, group read+write, rest read)
	PermDir  os.FileMode = 0o775 // rwxrwxr-x (owner read+write+execute, group read+write+execute, rest read+execute)
)

func cleanAbsPath(p string) string {
	if p == "" {
		return "/"
	}
	return filepath.Clean("/" + strings.TrimPrefix(p, "/"))
}

func relPath(p string) string {
	return fsroot.ToRel(cleanAbsPath(p))
}

func readDir(root *fsroot.FSRoot, dirPath string) ([]os.DirEntry, error) {
	dir, err := root.Root.Open(relPath(dirPath))
	if err != nil {
		return nil, err
	}
	defer dir.Close()

	entries, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// MoveFile moves a file from src to dst.
// By default, the rename system call is used. If src and dst point to different volumes,
// the file copy is used as a fallback.
func MoveFile(src, dst string, overwrite bool) error {
	src = cleanAbsPath(src)
	dst = cleanAbsPath(dst)

	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	// Validate the move operation before executing
	if validateErr := validateMoveDestination(root, src, dst); validateErr != nil {
		return validateErr
	}

	srcInfo, err := root.Root.Stat(relPath(src))
	if err != nil {
		return err
	}

	if destErr := prepareDestination(root, srcInfo.IsDir(), dst, overwrite); destErr != nil {
		return destErr
	}

	err = root.Root.Rename(relPath(src), relPath(dst))
	if err == nil {
		return nil
	}

	// fallback
	err = copyWithRoot(root, src, dst, overwrite)
	if err != nil {
		slog.Error("copy fallback failed", "component", "filebrowser", "subsystem", "file_operations", "path", src, "destination", dst, "error", err)
		return err
	}

	go func(removePath string) {
		asyncRoot, openErr := fsroot.Open()
		if openErr != nil {
			slog.Error("failed to open root during async remove", "component", "filebrowser", "subsystem", "file_operations", "error", openErr)
			return
		}
		defer asyncRoot.Close()

		if removeErr := asyncRoot.Root.RemoveAll(relPath(removePath)); removeErr != nil {
			slog.Error("failed to remove source after fallback copy", "component", "filebrowser", "subsystem", "file_operations", "path", removePath, "error", removeErr)
		}
	}(src)

	return nil
}

// CopyFile copies a file or directory from source to dest and returns an error if any.
// It handles both files and directories, copying recursively as needed.
func CopyFile(source, dest string, overwrite bool) error {
	source = cleanAbsPath(source)
	dest = cleanAbsPath(dest)

	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	return copyWithRoot(root, source, dest, overwrite)
}

func copyWithRoot(root *fsroot.FSRoot, source, dest string, overwrite bool) error {
	// Validate the copy operation before executing
	if err := validateMoveDestination(root, source, dest); err != nil {
		return err
	}

	// Check if the source exists and whether it's a file or directory.
	info, err := root.Root.Stat(relPath(source))
	if err != nil {
		return err
	}

	if err := prepareDestination(root, info.IsDir(), dest, overwrite); err != nil {
		return err
	}

	if info.IsDir() {
		// If the source is a directory, copy it recursively.
		return copyDirectory(root, source, dest)
	}

	// If the source is a file, copy the file.
	return copySingleFile(root, source, dest)
}

// copySingleFile handles copying a single file.
func copySingleFile(root *fsroot.FSRoot, source, dest string) error {
	// Open the source file.
	src, err := root.Root.Open(relPath(source))
	if err != nil {
		return err
	}
	defer src.Close()

	// Create the destination directory if needed.
	err = root.Root.MkdirAll(relPath(filepath.Dir(dest)), PermDir)
	if err != nil {
		return err
	}

	// Create the destination file.
	dst, err := root.Root.OpenFile(relPath(dest), os.O_RDWR|os.O_CREATE|os.O_TRUNC, PermFile)
	if err != nil {
		return err
	}
	defer dst.Close()

	// Copy the contents of the file.
	_, err = io.Copy(dst, src)
	if err != nil {
		return err
	}

	// Set the configured file permissions instead of copying from source
	err = root.Root.Chmod(relPath(dest), PermFile)
	if err != nil {
		return err
	}

	return nil
}

// copyDirectory handles copying directories recursively.
func copyDirectory(root *fsroot.FSRoot, source, dest string) error {
	// Create the destination directory.
	err := root.Root.MkdirAll(relPath(dest), PermDir)
	if err != nil {
		return err
	}

	// Read the contents of the source directory.
	entries, err := readDir(root, source)
	if err != nil {
		return err
	}

	// Iterate over each entry in the directory.
	for _, entry := range entries {
		srcPath := filepath.Join(source, entry.Name())
		destPath := filepath.Join(dest, entry.Name())

		if entry.IsDir() {
			// Recursively copy subdirectories.
			err = copyDirectory(root, srcPath, destPath)
			if err != nil {
				return err
			}
		} else {
			// Copy files.
			err = copySingleFile(root, srcPath, destPath)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// CopyFileWithCallbacks copies a file or directory with progress callbacks.
// It handles both files and directories, copying recursively as needed.
func CopyFileWithCallbacks(source, dest string, overwrite bool, opts *ipc.OperationCallbacks) error {
	source = cleanAbsPath(source)
	dest = cleanAbsPath(dest)

	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	return copyWithCallbacksAndRoot(root, source, dest, overwrite, opts)
}

func copyWithCallbacksAndRoot(root *fsroot.FSRoot, source, dest string, overwrite bool, opts *ipc.OperationCallbacks) error {
	// Validate the copy operation before executing
	if err := validateMoveDestination(root, source, dest); err != nil {
		return err
	}

	// Check if the source exists and whether it's a file or directory.
	info, err := root.Root.Stat(relPath(source))
	if err != nil {
		return err
	}

	if err := prepareDestination(root, info.IsDir(), dest, overwrite); err != nil {
		return err
	}

	if info.IsDir() {
		// If the source is a directory, copy it recursively.
		return copyDirectoryWithCallbacks(root, source, dest, opts)
	}

	// If the source is a file, copy the file.
	return copySingleFileWithCallbacks(root, source, dest, opts)
}

// copySingleFileWithCallbacks handles copying a single file with progress callbacks.
func copySingleFileWithCallbacks(root *fsroot.FSRoot, source, dest string, opts *ipc.OperationCallbacks) error {
	if isOperationCancelled(opts) {
		return ipc.ErrAborted
	}

	src, err := root.Root.Open(relPath(source))
	if err != nil {
		return err
	}
	defer src.Close()

	if mkdirErr := root.Root.MkdirAll(relPath(filepath.Dir(dest)), PermDir); mkdirErr != nil {
		return mkdirErr
	}

	dst, err := root.Root.OpenFile(relPath(dest), os.O_RDWR|os.O_CREATE|os.O_TRUNC, PermFile)
	if err != nil {
		return err
	}
	defer dst.Close()

	if err := copyFileDataWithCallbacks(src, dst, opts); err != nil {
		return err
	}

	if err := root.Root.Chmod(relPath(dest), PermFile); err != nil {
		return err
	}

	return nil
}

func copyFileDataWithCallbacks(src io.Reader, dst io.Writer, opts *ipc.OperationCallbacks) error {
	buf := make([]byte, 32*1024)
	for {
		if isOperationCancelled(opts) {
			return ipc.ErrAborted
		}

		n, readErr := src.Read(buf)
		if n > 0 {
			if _, writeErr := dst.Write(buf[:n]); writeErr != nil {
				return writeErr
			}
			reportOperationProgress(opts, int64(n))
		}

		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// copyDirectoryWithCallbacks handles copying directories recursively with progress callbacks.
func copyDirectoryWithCallbacks(root *fsroot.FSRoot, source, dest string, opts *ipc.OperationCallbacks) error {
	// Check for cancellation
	if opts != nil && opts.Cancel != nil && opts.Cancel() {
		return ipc.ErrAborted
	}

	// Create the destination directory.
	err := root.Root.MkdirAll(relPath(dest), PermDir)
	if err != nil {
		return err
	}

	// Read the contents of the source directory.
	entries, err := readDir(root, source)
	if err != nil {
		return err
	}

	// Iterate over each entry in the directory.
	for _, entry := range entries {
		srcPath := filepath.Join(source, entry.Name())
		destPath := filepath.Join(dest, entry.Name())

		if entry.IsDir() {
			// Recursively copy subdirectories.
			err = copyDirectoryWithCallbacks(root, srcPath, destPath, opts)
			if err != nil {
				return err
			}
		} else {
			// Copy files.
			err = copySingleFileWithCallbacks(root, srcPath, destPath, opts)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// ComputeCopySize calculates the total size of files to be copied.
// For directories, it recursively sums the sizes of all contained files.
func ComputeCopySize(path string) (int64, error) {
	path = cleanAbsPath(path)

	root, err := fsroot.Open()
	if err != nil {
		return 0, err
	}
	defer root.Close()

	info, err := root.Root.Stat(relPath(path))
	if err != nil {
		return 0, err
	}

	if !info.IsDir() {
		return info.Size(), nil
	}

	var totalSize int64
	err = root.WalkDir(path, func(_ string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		entryInfo, entryInfoErr := entry.Info()
		if entryInfoErr != nil {
			return entryInfoErr
		}
		totalSize += entryInfo.Size()
		return nil
	})

	return totalSize, err
}

// MoveFileWithCallbacks moves a file from src to dst with progress callbacks.
// By default, the rename system call is used. If src and dst point to different volumes,
// the file copy with callbacks is used as a fallback, followed by deletion of the source.
func MoveFileWithCallbacks(src, dst string, overwrite bool, opts *ipc.OperationCallbacks) error {
	src = cleanAbsPath(src)
	dst = cleanAbsPath(dst)

	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	// Validate the move operation before executing
	if validateErr := validateMoveDestination(root, src, dst); validateErr != nil {
		return validateErr
	}

	if isOperationCancelled(opts) {
		return ipc.ErrAborted
	}

	srcInfo, err := root.Root.Stat(relPath(src))
	if err != nil {
		return err
	}

	if destErr := prepareDestination(root, srcInfo.IsDir(), dst, overwrite); destErr != nil {
		return destErr
	}

	if moved, err := tryRenameMove(root, src, dst, opts); moved {
		return err
	} else if err != nil {
		return nil
	}

	if err := copyWithCallbacksAndRoot(root, src, dst, overwrite, opts); err != nil {
		slog.Error("copy with callbacks failed", "component", "filebrowser", "subsystem", "file_operations", "path", src, "destination", dst, "error", err)
		return err
	}

	if isOperationCancelled(opts) {
		return ipc.ErrAborted
	}

	// Delete source after successful copy
	if removeErr := root.Root.RemoveAll(relPath(src)); removeErr != nil {
		slog.Error("failed to remove source after copy", "component", "filebrowser", "subsystem", "file_operations", "path", src, "error", removeErr)
		return fmt.Errorf("failed to remove source after copy: %w", removeErr)
	}

	return nil
}

func tryRenameMove(root *fsroot.FSRoot, src, dst string, opts *ipc.OperationCallbacks) (bool, error) {
	if err := root.Root.Rename(relPath(src), relPath(dst)); err != nil {
		return false, nil
	}

	totalSize, sizeErr := ComputeCopySize(dst)
	if sizeErr != nil {
		slog.Debug("failed to compute move size after rename", "component", "filebrowser", "subsystem", "file_operations", "path", dst, "error", sizeErr)
		return true, nil
	}

	reportOperationProgress(opts, totalSize)
	return true, nil
}

func isOperationCancelled(opts *ipc.OperationCallbacks) bool {
	return opts != nil && opts.Cancel != nil && opts.Cancel()
}

func reportOperationProgress(opts *ipc.OperationCallbacks, bytes int64) {
	if opts != nil && opts.Progress != nil {
		opts.Progress(bytes)
	}
}

// DeleteFiles removes a file or directory
func DeleteFiles(absPath string) error {
	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	return root.Root.RemoveAll(relPath(absPath))
}

// CreateDirectory creates a directory with proper permissions
func CreateDirectory(opts iteminfo.FileOptions) error {
	realPath := cleanAbsPath(opts.Path)

	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	// Check if the destination exists and is a file
	if stat, err := root.Root.Stat(relPath(realPath)); err == nil && !stat.IsDir() {
		// If it's a file and we're trying to create a directory, remove the file first
		if err := root.Root.Remove(relPath(realPath)); err != nil {
			return fmt.Errorf("could not remove existing file to create directory: %v", err)
		}
	}

	// Ensure the parent directories exist
	if err := root.Root.MkdirAll(relPath(realPath), PermDir); err != nil {
		return err
	}

	// Explicitly set directory permissions to bypass umask
	if err := root.Root.Chmod(relPath(realPath), PermDir); err != nil {
		return err
	}

	return nil
}

// WriteContentInFile writes content to a file with proper permissions
func WriteContentInFile(opts iteminfo.FileOptions, in io.Reader) error {
	realPath := cleanAbsPath(opts.Path)
	// Strip trailing slash from realPath if it's meant to be a file
	realPath = strings.TrimRight(realPath, "/")

	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	// Ensure the parent directories exist
	parentDir := filepath.Dir(realPath)
	if mkdirErr := root.Root.MkdirAll(relPath(parentDir), PermDir); mkdirErr != nil {
		return mkdirErr
	}

	// Check if the destination exists and is a directory
	if stat, statErr := root.Root.Stat(relPath(realPath)); statErr == nil && stat.IsDir() {
		// If it's a directory and we're trying to create a file, remove the directory first
		if removeErr := root.Root.RemoveAll(relPath(realPath)); removeErr != nil {
			return fmt.Errorf("could not remove existing directory to create file: %v", removeErr)
		}
	}

	// Open the file for writing (create if it doesn't exist, truncate if it does)
	file, err := root.Root.OpenFile(relPath(realPath), os.O_RDWR|os.O_CREATE|os.O_TRUNC, PermFile)
	if err != nil {
		return err
	}
	defer file.Close()

	// Copy the contents from the reader to the file
	_, err = io.Copy(file, in)
	if err != nil {
		return err
	}

	// Explicitly set file permissions to bypass umask
	if err := root.Root.Chmod(relPath(realPath), PermFile); err != nil {
		return err
	}

	return nil
}

// GetContent reads and returns the file content if it's considered an editable text file.
func GetContent(realPath string) (string, error) {
	const headerSize = 4096

	cleanPath := cleanAbsPath(realPath)

	root, err := fsroot.Open()
	if err != nil {
		return "", err
	}
	defer root.Close()

	// Open file
	f, err := root.Root.Open(relPath(cleanPath))
	if err != nil {
		return "", err
	}
	defer f.Close()

	// Read header
	headerBytes := make([]byte, headerSize)
	n, err := f.Read(headerBytes)
	if err != nil && err != io.EOF {
		return "", err
	}
	if !isEditableTextHeader(headerBytes[:n]) {
		return "", nil
	}

	content, err := root.Root.ReadFile(relPath(cleanPath))
	if err != nil {
		return "", err
	}
	if len(content) == 0 {
		return "empty-file-x6OlSil", nil
	}

	if !isEditableTextContent(content) {
		return "", nil
	}

	return string(content), nil
}

func isEditableTextHeader(header []byte) bool {
	const maxNullBytesInHeaderAbs = 10
	const maxNullByteRatioInHeader = 0.1

	if len(header) == 0 {
		return true
	}
	if !utf8.Valid(header) {
		return false
	}

	nullCount := countNullBytes(header)
	if nullCount > maxNullBytesInHeaderAbs || float64(nullCount)/float64(len(header)) > maxNullByteRatioInHeader {
		return false
	}

	for _, b := range header {
		if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
			return false
		}
	}
	return true
}

func isEditableTextContent(content []byte) bool {
	const maxNullByteRatioInFile = 0.05
	const maxNonPrintableRuneRatio = 0.05

	stringContent := string(content)
	if !utf8.ValidString(stringContent) {
		return false
	}
	if float64(countNullBytes(content))/float64(len(content)) > maxNullByteRatioInFile {
		return false
	}
	return nonPrintableRuneRatio(stringContent) <= maxNonPrintableRuneRatio
}

func countNullBytes(content []byte) int {
	count := 0
	for _, b := range content {
		if b == 0x00 {
			count++
		}
	}
	return count
}

func nonPrintableRuneRatio(content string) float64 {
	nonPrintableRuneCount := 0
	totalRuneCount := 0

	for _, r := range content {
		totalRuneCount++
		if !unicode.IsPrint(r) && r != '\t' && r != '\n' && r != '\r' {
			nonPrintableRuneCount++
		}
	}
	if totalRuneCount == 0 {
		return 0
	}
	return float64(nonPrintableRuneCount) / float64(totalRuneCount)
}

// CommonPrefix returns the common directory path of provided files.
func CommonPrefix(sep byte, paths ...string) string {
	// Handle special cases.
	switch len(paths) {
	case 0:
		return ""
	case 1:
		return path.Clean(paths[0])
	}

	// Treat string as []byte, not []rune as is often done in Go.
	c := []byte(path.Clean(paths[0]))

	// Add a trailing sep to handle the case where the common prefix directory
	// is included in the path list.
	c = append(c, sep)

	// Ignore the first path since it's already in c.
	for _, v := range paths[1:] {
		// Clean up each path before testing it.
		v = path.Clean(v) + string(sep)

		// Find the first non-common byte and truncate c.
		if len(v) < len(c) {
			c = c[:len(v)]
		}
		for i := 0; i < len(c); i++ {
			if v[i] != c[i] {
				c = c[:i]
				break
			}
		}
	}

	// Remove trailing non-separator characters and the final separator.
	for i, v := range slices.Backward(c) {
		if v == sep {
			c = c[:i]
			break
		}
	}

	return string(c)
}

// ChangePermissions changes the permissions of a file or directory
// If recursive is true and the path is a directory, changes permissions recursively
func ChangePermissions(path string, mode os.FileMode, recursive bool) error {
	return ChangePermissionsCtx(context.Background(), path, mode, recursive, nil)
}

// ChangePermissionsCtx changes permissions and reports processed entries when requested.
func ChangePermissionsCtx(ctx context.Context, path string, mode os.FileMode, recursive bool, cb func(processed, total int64)) error {
	path = cleanAbsPath(path)

	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	// Check if path exists
	info, err := root.Root.Stat(relPath(path))
	if err != nil {
		return fmt.Errorf("failed to stat path: %w", err)
	}

	recursiveDir := info.IsDir() && recursive
	total, err := countRecursiveEntries(ctx, root, path, recursiveDir)
	if err != nil {
		return err
	}
	report := progressReporter(total, cb)

	if err := ctx.Err(); err != nil {
		return err
	}

	// Change permissions of the main path
	if err := root.Root.Chmod(relPath(path), mode); err != nil {
		return fmt.Errorf("failed to chmod %s: %w", path, err)
	}
	report()

	if recursiveDir {
		return changePermissionsRecursive(ctx, root, path, mode, report)
	}

	return nil
}

// ChangeOwnership updates the owner and/or group for a file or directory.
// If recursive is true and the path is a directory, changes ownership recursively.
// Passing uid or gid as -1 will leave that field unchanged (POSIX semantics).
func ChangeOwnership(path string, uid, gid int, recursive bool) error {
	return ChangeOwnershipCtx(context.Background(), path, uid, gid, recursive, nil)
}

// ChangeOwnershipCtx changes ownership and reports processed entries when requested.
func ChangeOwnershipCtx(ctx context.Context, path string, uid, gid int, recursive bool, cb func(processed, total int64)) error {
	path = cleanAbsPath(path)

	root, err := fsroot.Open()
	if err != nil {
		return err
	}
	defer root.Close()

	info, err := root.Root.Lstat(relPath(path))
	if err != nil {
		return fmt.Errorf("failed to lstat path: %w", err)
	}

	recursiveDir := info.IsDir() && recursive
	total, err := countRecursiveEntries(ctx, root, path, recursiveDir)
	if err != nil {
		return err
	}
	report := progressReporter(total, cb)

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := root.Root.Lchown(relPath(path), uid, gid); err != nil {
		return fmt.Errorf("failed to chown %s: %w", path, err)
	}
	report()

	if recursiveDir {
		return changeOwnershipRecursive(ctx, root, path, uid, gid, report)
	}

	return nil
}

func progressReporter(total int64, cb func(processed, total int64)) func() {
	var processed int64
	return func() {
		processed++
		if cb != nil {
			cb(processed, total)
		}
	}
}

func changePermissionsRecursive(ctx context.Context, root *fsroot.FSRoot, path string, mode os.FileMode, report func()) error {
	rootRel := relPath(path)
	return root.WalkDir(path, func(walkRel string, _ fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			slog.Error("error walking path during chmod", "component", "filebrowser", "subsystem", "file_operations", "path", walkRel, "error", walkErr)
			return nil
		}
		if isRecursiveRoot(walkRel, rootRel) {
			return nil
		}
		if err := root.Root.Chmod(walkRel, mode); err != nil {
			slog.Error("failed to chmod path", "component", "filebrowser", "subsystem", "file_operations", "path", walkRel, "error", err)
			return nil
		}
		report()
		return nil
	})
}

func changeOwnershipRecursive(ctx context.Context, root *fsroot.FSRoot, path string, uid, gid int, report func()) error {
	rootRel := relPath(path)
	return root.WalkDir(path, func(walkRel string, _ fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			slog.Error("error walking path during chown", "component", "filebrowser", "subsystem", "file_operations", "path", walkRel, "error", walkErr)
			return nil
		}
		if isRecursiveRoot(walkRel, rootRel) {
			return nil
		}
		if err := root.Root.Lchown(walkRel, uid, gid); err != nil {
			slog.Error("failed to chown path", "component", "filebrowser", "subsystem", "file_operations", "path", walkRel, "error", err)
		}
		report()
		return nil
	})
}

func isRecursiveRoot(walkRel, rootRel string) bool {
	return walkRel == "." || walkRel == rootRel
}

func countRecursiveEntries(ctx context.Context, root *fsroot.FSRoot, path string, recursive bool) (int64, error) {
	total := int64(1)
	if !recursive {
		return total, nil
	}
	err := root.WalkDir(path, func(walkRel string, _ fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			slog.Error("error walking path during permission count", "component", "filebrowser", "subsystem", "file_operations", "path", walkRel, "error", walkErr)
			return nil
		}
		if walkRel == "." || walkRel == relPath(path) {
			return nil
		}
		total++
		return nil
	})
	return total, err
}

// validateMoveDestination validates that a move operation is safe
func validateMoveDestination(root *fsroot.FSRoot, src, dst string) error {
	// Clean and normalize paths
	src = cleanAbsPath(src)
	dst = cleanAbsPath(dst)

	if src == dst {
		return fmt.Errorf("source and destination are the same")
	}

	// Check if source is a directory
	srcInfo, err := root.Root.Stat(relPath(src))
	if err != nil {
		return err
	}

	isSrcDir := srcInfo.IsDir()

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
		if _, err := root.Root.Stat(relPath(dstParent)); os.IsNotExist(err) {
			return fmt.Errorf("destination directory does not exist: '%s'", dstParent)
		}
	}

	return nil
}

func prepareDestination(root *fsroot.FSRoot, srcIsDir bool, dst string, overwrite bool) error {
	dstInfo, err := root.Root.Stat(relPath(dst))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if !overwrite {
		return fmt.Errorf("destination already exists")
	}

	if dstInfo.IsDir() != srcIsDir {
		return fmt.Errorf("destination exists with different type")
	}

	return root.Root.RemoveAll(relPath(dst))
}
