package files

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gtsteffaniak/go-logger/logger"

	"github.com/mordilloSan/filebrowser/backend/adapters/fs/fileutils"
	"github.com/mordilloSan/filebrowser/backend/common/settings"
	"github.com/mordilloSan/filebrowser/backend/common/utils"
	"github.com/mordilloSan/filebrowser/backend/indexing/iteminfo"
)

func FileInfoFaster(opts utils.FileOptions) (*iteminfo.ExtendedFileInfo, error) {
	response := &iteminfo.ExtendedFileInfo{}
	sourceName := opts.Source
	if sourceName == "" {
		sourceName = settings.RootPath
	}

	if !strings.HasPrefix(opts.Path, "/") {
		opts.Path = "/" + opts.Path
	}

	// Build real path directly
	realPath := filepath.Join(sourceName, opts.Path)

	// Resolve symlinks
	resolvedPath, isDir, err := iteminfo.ResolveSymlinks(realPath)
	if err != nil {
		return response, fmt.Errorf("could not resolve path: %v, error: %v", opts.Path, err)
	}

	if !strings.HasSuffix(opts.Path, "/") && isDir {
		opts.Path = opts.Path + "/"
	}
	opts.IsDir = isDir

	var info *iteminfo.FileInfo
	if isDir {
		info, err = getDirInfo(sourceName, opts.Path, resolvedPath)
		if err != nil {
			return response, err
		}
	} else {
		// For files, get info from parent directory
		parentPath := filepath.Dir(opts.Path)
		if parentPath == "." {
			parentPath = "/"
		}
		parentRealPath := filepath.Dir(resolvedPath)

		dirInfo, err := getDirInfo(sourceName, parentPath, parentRealPath)
		if err != nil {
			return response, err
		}

		// Find the file in the parent directory
		baseName := filepath.Base(resolvedPath)
		for _, file := range dirInfo.Files {
			if file.Name == baseName {
				info = &iteminfo.FileInfo{
					Path:     opts.Path,
					ItemInfo: file,
				}
				break
			}
		}
		if info == nil {
			return response, fmt.Errorf("file not found: %s", opts.Path)
		}
	}

	response.FileInfo = *info
	response.RealPath = resolvedPath
	response.Source = sourceName
	opts.Source = sourceName

	if opts.Content || opts.Metadata {
		processContent(response, opts)
	}
	return response, nil
}

func getDirInfo(sourceName, adjustedPath, realPath string) (*iteminfo.FileInfo, error) {
	dir, err := os.Open(realPath)
	if err != nil {
		return nil, err
	}
	defer dir.Close()

	dirStat, err := dir.Stat()
	if err != nil {
		return nil, err
	}

	if !dirStat.IsDir() {
		// It's a file
		fileInfo := &iteminfo.FileInfo{
			Path: adjustedPath,
			ItemInfo: iteminfo.ItemInfo{
				Name:    filepath.Base(realPath),
				Size:    dirStat.Size(),
				ModTime: dirStat.ModTime(),
			},
		}
		fileInfo.DetectType(realPath, false)
		setFilePreviewFlags(&fileInfo.ItemInfo, realPath)
		return fileInfo, nil
	}

	// Read directory contents
	entries, err := dir.Readdir(-1)
	if err != nil {
		return nil, err
	}

	var totalSize int64
	fileInfos := []iteminfo.ItemInfo{}
	dirInfos := []iteminfo.ItemInfo{}

	for _, entry := range entries {
		entryName := entry.Name()
		hidden := entryName[0] == '.'
		isDir := entry.IsDir()
		fileRealPath := filepath.Join(realPath, entryName)
		isSymlink := entry.Mode()&os.ModeSymlink != 0

		// Handle symlinks
		if !isDir && isSymlink {
			if resolvedPath, resolvedIsDir, simErr := iteminfo.ResolveSymlinks(fileRealPath); simErr == nil {
				isDir = resolvedIsDir
				if resolvedIsDir {
					fileRealPath = resolvedPath
				}
			}
		}

		itemInfo := &iteminfo.ItemInfo{
			Name:    entryName,
			ModTime: entry.ModTime(),
			Hidden:  hidden,
			Symlink: isSymlink,
		}

		if isDir {
			// Skip recursive size calculation for directories to keep listings fast
			itemInfo.Size = 0
			itemInfo.HasPreview = false
			itemInfo.Type = "directory"
			dirInfos = append(dirInfos, *itemInfo)
		} else {
			itemInfo.DetectType(fileRealPath, false)
			setFilePreviewFlags(itemInfo, fileRealPath)
			itemInfo.Size = entry.Size()
			fileInfos = append(fileInfos, *itemInfo)
			totalSize += itemInfo.Size
		}
	}

	dirFileInfo := &iteminfo.FileInfo{
		Path:    adjustedPath,
		Files:   fileInfos,
		Folders: dirInfos,
	}
	dirFileInfo.ItemInfo = iteminfo.ItemInfo{
		Name:       filepath.Base(realPath),
		Type:       "directory",
		Size:       totalSize,
		ModTime:    dirStat.ModTime(),
		HasPreview: false,
	}
	dirFileInfo.SortItems()

	return dirFileInfo, nil
}

