package iteminfo

import (
	"cmp"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/handlers/filebrowser/fsroot"
)

func SortItems(items []ItemInfo) {
	slices.SortStableFunc(items, func(a, b ItemInfo) int { return compareNames(a.Name, b.Name) })
}

func SortNames(names []string) {
	slices.SortStableFunc(names, compareNames)
}

// compareNames orders names with a numeric stem ("9", "10.txt") first, by
// number, then everything else case-insensitively; the raw string breaks ties
// so the order is total and stable.
func compareNames(left, right string) int {
	leftStem, _, _ := strings.Cut(left, ".")
	rightStem, _, _ := strings.Cut(right, ".")
	leftNumber, leftErr := strconv.Atoi(leftStem)
	rightNumber, rightErr := strconv.Atoi(rightStem)
	switch {
	case leftErr == nil && rightErr == nil:
		if c := cmp.Compare(leftNumber, rightNumber); c != 0 {
			return c
		}
	case leftErr == nil:
		return -1
	case rightErr == nil:
		return 1
	default:
		if c := strings.Compare(strings.ToLower(left), strings.ToLower(right)); c != 0 {
			return c
		}
	}
	return strings.Compare(left, right)
}

// ResolveSymlinksAt follows the final component of path through any chain of
// symlinks using an already open root and reports the resolved path and
// whether it is a directory.
func ResolveSymlinksAt(ctx context.Context, root *fsroot.FSRoot, path string) (string, bool, error) {
	cleanPath := fsroot.CleanAbsPath(path)
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
				cleanPath = fsroot.CleanAbsPath(target)
			} else {
				cleanPath = filepath.Clean(filepath.Join(filepath.Dir(cleanPath), target))
			}
			continue
		}

		isDir := info.IsDir()
		return cleanPath, isDir, nil
	}
}
