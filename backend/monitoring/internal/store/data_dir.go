package store

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/utils"
)

// errDataDirMissing marks a candidate directory that does not exist yet, so
// callers that may create it can tell it apart from a permission problem.
var errDataDirMissing = errors.New("does not exist")

// GetDataDir returns a writable data directory for the agent, creating it when
// none of the candidates exist yet.
//
// Explicit paths are honored exactly: resolution never falls back to the
// default candidates, and the returned error names every rejected path and the
// reason it was rejected.
func GetDataDir(dataDirs ...string) (string, error) {
	if len(dataDirs) > 0 {
		return explicitDataDir(dataDirs, true)
	}
	return testDataDirs(defaultDataDirs())
}

// GetReadOnlyDataDir returns the data directory to inspect without requiring
// write access, so an unprivileged user can look at a root-owned store. It
// never creates directories.
//
// With no explicit path it prefers the first default candidate that already
// holds a metrics.db, because an inspection targets the database that exists
// rather than the one this user could write to.
func GetReadOnlyDataDir(dataDirs ...string) (string, error) {
	if len(dataDirs) > 0 {
		return explicitDataDir(dataDirs, false)
	}

	return readOnlyDataDir(defaultDataDirs()), nil
}

// readOnlyDataDir picks the candidate to inspect: an existing database first, a
// readable directory next, and otherwise the directory the agent would use, so
// callers can report that path as not created yet.
func readOnlyDataDir(candidates []string) string {
	for _, path := range candidates {
		if _, err := os.Stat(DatabasePath(path)); err == nil {
			return path
		}
	}
	for _, path := range candidates {
		if dataDirIssue(path, false) == nil {
			return path
		}
	}
	return candidates[0]
}

// defaultDataDirs lists the data directories to probe, most preferred first.
func defaultDataDirs() []string {
	dirs := make([]string, 0, 3)
	if dataDir, _ := utils.GetEnv("DATA_DIR"); dataDir != "" {
		dirs = append(dirs, dataDir)
	}
	dirs = append(dirs, "/var/lib/go-monitoring")
	if homeDir, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(homeDir, ".config", "go-monitoring"))
	}
	return dirs
}

// explicitDataDir resolves directories the caller named. It never falls back to
// the default candidates and reports why each path was rejected.
func explicitDataDir(paths []string, requireWritable bool) (string, error) {
	reasons := make([]string, 0, len(paths))
	for _, path := range paths {
		err := dataDirIssue(path, requireWritable)
		if err == nil {
			return path, nil
		}
		if requireWritable && errors.Is(err, errDataDirMissing) {
			createErr := createDataDir(path)
			if createErr == nil {
				return path, nil
			}
			err = createErr
		}
		reasons = append(reasons, fmt.Sprintf("%s: %v", path, err))
	}
	return "", fmt.Errorf("data directory not usable: %s", strings.Join(reasons, "; "))
}

func testDataDirs(paths []string) (string, error) {
	reasons := make([]string, 0, len(paths))
	// first check if the directory exists and is writable
	for _, path := range paths {
		err := dataDirIssue(path, true)
		if err == nil {
			return path, nil
		}
		if !errors.Is(err, errDataDirMissing) {
			reasons = append(reasons, fmt.Sprintf("%s: %v", path, err))
		}
	}
	// if the directory doesn't exist, try to create it
	for _, path := range paths {
		if exists, _ := directoryExists(path); exists {
			continue
		}
		if err := createDataDir(path); err != nil {
			reasons = append(reasons, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		return path, nil
	}

	if len(reasons) == 0 {
		return "", errors.New("data directory not found")
	}
	return "", fmt.Errorf("data directory not found (tried %s)", strings.Join(reasons, "; "))
}

// dataDirIssue reports why path cannot serve as a data directory, or nil when
// it can. Read-only callers only need to list the directory; writable callers
// must be able to create files in it.
func dataDirIssue(path string, requireWritable bool) error {
	exists, err := directoryExists(path)
	if err != nil {
		return err
	}
	if !exists {
		return errDataDirMissing
	}
	if requireWritable {
		if _, err := directoryIsWritable(path); err != nil {
			return fmt.Errorf("not writable by uid %d: %w", os.Getuid(), err)
		}
		return nil
	}
	if err := directoryIsReadable(path); err != nil {
		return fmt.Errorf("not readable by uid %d: %w", os.Getuid(), err)
	}
	return nil
}

// createDataDir creates path and verifies the result is actually writable.
func createDataDir(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return err
	}
	if _, err := directoryIsWritable(path); err != nil {
		return fmt.Errorf("not writable by uid %d: %w", os.Getuid(), err)
	}
	return nil
}

// directoryExists checks if a directory exists
func directoryExists(path string) (bool, error) {
	// Check if directory exists
	stat, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if !stat.IsDir() {
		return false, fmt.Errorf("%s is not a directory", path)
	}
	return true, nil
}

// directoryIsWritable tests if a directory is writable by creating and removing a temporary file
func directoryIsWritable(path string) (bool, error) {
	testFile := filepath.Join(path, ".write-test")
	file, err := os.Create(testFile)
	if err != nil {
		return false, err
	}
	defer file.Close()
	defer os.Remove(testFile)
	return true, nil
}

// directoryIsReadable tests if a directory can be listed by the current user.
func directoryIsReadable(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	if _, err := dir.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}