func setFilePreviewFlags(fileInfo *iteminfo.ItemInfo, realPath string) {
	simpleType := strings.Split(fileInfo.Type, "/")[0]

	// Check if it's an image
	if simpleType == "image" {
		fileInfo.HasPreview = true
	}

	if iteminfo.HasDocConvertableExtension(fileInfo.Name, fileInfo.Type) {
		fileInfo.HasPreview = true
	}
}

func processContent(info *iteminfo.ExtendedFileInfo, opts utils.FileOptions) {
	isVideo := strings.HasPrefix(info.Type, "video")
	isAudio := strings.HasPrefix(info.Type, "audio")
	isFolder := info.Type == "directory"
	if isFolder {
		return
	}

	if isVideo {
		return
	}

	// Audio files no longer have preview capability (playback removed)
	if isAudio {
		return
	}

	// Process text content for non-video, non-audio files
	if info.Size < 20*1024*1024 { // 20 megabytes in bytes
		content, err := getContent(info.RealPath)
		if err != nil {
			logger.Debugf("could not get content for file: "+info.RealPath, info.Name, err)
			return
		}
		info.Content = content
	} else {
		logger.Debug("skipping large text file contents (20MB limit): "+info.Path, info.Name, info.Type)
	}
}

func DeleteFiles(source, absPath string, absDirPath string) error {
	if source == "" {
		source = settings.RootPath
	}
	err := os.RemoveAll(absPath)
	if err != nil {
		return err
	}
	return nil
}

func RefreshIndex(source string, path string, isDir bool, recursive bool) error {
	// No indexing - this is a no-op
	return nil
}

func validateMoveDestination(src, dst string, isSrcDir bool) error {
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

func MoveResource(isSrcDir bool, sourceIndex, destIndex, realsrc, realdst string) error {
	if sourceIndex == "" {
		sourceIndex = settings.RootPath
	}
	if destIndex == "" {
		destIndex = settings.RootPath
	}

	// Validate the move operation before executing
	if err := validateMoveDestination(realsrc, realdst, isSrcDir); err != nil {
		return err
	}

	err := fileutils.MoveFile(realsrc, realdst)
	if err != nil {
		return err
	}

	return nil
}

func CopyResource(isSrcDir bool, sourceIndex, destIndex, realsrc, realdst string) error {
	// Validate the copy operation before executing
	if err := validateMoveDestination(realsrc, realdst, isSrcDir); err != nil {
		return err
	}

	err := fileutils.CopyFile(realsrc, realdst)
	if err != nil {
		return err
	}

	return nil
}

func WriteDirectory(opts utils.FileOptions) error {
	if opts.Source == "" {
		opts.Source = settings.RootPath
	}

	realPath := filepath.Join(opts.Source, opts.Path)

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
	err = os.MkdirAll(realPath, fileutils.PermDir)
	if err != nil {
		return err
	}

	// Explicitly set directory permissions to bypass umask
	err = os.Chmod(realPath, fileutils.PermDir)
	if err != nil {
		return err
	}

	return nil
}

func WriteFile(opts utils.FileOptions, in io.Reader) error {
	if opts.Source == "" {
		opts.Source = settings.RootPath
	}

	realPath := filepath.Join(opts.Source, opts.Path)
	// Strip trailing slash from realPath if it's meant to be a file
	realPath = strings.TrimRight(realPath, "/")

	// Ensure the parent directories exist
	parentDir := filepath.Dir(realPath)
	err := os.MkdirAll(parentDir, fileutils.PermDir)
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
	file, err := os.OpenFile(realPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, fileutils.PermFile)
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
	err = os.Chmod(realPath, fileutils.PermFile)
	if err != nil {
		return err
	}

	return nil
}

// getContent reads and returns the file content if it's considered an editable text file.
func getContent(realPath string) (string, error) {
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

func IsNamedPipe(mode os.FileMode) bool {
	return mode&os.ModeNamedPipe != 0
}

func IsSymlink(mode os.FileMode) bool {
	return mode&os.ModeSymlink != 0
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}
