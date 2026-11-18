package memory

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"strings"
)

// unchangedConffile reports whether data is the file at path as its package
// shipped it: dpkg records the MD5 of each conffile in the Conffiles field of
// the owning package's stanza in /var/lib/dpkg/status, a line of the path,
// the checksum and flags. Only an installed package's record counts: after
// a rename the old package can stay in config-files state, listing the file
// with its old checksum. An entry marked obsolete is a file the package no
// longer ships, and a host without dpkg, or a file no installed package
// owns, has no record: none of them counts as unchanged.
func (h hostRoot) unchangedConffile(path string, data []byte) bool {
	sum, ok := h.dpkgConffileSum(path)
	if !ok {
		return false
	}
	got := md5.Sum(data)
	return sum == hex.EncodeToString(got[:])
}

// dpkgConffileSum is the checksum an installed package records for the
// conffile at path, or false when none does. The status file is read as a
// stream, up to the stanza that decides.
func (h hostRoot) dpkgConffileSum(path string) (string, bool) {
	f, err := os.Open(h.path(dpkgStatusPath))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Debug("read dpkg status failed", "path", dpkgStatusPath, "error", err)
		}
		return "", false
	}
	defer f.Close()
	var stanza dpkgStanza
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if sum, ok := stanza.decides(); ok {
				return sum, true
			}
			stanza = dpkgStanza{}
			continue
		}
		stanza.read(line, path)
	}
	if err := scanner.Err(); err != nil {
		slog.Debug("read dpkg status failed", "path", dpkgStatusPath, "error", err)
		return "", false
	}
	return stanza.decides()
}

// dpkgStanza is what one package's stanza of dpkg's status says about one
// conffile.
type dpkgStanza struct {
	installed, inConffiles bool
	sum                    string
}

// read takes one line of the stanza: the Status field, and the Conffiles
// field's continuation lines, which start with a space, one per file.
func (s *dpkgStanza) read(line, path string) {
	if !strings.HasPrefix(line, " ") {
		s.inConffiles = strings.TrimSpace(line) == "Conffiles:"
		if status, ok := strings.CutPrefix(line, "Status:"); ok {
			s.installed = strings.HasSuffix(strings.TrimSpace(status), " installed")
		}
		return
	}
	fields := strings.Fields(line)
	if s.inConffiles && len(fields) >= 2 && fields[0] == path && !slices.Contains(fields[2:], "obsolete") {
		s.sum = fields[1]
	}
}

// decides reports the checksum when the stanza is an installed package's
// and records the file.
func (s dpkgStanza) decides() (string, bool) {
	return s.sum, s.installed && s.sum != ""
}
