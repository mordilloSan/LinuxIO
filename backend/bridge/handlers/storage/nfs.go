package storage

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
)

// Validation patterns for NFS
var (
	validNFSServer = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)
	validPath      = regexp.MustCompile(`^/[a-zA-Z0-9/_.-]*$`)
)

// fstabEntry contains info parsed from an fstab line
type fstabEntry struct {
	source  string
	fstype  string
	options string
}

// getFstabEntries returns a map of mountpoint -> fstab entry info
func getFstabEntries() map[string]fstabEntry {
	fstabEntries := make(map[string]fstabEntry)
	file, err := os.Open("/etc/fstab")
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
	return fstabEntries
}

// parseNFSSource parses server and export path from NFS source string (server:/path)
func parseNFSSource(source string) (server, exportPath string) {
	colonIdx := strings.Index(source, ":")
	if colonIdx > 0 {
		return source[:colonIdx], source[colonIdx+1:]
	}
	return "", ""
}

// ListNFSMounts returns all mounted NFS shares
func ListNFSMounts() ([]NFSMount, error) {
	partitions, err := disk.Partitions(true)
	if err != nil {
		return nil, err
	}

	// Get fstab entries to check persistence and get source info
	fstabEntries := getFstabEntries()

	var mounts []NFSMount
	for _, p := range partitions {
		if p.Fstype == "nfs" || p.Fstype == "nfs4" {
			source := p.Device
			_, inFstab := fstabEntries[p.Mountpoint]

			// If source is "none" or doesn't contain ":", try to get it from fstab
			if !strings.Contains(source, ":") {
				if entry, ok := fstabEntries[p.Mountpoint]; ok {
					source = entry.source
				}
			}

			server, exportPath := parseNFSSource(source)

			mount := NFSMount{
				Source:     source,
				Server:     server,
				ExportPath: exportPath,
				Mountpoint: p.Mountpoint,
				FSType:     p.Fstype,
				Options:    parseOptions(p.Opts),
				InFstab:    inFstab,
			}

			// Get usage info
			if usage, err := disk.Usage(p.Mountpoint); err == nil {
				mount.Size = usage.Total
				mount.Used = usage.Used
				mount.Free = usage.Free
				mount.UsedPct = usage.UsedPercent
			}

			mounts = append(mounts, mount)
		}
	}
	return mounts, nil
}

// MountNFS mounts an NFS share
func MountNFS(server, exportPath, mountpoint, optionsJSON string, persist bool) (map[string]any, error) {
	// Validate inputs
	if !validNFSServer.MatchString(server) {
		return nil, fmt.Errorf("invalid NFS server hostname")
	}
	if !validPath.MatchString(exportPath) {
		return nil, fmt.Errorf("invalid export path")
	}
	if !validPath.MatchString(mountpoint) {
		return nil, fmt.Errorf("invalid mountpoint")
	}

	// Block dangerous mountpoints
	if isSystemPath(mountpoint) {
		return nil, fmt.Errorf("cannot mount to system path: %s", mountpoint)
	}

	source := fmt.Sprintf("%s:%s", server, exportPath)

	// Create mountpoint if it doesn't exist
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		return nil, fmt.Errorf("failed to create mountpoint: %w", err)
	}

	// Build mount command
	args := []string{"-t", "nfs"}
	if optionsJSON != "" && optionsJSON != "[]" {
		// Parse options (comma-separated string or JSON array)
		options := parseOptionsString(optionsJSON)
		if len(options) > 0 {
			args = append(args, "-o", strings.Join(options, ","))
		}
	}
	args = append(args, source, mountpoint)

	cmd := exec.Command("mount", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("mount failed: %s", strings.TrimSpace(string(out)))
	}

	result := map[string]any{
		"success":    true,
		"mountpoint": mountpoint,
	}

	// Add to fstab if persist is true
	if persist {
		options := parseOptionsString(optionsJSON)
		if err := addToFstab(source, mountpoint, "nfs", options); err != nil {
			result["warning"] = fmt.Sprintf("mount succeeded but fstab update failed: %v", err)
		}
	}

	return result, nil
}

