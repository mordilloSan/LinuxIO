package services

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mordilloSan/LinuxIO/backend/server/filebrowser/common/utils"
)

// FileService handles file I/O operations
type FileService struct{}

// NewFileService creates a new file service
func NewFileService() *FileService {
	return &FileService{}
}

// DeleteFiles removes a file or directory
func (s *FileService) DeleteFiles(absPath string) error {
	err := os.RemoveAll(absPath)
	if err != nil {
		return err
	}
	return nil
}

// WriteDirectory creates a directory with proper permissions
func (s *FileService) WriteDirectory(opts utils.FileOptions) error {
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

// WriteFile writes content to a file with proper permissions
func (s *FileService) WriteFile(opts utils.FileOptions, in io.Reader) error {
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
func (s *FileService) GetContent(realPath string) (string, error) {
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
