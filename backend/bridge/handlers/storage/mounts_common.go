package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/utils"
)

// Shared validation patterns for network-mount sources and local paths, used by
// both the NFS and CIFS client handlers.
var (
	validHostname = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)
	validPath     = regexp.MustCompile(`^/[a-zA-Z0-9/_.-]*$`)
)

// mountCommandSearchDirs are the sbin/bin locations consulted when a mount
// helper is not on the bridge's PATH. Tools like mount.nfs, mount.cifs,
// showmount and smbclient typically live in /usr/sbin or /sbin, which are not
// always exported to the environment the bridge runs under.
var mountCommandSearchDirs = []string{"/usr/sbin", "/sbin", "/usr/bin", "/bin"}

// fstabPath is the system mount table; a var so tests can use a temp file.
var fstabPath = "/etc/fstab"

// findMountCommand resolves command via PATH, falling back to the well-known
// sbin/bin directories. Returns exec.ErrNotFound when the command is absent.
func findMountCommand(command string) (string, error) {
	if path, err := exec.LookPath(command); err == nil {
		return path, nil
	}
	for _, dir := range mountCommandSearchDirs {
		path := filepath.Join(dir, command)
		if info, err := os.Stat(path); err == nil && !info.IsDir() && info.Mode()&0111 != 0 {
			return path, nil
		}
	}
	return "", exec.ErrNotFound
}

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

// parseOptions converts []string from gopsutil to []string.
func parseOptions(opts []string) []string {
	if opts == nil {
		return []string{}
	}
	return opts
}

// parseOptionsString parses options from a JSON array or comma-separated string.
func parseOptionsString(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return []string{}
	}

	// Try JSON array first
	if after, ok := strings.CutPrefix(s, "["); ok {
		s = after
		s = strings.TrimSuffix(s, "]")
		s = strings.ReplaceAll(s, "\"", "")
	}

	// Split by comma
	parts := strings.Split(s, ",")
	var options []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			options = append(options, p)
		}
	}
	return options
}

func cleanMountOptions(options []string) []string {
	out := make([]string, 0, len(options))
	for _, option := range options {
		option = strings.TrimSpace(option)
		if option != "" {
			out = append(out, option)
		}
	}
	return out
}

// isSystemPath checks if a path is a critical system directory.
func isSystemPath(path string) bool {
	systemPaths := []string{
		"/", "/bin", "/boot", "/dev", "/etc", "/lib", "/lib64",
		"/proc", "/root", "/run", "/sbin", "/sys", "/tmp", "/usr", "/var",
	}
	path = strings.TrimSuffix(path, "/")
	return slices.Contains(systemPaths, path)
}

// sensitiveOptionKeys are mount option keys whose values must never be logged
// and must not be accepted from user-supplied custom options (auth is set only
// from the dedicated credential fields).
var sensitiveOptionKeys = map[string]bool{
	"password":    true,
	"pass":        true,
	"passwd":      true,
	"credentials": true,
	"cred":        true,
	"username":    true,
	"user":        true,
	"domain":      true,
	"sec":         true,
	"guest":       true,
}

// rejectSensitiveCustomOptions returns an error if user-supplied custom options
// try to set authentication out-of-band; those must come from the dedicated
// credential fields, never the free-text options box.
func rejectSensitiveCustomOptions(options []string) error {
	for _, o := range options {
		key, _, _ := strings.Cut(o, "=")
		key = strings.ToLower(strings.TrimSpace(key))
		if sensitiveOptionKeys[key] {
			return fmt.Errorf("option %q is not allowed in custom options; use the dedicated credential fields", key)
		}
	}
	return nil
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

// updateFstabEntry updates an existing fstab entry in-place.
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

	var newLines []string
	found := false
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) >= 4 && fields[1] == mountpoint {
			newLines = append(newLines, fmt.Sprintf("%s\t%s\t%s\t%s\t0\t0", source, mountpoint, fstype, optStr))
			found = true
		} else {
			newLines = append(newLines, line)
		}
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

func runMountOutput(parent context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return nil, wrapMountCommandError(ctx, timeout, name, err)
	}

	return output, nil
}

func runMountCombinedOutput(parent context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		return output, wrapMountCommandError(ctx, timeout, name, err)
	}

	return output, nil
}

func wrapMountCommandError(ctx context.Context, timeout time.Duration, name string, err error) error {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return fmt.Errorf("%s timed out after %s", name, timeout)
	case errors.Is(ctx.Err(), context.Canceled):
		return fmt.Errorf("%s canceled", name)
	default:
		return err
	}
}

func commandFailureMessage(output []byte, err error) string {
	if trimmed := strings.TrimSpace(string(output)); trimmed != "" {
		return trimmed
	}
	return err.Error()
}

// managedMountEntry is one LinuxIO-managed network mount that is not recorded in
// /etc/fstab (currently NFS temporary mounts).
type managedMountEntry struct {
	Source     string   `json:"source"`
	Mountpoint string   `json:"mountpoint"`
	FSType     string   `json:"fsType"`
	Options    []string `json:"options"`
}

// managedMountStore is a small JSON-backed registry of managed mounts keyed by
// mountpoint, used for NFS temporary (non-fstab) mounts (see nfsMountStore). It
// stores FSType verbatim; callers normalize on read.
type managedMountStore struct {
	path string
}

func (s *managedMountStore) load() (map[string]managedMountEntry, error) {
	entries := make(map[string]managedMountEntry)

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return entries, nil
		}
		return nil, err
	}

	if strings.TrimSpace(string(data)) == "" {
		return entries, nil
	}

	var stored []managedMountEntry
	if err := json.Unmarshal(data, &stored); err != nil {
		return nil, err
	}

	for _, entry := range stored {
		if entry.Mountpoint == "" || entry.Source == "" {
			continue
		}
		entry.Options = append([]string(nil), entry.Options...)
		entries[entry.Mountpoint] = entry
	}

	return entries, nil
}

func (s *managedMountStore) save(entries map[string]managedMountEntry) error {
	stored := make([]managedMountEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Mountpoint == "" || entry.Source == "" {
			continue
		}
		entry.Options = append([]string(nil), entry.Options...)
		stored = append(stored, entry)
	}

	if len(stored) == 0 {
		if err := os.Remove(s.path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}

	sort.Slice(stored, func(i, j int) bool {
		return stored[i].Mountpoint < stored[j].Mountpoint
	})

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	return utils.WriteFileAtomic(s.path, data, 0o644)
}

func (s *managedMountStore) upsert(entry managedMountEntry) error {
	entries, err := s.load()
	if err != nil {
		return err
	}

	entry.Options = append([]string(nil), entry.Options...)
	entries[entry.Mountpoint] = entry

	return s.save(entries)
}

func (s *managedMountStore) remove(mountpoint string) error {
	entries, err := s.load()
	if err != nil {
		return err
	}

	delete(entries, mountpoint)
	return s.save(entries)
}