// RemountNFS remounts an NFS share with new options
func RemountNFS(mountpoint, newOptions string, updateFstab bool) (map[string]any, error) {
	// Validate input
	if !validPath.MatchString(mountpoint) {
		return nil, fmt.Errorf("invalid mountpoint")
	}

	// Get current mount info
	partitions, err := disk.Partitions(true)
	if err != nil {
		return nil, fmt.Errorf("failed to get mount info: %w", err)
	}

	var currentMount *disk.PartitionStat
	for _, p := range partitions {
		if p.Mountpoint == mountpoint && (p.Fstype == "nfs" || p.Fstype == "nfs4") {
			currentMount = &p
			break
		}
	}

	if currentMount == nil {
		return nil, fmt.Errorf("NFS mount not found at %s", mountpoint)
	}

	source := currentMount.Device
	fstype := currentMount.Fstype

	// Unmount first
	unmountCmd := exec.Command("umount", mountpoint)
	out, err := unmountCmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("unmount failed: %s", strings.TrimSpace(string(out)))
	}

	// Remount with new options
	args := []string{"-t", fstype}
	options := parseOptionsString(newOptions)
	if len(options) > 0 {
		args = append(args, "-o", strings.Join(options, ","))
	}
	args = append(args, source, mountpoint)

	mountCmd := exec.Command("mount", args...)
	out, err = mountCmd.CombinedOutput()
	if err != nil {
		// Try to remount with original options on failure
		return nil, fmt.Errorf("remount failed: %s", strings.TrimSpace(string(out)))
	}

	result := map[string]any{
		"success":    true,
		"mountpoint": mountpoint,
	}

	// Update fstab if requested
	if updateFstab {
		if err := updateFstabOptions(mountpoint, options); err != nil {
			result["warning"] = fmt.Sprintf("remount succeeded but fstab update failed: %v", err)
		}
	}

	return result, nil
}

// UnmountNFS unmounts an NFS share
func UnmountNFS(mountpoint string, removeFstab bool) (map[string]any, error) {
	// Validate input
	if !validPath.MatchString(mountpoint) {
		return nil, fmt.Errorf("invalid mountpoint")
	}

	cmd := exec.Command("umount", mountpoint)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("umount failed: %s", strings.TrimSpace(string(out)))
	}

	result := map[string]any{"success": true}

	if removeFstab {
		if err := removeFromFstab(mountpoint); err != nil {
			result["warning"] = fmt.Sprintf("unmount succeeded but fstab update failed: %v", err)
		}
	}

	return result, nil
}

// parseOptions converts []string from gopsutil to []string
func parseOptions(opts []string) []string {
	if opts == nil {
		return []string{}
	}
	return opts
}

// parseOptionsString parses options from JSON array or comma-separated string
func parseOptionsString(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" || s == "[]" {
		return []string{}
	}

	// Try JSON array first
	if strings.HasPrefix(s, "[") {
		s = strings.TrimPrefix(s, "[")
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

// isSystemPath checks if a path is a critical system directory
func isSystemPath(path string) bool {
	systemPaths := []string{
		"/", "/bin", "/boot", "/dev", "/etc", "/lib", "/lib64",
		"/proc", "/root", "/run", "/sbin", "/sys", "/tmp", "/usr", "/var",
	}
	path = strings.TrimSuffix(path, "/")
	for _, sp := range systemPaths {
		if path == sp {
			return true
		}
	}
	return false
}

// addToFstab adds an entry to /etc/fstab
func addToFstab(source, mountpoint, fstype string, options []string) error {
	fstabPath := "/etc/fstab"

	// Check if entry already exists
	content, err := os.ReadFile(fstabPath)
	if err != nil {
		return err
	}

	// Check if mountpoint already in fstab
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
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

// updateFstabOptions updates the options for an existing fstab entry
func updateFstabOptions(mountpoint string, options []string) error {
	fstabPath := "/etc/fstab"

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
			// Update this entry with new options
			fields[3] = optStr
			newLines = append(newLines, strings.Join(fields, "\t"))
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

// removeFromFstab removes an entry from /etc/fstab
func removeFromFstab(mountpoint string) error {
	fstabPath := "/etc/fstab"

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
