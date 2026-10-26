package storage

import (
	"path/filepath"
	"strings"
)

func escapeLikePattern(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}

// subtreeBounds returns half-open relative_path bounds [lo, hi) spanning the
// entry at path plus its whole subtree, so the (index_id, relative_path)
// index can be range-seeked instead of scanned with a non-sargable LIKE
// (LIKE cannot use a BINARY-collated index under case-insensitive matching).
// The range also admits sibling paths whose byte after the prefix sorts below
// '/' (e.g. "/a.bak" for "/a"), so subtree queries must add the residual
// filter (relative_path = lo OR relative_path >= childLo). For the root path
// the bounds cover the whole partition and the residual passes everything.
func subtreeBounds(path string) (lo, childLo, hi string) {
	trimmed := strings.TrimRight(path, "/")
	if trimmed == "" {
		return "/", "/", string('/' + 1)
	}
	return trimmed, trimmed + "/", trimmed + string('/'+1)
}

func parentDirKey(path string) string {
	if path == "" || path == "/" {
		return "/"
	}
	trimmed := strings.TrimSuffix(path, "/")
	if trimmed == "" {
		return "/"
	}
	parent := filepath.Dir(trimmed)
	if parent == "." || parent == "" {
		return "/"
	}
	if parent == "/" {
		return "/"
	}
	return "/" + strings.Trim(parent, "/")
}
