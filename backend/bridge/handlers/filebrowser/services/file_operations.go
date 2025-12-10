package services

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mordilloSan/go_logger/logger"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/iteminfo"
)

var (
	PermFile os.FileMode = 0o664 // rw-rw-r-- (owner read+write, group read+write, rest read)
	PermDir  os.FileMode = 0o775 // rwxrwxr-x (owner read+write+execute, group read+write+execute, rest read+execute)
)

// MoveFile moves a file from src to dst.
// By default, the rename system call is used. If src and dst point to different volumes,
// the file copy is used as a fallback.
func MoveFile(src, dst string, overwrite bool) error {
	// Validate the move operation before executing
	if err := validateMoveDestination(src, dst); err != nil {
		return err
	}

	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}

	if destErr := prepareDestination(srcInfo.IsDir(), dst, overwrite); destErr != nil {
		return destErr
	}

	err = os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// fallback
	err = CopyFile(src, dst, overwrite)
	if err != nil {
		logger.Errorf("CopyFile failed %v %v %v ", src, dst, err)
		return err
	}

	go func() {
		if removeErr := os.RemoveAll(src); removeErr != nil {
			logger.Errorf("os.Remove failed %v %v ", src, removeErr)
		}
	}()

	return nil
}

// CopyFile copies a file or directory from source to dest and returns an error if any.
// It handles both files and directories, copying recursively as needed.
func CopyFile(source, dest string, overwrite bool) error {
	// Validate the copy operation before executing
	if err := validateMoveDestination(source, dest); err != nil {
		return err
	}

	// Check if the source exists and whether it's a file or directory.
	info, err := os.Stat(source)
	if err != nil {
		return err
	}

	if err := prepareDestination(info.IsDir(), dest, overwrite); err != nil {
		return err
	}

	if info.IsDir() {
		// If the source is a directory, copy it recursively.
		return copyDirectory(source, dest)
	}

	// If the source is a file, copy the file.
	return copySingleFile(source, dest)
}

// copySingleFile handles copying a single file.
func copySingleFile(source, dest string) error {
	// Open the source file.
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()

	// Create the destination directory if needed.
	err = os.MkdirAll(filepath.Dir(dest), PermDir)
	if err != nil {
		return err
	}

	// Create the destination file.
	dst, err := os.OpenFile(dest, os.O_RDWR|os.O_CREATE|os.O_TRUNC, PermFile)
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
	err = os.Chmod(dest, PermFile)
	if err != nil {
		return err
	}

	return nil
}

// copyDirectory handles copying directories recursively.
func copyDirectory(source, dest string) error {
	// Create the destination directory.
	err := os.MkdirAll(dest, PermDir)
	if err != nil {
		return err
	}

	// Read the contents of the source directory.
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}

	// Iterate over each entry in the directory.
	for _, entry := range entries {
		srcPath := filepath.Join(source, entry.Name())
		destPath := filepath.Join(dest, entry.Name())

		if entry.IsDir() {
			// Recursively copy subdirectories.
			err = copyDirectory(srcPath, destPath)
			if err != nil {
				return err
			}
		} else {
			// Copy files.
			err = copySingleFile(srcPath, destPath)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// DeleteFiles removes a file or directory
func DeleteFiles(absPath string) error {
	err := os.RemoveAll(absPath)
	if err != nil {
		return err
	}
	return nil
}

// CreateDirectory creates a directory with proper permissions
func CreateDirectory(opts iteminfo.FileOptions) error {
	realPath := filepath.Join(opts.Path)

	var stat os.FileInfo
	var err error
	// Check if the destination exists and is a file
	if stat, err = os.Stat(realPath); err == nil && !stat.IsDir() {
		// If it's a file and we're trying to create a directory, remove the file first
		err = os.Remove(realPath)
		if err != nil {
			return fmt.Errorf("could not remove existing file to create directory: %v", err)
		}
	}

	// Ensure the parent directories exist
	err = os.MkdirAll(realPath, PermDir)
	if err != nil {
		return err
	}

	// Explicitly set directory permissions to bypass umask
	err = os.Chmod(realPath, PermDir)
	if err != nil {
		return err
	}

	return nil
}

// WriteContentInFile writes content to a file with proper permissions
func WriteContentInFile(opts iteminfo.FileOptions, in io.Reader) error {
	realPath := filepath.Join(opts.Path)
	// Strip trailing slash from realPath if it's meant to be a file
	realPath = strings.TrimRight(realPath, "/")

	// Ensure the parent directories exist
	parentDir := filepath.Dir(realPath)
	err := os.MkdirAll(parentDir, PermDir)
	if err != nil {
		return err
	}

	var stat os.FileInfo
	// Check if the destination exists and is a directory
	if stat, err = os.Stat(realPath); err == nil && stat.IsDir() {
		// If it's a directory and we're trying to create a file, remove the directory first
		err = os.RemoveAll(realPath)
		if err != nil {
			return fmt.Errorf("could not remove existing directory to create file: %v", err)
		}
	}

	// Open the file for writing (create if it doesn't exist, truncate if it does)
	file, err := os.OpenFile(realPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, PermFile)
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
	err = os.Chmod(realPath, PermFile)
	if err != nil {
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

	// Open file
	f, err := os.Open(realPath)
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
	content, err := os.ReadFile(realPath)
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
	path = filepath.Clean(path)

	// Check if path exists
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat path: %w", err)
	}

	// Change permissions of the main path
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("failed to chmod %s: %w", path, err)
	}

	// If it's a directory and recursive is true, walk through and change all nested items
	if info.IsDir() && recursive {
		return filepath.Walk(path, func(walkPath string, walkInfo os.FileInfo, walkErr error) error {
			if walkErr != nil {
				logger.Errorf("error walking path %s: %v", walkPath, walkErr)
				return nil // Continue walking even if one item fails
			}

			// Skip the root path as we already changed it
			if walkPath == path {
				return nil
			}

			if err := os.Chmod(walkPath, mode); err != nil {
				logger.Errorf("failed to chmod %s: %v", walkPath, err)
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
	path = filepath.Clean(path)

	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("failed to lstat path: %w", err)
	}

	if err := os.Lchown(path, uid, gid); err != nil {
		return fmt.Errorf("failed to chown %s: %w", path, err)
	}

	if info.IsDir() && recursive {
		return filepath.Walk(path, func(walkPath string, walkInfo os.FileInfo, walkErr error) error {
			if walkErr != nil {
				logger.Errorf("error walking path %s: %v", walkPath, walkErr)
				return nil
			}

			// Skip root (already changed)
			if walkPath == path {
				return nil
			}

			if err := os.Lchown(walkPath, uid, gid); err != nil {
				logger.Errorf("failed to chown %s: %v", walkPath, err)
			}

			return nil
		})
	}

	return nil
}

// validateMoveDestination validates that a move operation is safe
func validateMoveDestination(src, dst string) error {
	// Clean and normalize paths
	src = filepath.Clean(src)
	dst = filepath.Clean(dst)

	// Check if source is a directory
	srcInfo, err := os.Stat(src)
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
		if _, err := os.Stat(dstParent); os.IsNotExist(err) {
			return fmt.Errorf("destination directory does not exist: '%s'", dstParent)
		}
	}

	return nil
}

func prepareDestination(srcIsDir bool, dst string, overwrite bool) error {
	dstInfo, err := os.Stat(dst)
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

	return os.RemoveAll(dst)
}
