// Package octalescape decodes the \ooo escapes in the path fields of
// /proc/self/mountinfo, /proc/swaps and /etc/fstab.
package octalescape

import (
	"strconv"
	"strings"
)

// Unescape decodes each \ooo octal escape in s, the form in which the kernel
// writes a space, tab, newline or backslash in a path (a space is \040) and
// fstab(5) reads one. A backslash not followed by three octal digits worth at
// most \377 is kept as it is.
func Unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
