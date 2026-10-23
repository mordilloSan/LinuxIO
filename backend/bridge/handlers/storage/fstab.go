package storage

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

// fstabPath is the system mount table; a var so tests can use a temp file.
var fstabPath = "/etc/fstab"

// fstabEntry contains info parsed from an fstab line.
type fstabEntry struct {
	source  string
	fstype  string
	options string
}

// getFstabEntries returns a map of mountpoint -> fstab entry info.
func getFstabEntries() map[string]fstabEntry {
	fstabEntries := make(map[string]fstabEntry)
	file, err := os.Open(fstabPath)
	if err != nil {
		return fstabEntries
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip comments and empty lines
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 4 {
			fstabEntries[fields[1]] = fstabEntry{
				source:  fields[0],
				fstype:  fields[2],
				options: fields[3],
			}
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Warn("error reading /etc/fstab", "error", err)
	}
	return fstabEntries
}

// replaceFstabOption returns options without any option starting with prefix,
// plus replacement appended when it is not empty. Other options keep order.
func replaceFstabOption(options []string, prefix, replacement string) []string {
	out := make([]string, 0, len(options)+1)
	for _, o := range options {
		if !strings.HasPrefix(o, prefix) {
			out = append(out, o)
		}
	}
	if replacement != "" {
		out = append(out, replacement)
	}
	return out
}

// addToFstab adds an entry to /etc/fstab.
func addToFstab(source, mountpoint, fstype string, options []string) error {
	// Check if entry already exists
	content, err := os.ReadFile(fstabPath)
	if err != nil {
		return err
	}

	// Check if mountpoint already in fstab
	lines := strings.SplitSeq(string(content), "\n")
	for line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == mountpoint {
			// Entry already exists
			return nil
		}
	}

	// Build fstab entry
	optStr := "defaults"
	if len(options) > 0 {
		optStr = strings.Join(options, ",")
	}
	entry := fmt.Sprintf("%s %s %s %s 0 0\n", source, mountpoint, fstype, optStr)

	// Append to fstab
	f, err := os.OpenFile(fstabPath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(entry)
	return err
}

// fstabField matches one whitespace-delimited fstab field.
var fstabField = regexp.MustCompile(`\S+`)

// updateFstabEntry updates an existing fstab entry in-place. Comment lines
// are skipped; the first four fields are replaced where they stand, so the
// original separators, the dump/pass fields and trailing comments survive.
func updateFstabEntry(mountpoint, source, fstype string, options []string) error {
	file, err := os.Open(fstabPath)
	if err != nil {
		return err
	}
	defer file.Close()

	optStr := "defaults"
	if len(options) > 0 {
		optStr = strings.Join(options, ",")
	}
	replacements := [4]string{source, mountpoint, fstype, optStr}

	var newLines []string
	found := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		spans := fstabField.FindAllStringIndex(line, -1)
		if strings.HasPrefix(strings.TrimSpace(line), "#") || len(spans) < 4 || line[spans[1][0]:spans[1][1]] != mountpoint {
			newLines = append(newLines, line)
			continue
		}
		var b strings.Builder
		prev := 0
		for i, r := range replacements {
			b.WriteString(line[prev:spans[i][0]])
			b.WriteString(r)
			prev = spans[i][1]
		}
		b.WriteString(line[prev:])
		if len(spans) == 4 {
			b.WriteString("\t0\t0")
		}
		newLines = append(newLines, b.String())
		found = true
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	if !found {
		return fmt.Errorf("mountpoint not found in fstab")
	}

	return os.WriteFile(fstabPath, []byte(strings.Join(newLines, "\n")+"\n"), 0644)
}

// removeFromFstab removes an entry from /etc/fstab.
func removeFromFstab(mountpoint string) error {
	file, err := os.Open(fstabPath)
	if err != nil {
		return err
	}
	defer file.Close()

	var newLines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		// Keep lines that don't match the mountpoint
		if len(fields) < 2 || fields[1] != mountpoint {
			newLines = append(newLines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	return os.WriteFile(fstabPath, []byte(strings.Join(newLines, "\n")+"\n"), 0644)
}

// upsertFstabEntry updates an existing fstab entry or adds a new one.
func upsertFstabEntry(mountpoint, source, fstype string, options []string, exists bool) error {
	if exists {
		return updateFstabEntry(mountpoint, source, fstype, options)
	}
	return addToFstab(source, mountpoint, fstype, options)
}
