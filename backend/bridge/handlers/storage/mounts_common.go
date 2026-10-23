package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
)

// Shared validation patterns for network-mount sources and local paths, used by
// both the NFS and CIFS client handlers.
var (
	validHostname = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)
	validPath     = regexp.MustCompile(`^/[a-zA-Z0-9/_.-]*$`)
)

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

// cleanMountOptions splits comma-joined entries into single options, drops
// blanks, and rejects any option carrying whitespace or control characters:
// those would start a new fstab field or line.
func cleanMountOptions(options []string) ([]string, error) {
	out := make([]string, 0, len(options))
	for _, raw := range options {
		for option := range strings.SplitSeq(raw, ",") {
			option = strings.TrimSpace(option)
			if option == "" {
				continue
			}
			if strings.ContainsFunc(option, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
				return nil, fmt.Errorf("invalid mount option %q", option)
			}
			out = append(out, option)
		}
	}
	return out, nil
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

	return atomicfile.Write(s.path, data, 0o644)
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

// Mount and unmount results used to be built as map[string]any and threaded
// through the helpers below, which appended a "warning" key. The routes always
// declared a struct, so nothing checked that the keys matched. The helpers now
// take a *string warning instead, and these constructors assemble the declared
// type once, at the return.

func mountResult(mountpoint, warning string) apischema.StorageMountResult {
	result := apischema.StorageMountResult{Success: true, Mountpoint: &mountpoint}
	if warning != "" {
		result.Warning = &warning
	}
	return result
}

func warningResult(warning string) apischema.StorageWarningResult {
	result := apischema.StorageWarningResult{Success: true}
	if warning != "" {
		result.Warning = &warning
	}
	return result
}
