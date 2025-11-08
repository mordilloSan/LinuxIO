package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MoveCopyService handles move, copy, and delete operations with validation
type MoveCopyService struct{}

// NewMoveCopyService creates a new move/copy service
func NewMoveCopyService() *MoveCopyService {
	return &MoveCopyService{}
}

// ValidateMoveDestination validates that a move operation is safe
func (s *MoveCopyService) ValidateMoveDestination(src, dst string, isSrcDir bool) error {
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

// MoveResource moves a file or directory from src to dst
func (s *MoveCopyService) MoveResource(isSrcDir bool, realsrc, realdst string) error {
	// Validate the move operation before executing
	if err := s.ValidateMoveDestination(realsrc, realdst, isSrcDir); err != nil {
		return err
	}

	err := MoveFile(realsrc, realdst)
	if err != nil {
		return err
	}

	return nil
}

// CopyResource copies a file or directory from src to dst
func (s *MoveCopyService) CopyResource(isSrcDir bool, realsrc, realdst string) error {
	// Validate the copy operation before executing
	if err := s.ValidateMoveDestination(realsrc, realdst, isSrcDir); err != nil {
		return err
	}

	err := CopyFile(realsrc, realdst)
	if err != nil {
		return err
	}

	return nil
}
