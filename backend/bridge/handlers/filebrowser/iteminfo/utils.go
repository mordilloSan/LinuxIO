package iteminfo

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/fsroot"
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/linksafe"
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

// ResolveSymlinksAt resolves path for a read: every symlink on the way to
// it and at it, through any chain, is followed as the bridge's own user would
// follow it (see linksafe.Resolve), a link with an absolute target included.
// It reports the real path, which holds no link, and whether it is a
// directory. A read follows links on a root bridge too: what it finds goes to
// the admin who asked, never into a folder another user can write.
func ResolveSymlinksAt(ctx context.Context, root *fsroot.FSRoot, path string) (string, bool, error) {
	cleanPath := fsroot.CleanAbsPath(path)
	if err := ctx.Err(); err != nil {
		return cleanPath, false, err
	}
	resolved, err := linksafe.Resolve(ctx, cleanPath, linksafe.Owner{})
	if err != nil {
		return cleanPath, false, fmt.Errorf("could not resolve path %s: %w", cleanPath, err)
	}
	if err = ctx.Err(); err != nil {
		return resolved, false, err
	}
	info, err := root.Root.Lstat(fsroot.ToRel(resolved))
	if err != nil {
		return resolved, false, fmt.Errorf("could not stat path %s: %w", resolved, err)
	}
	return resolved, info.IsDir(), nil
}
