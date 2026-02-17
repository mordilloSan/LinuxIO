package fsroot

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// FSRoot provides helpers around os.Root for path-contained filesystem access.
type FSRoot struct {
	Root *os.Root
}

// Open opens the filesystem root (/).
func Open() (*FSRoot, error) {
	return OpenAt("/")
}

// OpenAt opens a specific root path. Primarily used by tests.
func OpenAt(path string) (*FSRoot, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, err
	}
	return &FSRoot{Root: root}, nil
}

// Close closes the underlying os.Root.
func (r *FSRoot) Close() error {
	if r == nil || r.Root == nil {
		return nil
	}
	return r.Root.Close()
}

// ToRel converts a path to a root-relative path safe for os.Root methods.
func ToRel(path string) string {
	cleanPath := filepath.Clean("/" + strings.TrimPrefix(path, "/"))
	if cleanPath == "/" {
		return "."
	}
	return strings.TrimPrefix(cleanPath, "/")
}

// WalkDir walks the tree under rel path using the root-scoped fs.FS.
func (r *FSRoot) WalkDir(rel string, fn fs.WalkDirFunc) error {
	if r == nil || r.Root == nil {
		return fmt.Errorf("nil root")
	}
	return fs.WalkDir(r.Root.FS(), ToRel(rel), fn)
}

// CreateTemp creates a temporary file relative to dirRel and returns file + relative path.
func (r *FSRoot) CreateTemp(dirRel, pattern string) (*os.File, string, error) {
	if r == nil || r.Root == nil {
		return nil, "", fmt.Errorf("nil root")
	}

	dirRel = ToRel(dirRel)
	prefix, suffix := pattern, ""
	if before, after, ok := strings.Cut(pattern, "*"); ok {
		prefix = before
		suffix = after
	}

	for range 10_000 {
		buf := make([]byte, 6)
		if _, err := rand.Read(buf); err != nil {
			return nil, "", err
		}
		name := prefix + hex.EncodeToString(buf) + suffix
		relPath := filepath.Join(dirRel, name)

		file, err := r.Root.OpenFile(relPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return file, relPath, nil
		}
		if errors.Is(err, os.ErrExist) {
			continue
		}
		return nil, "", err
	}

	return nil, "", fmt.Errorf("failed to create unique temp file for pattern %q", pattern)
}
