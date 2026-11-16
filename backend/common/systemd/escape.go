package systemd

import (
	"errors"
	"fmt"
	"strings"
)

// EscapePath names a path the way systemd-escape --path does, which is how
// systemd names the mount, swap and device units of a path: repeated
// slashes and "." components collapse, the outer slashes go, inner slashes
// become "-", and every byte outside [A-Za-z0-9:_.] becomes \xNN, as does a
// leading ".". "/" is "-". A path with a ".." component is refused, as
// systemd refuses it, and so is a relative path, which names no unit.
func EscapePath(path string) (string, error) {
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("escape %q: not an absolute path", path)
	}
	var parts []string
	for part := range strings.SplitSeq(path, "/") {
		switch part {
		case "", ".":
			continue
		case "..":
			return "", fmt.Errorf("escape %q: %w", path, errNotNormalized)
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return "-", nil
	}
	var b strings.Builder
	for i, c := range []byte(strings.Join(parts, "/")) {
		switch {
		case c == '/':
			b.WriteByte('-')
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == ':', c == '_', c == '.' && i > 0:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, `\x%02x`, c)
		}
	}
	return b.String(), nil
}

var errNotNormalized = errors.New("a path with a .. component is not normalized")
