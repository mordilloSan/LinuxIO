package iteminfo

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

func SortItems(items []ItemInfo) {
	sort.Slice(items, func(i, j int) bool { return lessName(items[i].Name, items[j].Name) })
}

func SortNames(names []string) {
	sort.Slice(names, func(i, j int) bool { return lessName(names[i], names[j]) })
}

func lessName(left, right string) bool {
	leftStem, _, _ := strings.Cut(left, ".")
	rightStem, _, _ := strings.Cut(right, ".")
	leftNumber, leftErr := strconv.Atoi(leftStem)
	rightNumber, rightErr := strconv.Atoi(rightStem)
	if leftErr == nil && rightErr == nil {
		return leftNumber < rightNumber
	}
	return strings.ToLower(left) < strings.ToLower(right)
}

// ResolveSymlinksAt follows the final component of path through any chain of
// symlinks using an already open root and reports the resolved path and
// whether it is a directory.
func ResolveSymlinksAt(ctx context.Context, root *fsroot.FSRoot, path string) (string, bool, error) {
	cleanPath := utils.CleanAbsPath(path)
	visited := make(map[string]struct{})

	for {
		if err := ctx.Err(); err != nil {
			return cleanPath, false, err
		}
		if _, seen := visited[cleanPath]; seen {
			return cleanPath, false, fmt.Errorf("detected symlink loop at %s", cleanPath)
		}
		visited[cleanPath] = struct{}{}

		relPath := fsroot.ToRel(cleanPath)
		info, err := root.Root.Lstat(relPath)
		if err != nil {
			return cleanPath, false, fmt.Errorf("could not stat path %s: %w", cleanPath, err)
		}

		if info.Mode()&os.ModeSymlink != 0 {
			if err := ctx.Err(); err != nil {
				return cleanPath, false, err
			}
			target, err := root.Root.Readlink(relPath)
			if err != nil {
				return cleanPath, false, fmt.Errorf("could not read symlink %s: %w", cleanPath, err)
			}

			if filepath.IsAbs(target) {
				cleanPath = utils.CleanAbsPath(target)
			} else {
				cleanPath = filepath.Clean(filepath.Join(filepath.Dir(cleanPath), target))
			}
			continue
		}

		isDir := IsDirectory(info)
		return cleanPath, isDir, nil
	}
}
