package services

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mordilloSan/go-logger/logger"

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
		logger.Errorf("CopyFile failed %v %v %v ", src, dst, err)
		return err
	}

	go func(removePath string) {
		asyncRoot, openErr := fsroot.Open()
		if openErr != nil {
			logger.Errorf("open root failed during async remove %v", openErr)
			return
		}
		defer asyncRoot.Close()

		if removeErr := asyncRoot.Root.RemoveAll(relPath(removePath)); removeErr != nil {
			logger.Errorf("Root.RemoveAll failed %v %v ", removePath, removeErr)
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
	// Check for cancellation
	if opts != nil && opts.Cancel != nil && opts.Cancel() {
		return ipc.ErrAborted
	}

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

	// Copy the contents of the file with progress tracking.
	buf := make([]byte, 32*1024) // 32KB buffer
	for {
		// Check for cancellation
		if opts != nil && opts.Cancel != nil && opts.Cancel() {
			return ipc.ErrAborted
		}

		n, readErr := src.Read(buf)
		if n > 0 {
			_, writeErr := dst.Write(buf[:n])
			if writeErr != nil {
				return writeErr
			}

			// Report progress
			if opts != nil && opts.Progress != nil {
				opts.Progress(int64(n))
			}
		}

		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	// Set the configured file permissions instead of copying from source
	err = root.Root.Chmod(relPath(dest), PermFile)
	if err != nil {
		return err
	}

	return nil
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

	// Check for cancellation
	if opts != nil && opts.Cancel != nil && opts.Cancel() {
		return ipc.ErrAborted
	}

	srcInfo, err := root.Root.Stat(relPath(src))
	if err != nil {
		return err
	}

	if destErr := prepareDestination(root, srcInfo.IsDir(), dst, overwrite); destErr != nil {
		return destErr
	}

	// Try rename first (instant, no progress needed)
	err = root.Root.Rename(relPath(src), relPath(dst))
	if err == nil {
		// Rename succeeded - update progress to 100%
		if opts != nil && opts.Progress != nil {
			totalSize, sizeErr := ComputeCopySize(dst)
			if sizeErr != nil {
				logger.Debugf("failed to compute move size after rename for %s: %v", dst, sizeErr)
			} else {
				opts.Progress(totalSize)
			}
		}
		return nil
	}

	// Rename failed (likely different volumes) - fallback to copy with callbacks
	err = copyWithCallbacksAndRoot(root, src, dst, overwrite, opts)
	if err != nil {
		logger.Errorf("CopyFileWithCallbacks failed %v %v %v ", src, dst, err)
		return err
	}

	// Check for cancellation before deleting source
	if opts != nil && opts.Cancel != nil && opts.Cancel() {
		return ipc.ErrAborted
	}

	// Delete source after successful copy
	if removeErr := root.Root.RemoveAll(relPath(src)); removeErr != nil {
		logger.Errorf("Root.RemoveAll failed %v %v ", src, removeErr)
		return fmt.Errorf("failed to remove source after copy: %w", removeErr)
	}

	return nil
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
	// Thresholds for detecting binary-like content (these can be tuned)
	const maxNullBytesInHeaderAbs = 10    // Max absolute null bytes in header
	const maxNullByteRatioInHeader = 0.1  // Max 10% null bytes in header
	const maxNullByteRatioInFile = 0.05   // Max 5% null bytes in the entire file
	const maxNonPrintableRuneRatio = 0.05 // Max 5% non-printable runes in the entire file

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
	actualHeader := headerBytes[:n]

	// --- Start of new heuristic checks ---

	if n > 0 {
		// 1. Basic Check: Is the header valid UTF-8?
		// If not, it's unlikely an editable UTF-8 text file.
		if !utf8.Valid(actualHeader) {
			return "", nil // Not an error, just not the text file we want
		}

		// 2. Check for excessive null bytes in the header
		nullCountInHeader := 0
		for _, b := range actualHeader {
			if b == 0x00 {
				nullCountInHeader++
			}
		}
		// Reject if too many nulls absolutely or relatively in the header
		if nullCountInHeader > 0 { // Only perform check if there are any nulls
			if nullCountInHeader > maxNullBytesInHeaderAbs ||
				(float64(nullCountInHeader)/float64(n) > maxNullByteRatioInHeader) {
				return "", nil // Too many nulls in header
			}
		}

		// 3. Check for other non-text ASCII control characters in the header
		// (C0 controls excluding \t, \n, \r)
		for _, b := range actualHeader {
			if b < 0x20 && b != '\t' && b != '\n' && b != '\r' {
				return "", nil // Found problematic control character
			}
			// C1 control characters (0x80-0x9F) would be caught by utf8.Valid if part of invalid sequences,
			// or by the non-printable rune check later if they form valid (but undesirable) codepoints.
		}
	}
	// --- End of new heuristic checks for header ---

	// Now read the full file (original logic)
	content, err := root.Root.ReadFile(relPath(cleanPath))
	if err != nil {
		return "", err
	}
	// Handle empty file (original logic - returns specific string)
	if len(content) == 0 {
		return "empty-file-x6OlSil", nil
	}

	stringContent := string(content)

	// 4. Final UTF-8 validation for the entire file
	// (This is crucial as the header might be fine, but the rest of the file isn't)
	if !utf8.ValidString(stringContent) {
		return "", nil
	}

	// 5. Check for excessive null bytes in the entire file content
	if len(content) > 0 { // Check only for non-empty files
		totalNullCount := 0
		for _, b := range content {
			if b == 0x00 {
				totalNullCount++
			}
		}
		if float64(totalNullCount)/float64(len(content)) > maxNullByteRatioInFile {
			return "", nil // Too many nulls in the entire file
		}
	}

	// 6. Check for excessive non-printable runes in the entire file content
	// (Excluding tab, newline, carriage return, which are common in text files)
	if len(stringContent) > 0 { // Check only for non-empty strings
		nonPrintableRuneCount := 0
		totalRuneCount := 0
		for _, r := range stringContent {
			totalRuneCount++
			// unicode.IsPrint includes letters, numbers, punctuation, symbols, and spaces.
			// It excludes control characters. We explicitly allow \t, \n, \r.
			if !unicode.IsPrint(r) && r != '\t' && r != '\n' && r != '\r' {
				nonPrintableRuneCount++
			}
		}

		if totalRuneCount > 0 { // Avoid division by zero
			if float64(nonPrintableRuneCount)/float64(totalRuneCount) > maxNonPrintableRuneRatio {
				return "", nil // Too many non-printable runes
			}
		}
	}

	// The file has passed all checks and is considered editable text.
	return stringContent, nil
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
	for i := len(c) - 1; i >= 0; i-- {
		if c[i] == sep {
			c = c[:i]
			break
		}
	}

	return string(c)
}

// ChangePermissions changes the permissions of a file or directory
// If recursive is true and the path is a directory, changes permissions recursively
func ChangePermissions(path string, mode os.FileMode, recursive bool) error {
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

	// Change permissions of the main path
	if err := root.Root.Chmod(relPath(path), mode); err != nil {
		return fmt.Errorf("failed to chmod %s: %w", path, err)
	}

	// If it's a directory and recursive is true, walk through and change all nested items
	if info.IsDir() && recursive {
		return root.WalkDir(path, func(walkRel string, _ fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				logger.Errorf("error walking path %s: %v", walkRel, walkErr)
				return nil // Continue walking even if one item fails
			}

			// Skip the root path as we already changed it
			if walkRel == "." || walkRel == relPath(path) {
				return nil
			}

			if err := root.Root.Chmod(walkRel, mode); err != nil {
				logger.Errorf("failed to chmod %s: %v", walkRel, err)
				// Continue even if one item fails
				return nil
			}

			return nil
		})
	}

	return nil
}

// ChangeOwnership updates the owner and/or group for a file or directory.
// If recursive is true and the path is a directory, changes ownership recursively.
// Passing uid or gid as -1 will leave that field unchanged (POSIX semantics).
func ChangeOwnership(path string, uid, gid int, recursive bool) error {
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

	if err := root.Root.Lchown(relPath(path), uid, gid); err != nil {
		return fmt.Errorf("failed to chown %s: %w", path, err)
	}

	if info.IsDir() && recursive {
		return root.WalkDir(path, func(walkRel string, _ fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				logger.Errorf("error walking path %s: %v", walkRel, walkErr)
				return nil
			}

			// Skip root (already changed)
			if walkRel == "." || walkRel == relPath(path) {
				return nil
			}

			if err := root.Root.Lchown(walkRel, uid, gid); err != nil {
				logger.Errorf("failed to chown %s: %v", walkRel, err)
			}

			return nil
		})
	}

	return nil
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
