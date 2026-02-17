package iteminfo

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
)

func (info *FileInfo) SortItems() {
	sort.Slice(info.Folders, func(i, j int) bool {
		nameWithoutExt := strings.Split(info.Folders[i].Name, ".")[0]
		nameWithoutExt2 := strings.Split(info.Folders[j].Name, ".")[0]
		// Convert strings to integers for numeric sorting if both are numeric
		numI, errI := strconv.Atoi(nameWithoutExt)
		numJ, errJ := strconv.Atoi(nameWithoutExt2)
		if errI == nil && errJ == nil {
			return numI < numJ
		}
		// Fallback to case-insensitive lexicographical sorting
		return strings.ToLower(info.Folders[i].Name) < strings.ToLower(info.Folders[j].Name)
	})
	sort.Slice(info.Files, func(i, j int) bool {
		nameWithoutExt := strings.Split(info.Files[i].Name, ".")[0]
		nameWithoutExt2 := strings.Split(info.Files[j].Name, ".")[0]
		// Convert strings to integers for numeric sorting if both are numeric
		numI, errI := strconv.Atoi(nameWithoutExt)
		numJ, errJ := strconv.Atoi(nameWithoutExt2)
		if errI == nil && errJ == nil {
			return numI < numJ
		}
		// Fallback to case-insensitive lexicographical sorting
		return strings.ToLower(info.Files[i].Name) < strings.ToLower(info.Files[j].Name)
	})
}

// ResolveSymlinks resolves symlinks in the given path and returns
// the final resolved path, whether it's a directory (considering bundle logic), and any error.
func ResolveSymlinks(path string) (string, bool, error) {
	root, err := fsroot.Open()
	if err != nil {
		return path, false, fmt.Errorf("could not open filesystem root: %v", err)
	}
	defer root.Close()

	cleanPath := filepath.Clean("/" + strings.TrimPrefix(path, "/"))
	visited := make(map[string]struct{})

	for {
		if _, seen := visited[cleanPath]; seen {
			return cleanPath, false, fmt.Errorf("detected symlink loop at %s", cleanPath)
		}
		visited[cleanPath] = struct{}{}

		relPath := fsroot.ToRel(cleanPath)
		info, err := root.Root.Lstat(relPath)
		if err != nil {
			return cleanPath, false, fmt.Errorf("could not stat path: %s, %v", cleanPath, err)
		}

		if info.Mode()&os.ModeSymlink != 0 {
			target, err := root.Root.Readlink(relPath)
			if err != nil {
				return cleanPath, false, fmt.Errorf("could not read symlink: %s, %v", cleanPath, err)
			}

			if filepath.IsAbs(target) {
				cleanPath = filepath.Clean("/" + strings.TrimPrefix(target, "/"))
			} else {
				cleanPath = filepath.Clean(filepath.Join(filepath.Dir(cleanPath), target))
			}
			continue
		}

		isDir := IsDirectory(info)
		return cleanPath, isDir, nil
	}
}
