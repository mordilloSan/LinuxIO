// Package osrelease parses /etc/os-release style KEY=value files.
package osrelease

import (
	"os"
	"strings"
)

// Path is the host's os-release file.
const Path = "/etc/os-release"

// Info holds the file's variables with surrounding quotes removed.
type Info map[string]string

// Read parses Path.
func Read() (Info, error) {
	data, err := os.ReadFile(Path)
	if err != nil {
		return nil, err
	}
	return Parse(data), nil
}

// Parse reads KEY=value lines, skipping blanks and comments. Later duplicates
// win. It does not process escapes inside values.
func Parse(data []byte) Info {
	info := make(Info)
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		info[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	return info
}

// ID is the lower-cased ID, such as "debian" or "rocky".
func (i Info) ID() string { return strings.ToLower(i["ID"]) }

// IDLike is the lower-cased ID_LIKE parent distributions.
func (i Info) IDLike() []string { return strings.Fields(strings.ToLower(i["ID_LIKE"])) }
