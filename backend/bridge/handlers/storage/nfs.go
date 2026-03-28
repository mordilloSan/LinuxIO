package storage

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/mordilloSan/go-logger/logger"
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

func isNFSFSType(fstype string) bool {
	return fstype == "nfs" || fstype == "nfs4"
}

func mountFromFstabEntry(mountpoint string, entry fstabEntry) NFSMount {
	source := entry.source
	server, exportPath := parseNFSSource(source)

	return NFSMount{
		Source:     source,
		Server:     server,
		ExportPath: exportPath,
		Mountpoint: mountpoint,
		FSType:     entry.fstype,
		Options:    parseOptionsString(entry.options),
		InFstab:    true,
		Mounted:    false,
	}
}

// ListNFSExports queries an NFS server for available exports using showmount -e
func ListNFSExports(server string) ([]string, error) {
	// Validate server input
	if !validNFSServer.MatchString(server) {
		logger.Warnf("Invalid server hostname: %s", server)
		return nil, fmt.Errorf("invalid NFS server hostname")
	}

	// Run showmount -e to list exports
	logger.Debugf("Querying exports from server: %s", server)
	cmd := exec.Command("showmount", "-e", server, "--no-headers")
	output, err := cmd.Output()
	if err != nil {
		logger.Errorf("Failed to query exports from %s: %v", server, err)
		return nil, fmt.Errorf("failed to query NFS exports: %v", err)
	}

	var exports []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// showmount output format: "/export/path  client1,client2,..."
		// We only need the path (first field)
		fields := strings.Fields(line)
		if len(fields) >= 1 {
			exports = append(exports, fields[0])
		}
	}

	logger.Debugf("Found %d exports from %s", len(exports), server)
	return exports, nil
}

// ListNFSMounts returns all NFS mount entries, including active mounts and
// persistent /etc/fstab entries that are currently inactive.
func ListNFSMounts() ([]NFSMount, error) {
	partitions, err := disk.Partitions(true)
	if err != nil {
		return nil, err
	}

	fstabEntries := getFstabEntries()
	mountsByMountpoint := make(map[string]NFSMount)

	for _, p := range partitions {
		if !isNFSFSType(p.Fstype) {
			continue
		}

		source := p.Device
		entry, hasFstabEntry := fstabEntries[p.Mountpoint]
		inFstab := hasFstabEntry && isNFSFSType(entry.fstype)

		// Some live mount sources are incomplete; recover the durable source from fstab.
		if (!strings.Contains(source, ":") || source == "none") && inFstab {
			source = entry.source
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
			Mounted:    true,
		}

		if usage, err := disk.Usage(p.Mountpoint); err == nil {
			mount.Size = usage.Total
			mount.Used = usage.Used
			mount.Free = usage.Free
			mount.UsedPct = usage.UsedPercent
		}

		mountsByMountpoint[p.Mountpoint] = mount
	}

	for mountpoint, entry := range fstabEntries {
		if !isNFSFSType(entry.fstype) {
			continue
		}

		existing, exists := mountsByMountpoint[mountpoint]
		if !exists {
			mountsByMountpoint[mountpoint] = mountFromFstabEntry(mountpoint, entry)
			continue
		}

		existing.InFstab = true
		if existing.Source == "" || !strings.Contains(existing.Source, ":") {
			existing.Source = entry.source
			existing.Server, existing.ExportPath = parseNFSSource(entry.source)
		}
		if existing.FSType == "" {
			existing.FSType = entry.fstype
		}
		if len(existing.Options) == 0 {
			existing.Options = parseOptionsString(entry.options)
		}
		mountsByMountpoint[mountpoint] = existing
	}

	mounts := make([]NFSMount, 0, len(mountsByMountpoint))
	for _, mount := range mountsByMountpoint {
		mounts = append(mounts, mount)
	}

	sort.Slice(mounts, func(i, j int) bool {
		if mounts[i].Mounted != mounts[j].Mounted {
			return mounts[i].Mounted
		}
		return mounts[i].Mountpoint < mounts[j].Mountpoint
	})

	return mounts, nil
}

// MountNFS mounts an NFS share
func MountNFS(server, exportPath, mountpoint, optionsJSON string, persist bool) (map[string]any, error) {
	// Validate inputs
	if !validNFSServer.MatchString(server) {
		logger.Warnf("Invalid server hostname: %s", server)
		return nil, fmt.Errorf("invalid NFS server hostname")
	}
	if !validPath.MatchString(exportPath) {
		logger.Warnf("Invalid export path: %s", exportPath)
		return nil, fmt.Errorf("invalid export path")
	}
	if !validPath.MatchString(mountpoint) {
		logger.Warnf("Invalid mountpoint: %s", mountpoint)
		return nil, fmt.Errorf("invalid mountpoint")
	}

	// Block dangerous mountpoints
	if isSystemPath(mountpoint) {
		logger.Warnf("Blocked mount to system path: %s", mountpoint)
		return nil, fmt.Errorf("cannot mount to system path: %s", mountpoint)
	}

	source := fmt.Sprintf("%s:%s", server, exportPath)

	// Create mountpoint if it doesn't exist
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		logger.Errorf("Failed to create mountpoint %s: %v", mountpoint, err)
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

	logger.Infof("Mounting source=%s target=%s options=%v", source, mountpoint, args)
	cmd := exec.Command("mount", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("Mount failed for %s: %s", source, strings.TrimSpace(string(out)))
		return nil, fmt.Errorf("mount failed: %s", strings.TrimSpace(string(out)))
	}

	logger.Infof("Successfully mounted %s at %s", source, mountpoint)

	result := map[string]any{
		"success":    true,
		"mountpoint": mountpoint,
	}

	// Add to fstab if persist is true
	if persist {
		options := parseOptionsString(optionsJSON)
		if err := addToFstab(source, mountpoint, "nfs", options); err != nil {
			logger.Warnf("Mount succeeded but fstab update failed: %v", err)
			result["warning"] = fmt.Sprintf("mount succeeded but fstab update failed: %v", err)
		} else {
			logger.Infof("Added %s to fstab for persistence", mountpoint)
		}
	}

	return result, nil
}

// RemountNFS remounts an NFS share with new options
func RemountNFS(mountpoint, newOptions string, updateFstab bool) (map[string]any, error) {
	// Validate input
	if !validPath.MatchString(mountpoint) {
		logger.Warnf("Invalid mountpoint: %s", mountpoint)
		return nil, fmt.Errorf("invalid mountpoint")
	}

	options := parseOptionsString(newOptions)
	fstabEntries := getFstabEntries()
	entry, inFstab := fstabEntries[mountpoint]

	// Get current mount info
	partitions, err := disk.Partitions(true)
	if err != nil {
		logger.Errorf("Failed to get mount info: %v", err)
		return nil, fmt.Errorf("failed to get mount info: %w", err)
	}

	var currentMount *disk.PartitionStat
	for _, p := range partitions {
		if p.Mountpoint == mountpoint && (p.Fstype == "nfs" || p.Fstype == "nfs4") {
			currentMount = &p
			break
		}
	}

	result := map[string]any{
		"success":    true,
		"mountpoint": mountpoint,
	}

	if currentMount == nil {
		if !inFstab || !isNFSFSType(entry.fstype) {
			logger.Warnf("Mount not found at %s", mountpoint)
			return nil, fmt.Errorf("NFS mount not found at %s", mountpoint)
		}

		if updateFstab {
			if err := updateFstabEntry(mountpoint, entry.source, entry.fstype, options); err != nil {
				logger.Errorf("Failed to update stored NFS config for %s: %v", mountpoint, err)
				return nil, fmt.Errorf("failed to update stored NFS config: %w", err)
			}
			result["warning"] = "mount is not currently active; saved configuration was updated only"
			return result, nil
		}

		if err := removeFromFstab(mountpoint); err != nil {
			logger.Errorf("Failed to remove stored NFS config for %s: %v", mountpoint, err)
			return nil, fmt.Errorf("failed to remove stored NFS config: %w", err)
		}
		result["warning"] = "mount is not currently active; saved configuration was removed"
		return result, nil
	}

	source := currentMount.Device
	fstype := currentMount.Fstype
	if (!strings.Contains(source, ":") || source == "none") && inFstab {
		source = entry.source
	}
	if fstype == "" && inFstab {
		fstype = entry.fstype
	}

	// Unmount first
	logger.Infof("Remount step 1/2: unmounting %s", mountpoint)
	unmountCmd := exec.Command("umount", mountpoint)
	out, err := unmountCmd.CombinedOutput()
	if err != nil {
		logger.Errorf("Unmount failed for %s: %s", mountpoint, strings.TrimSpace(string(out)))
		return nil, fmt.Errorf("unmount failed: %s", strings.TrimSpace(string(out)))
	}

	// Remount with new options
	args := []string{"-t", fstype}
	if len(options) > 0 {
		args = append(args, "-o", strings.Join(options, ","))
	}
	args = append(args, source, mountpoint)

	logger.Infof("Remount step 2/2: mounting source=%s target=%s options=%v", source, mountpoint, args)
	mountCmd := exec.Command("mount", args...)
	out, err = mountCmd.CombinedOutput()
	if err != nil {
		logger.Errorf("Remount failed for %s: %s", mountpoint, strings.TrimSpace(string(out)))
		return nil, fmt.Errorf("remount failed: %s", strings.TrimSpace(string(out)))
	}

	logger.Infof("Successfully remounted %s with new options", mountpoint)

	// Update fstab if requested
	if updateFstab {
		if inFstab {
			if err := updateFstabEntry(mountpoint, source, fstype, options); err != nil {
				logger.Warnf("Remount succeeded but fstab update failed: %v", err)
				result["warning"] = fmt.Sprintf("remount succeeded but fstab update failed: %v", err)
			} else {
				logger.Infof("Updated fstab options for %s", mountpoint)
			}
		} else if err := addToFstab(source, mountpoint, fstype, options); err != nil {
			logger.Warnf("Remount succeeded but fstab update failed: %v", err)
			result["warning"] = fmt.Sprintf("remount succeeded but fstab update failed: %v", err)
		} else {
			logger.Infof("Added %s to fstab", mountpoint)
		}
	} else if inFstab {
		if err := removeFromFstab(mountpoint); err != nil {
			logger.Warnf("Remount succeeded but fstab removal failed: %v", err)
			result["warning"] = fmt.Sprintf("remount succeeded but fstab removal failed: %v", err)
		} else {
			logger.Infof("Removed %s from fstab", mountpoint)
		}
	}

	return result, nil
}

// UnmountNFS unmounts an NFS share
func UnmountNFS(mountpoint string, removeFstab bool) (map[string]any, error) {
	// Validate input
	if !validPath.MatchString(mountpoint) {
		logger.Warnf("Invalid mountpoint: %s", mountpoint)
		return nil, fmt.Errorf("invalid mountpoint")
	}

	fstabEntries := getFstabEntries()
	entry, inFstab := fstabEntries[mountpoint]
	isConfiguredNFS := inFstab && isNFSFSType(entry.fstype)

	partitions, err := disk.Partitions(true)
	if err != nil {
		logger.Errorf("Failed to get mount info: %v", err)
		return nil, fmt.Errorf("failed to get mount info: %w", err)
	}

	isMounted := false
	for _, p := range partitions {
		if p.Mountpoint == mountpoint && isNFSFSType(p.Fstype) {
			isMounted = true
			break
		}
	}

	result := map[string]any{"success": true}

	if !isMounted {
		if removeFstab && isConfiguredNFS {
			if err := removeFromFstab(mountpoint); err != nil {
				logger.Warnf("Failed to remove %s from fstab: %v", mountpoint, err)
				return nil, fmt.Errorf("failed to remove stored mount: %w", err)
			}
			logger.Infof("Removed inactive NFS entry %s from fstab", mountpoint)
			result["warning"] = "mount was not active; removed saved configuration only"
			return result, nil
		}

		logger.Warnf("Mount not found at %s", mountpoint)
		return nil, fmt.Errorf("NFS mount not found at %s", mountpoint)
	}

	logger.Infof("Unmounting %s", mountpoint)
	cmd := exec.Command("umount", mountpoint)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("Unmount failed for %s: %s", mountpoint, strings.TrimSpace(string(out)))
		return nil, fmt.Errorf("umount failed: %s", strings.TrimSpace(string(out)))
	}

	logger.Infof("Successfully unmounted %s", mountpoint)

	if removeFstab {
		if err := removeFromFstab(mountpoint); err != nil {
			logger.Warnf("Unmount succeeded but fstab update failed: %v", err)
			result["warning"] = fmt.Sprintf("unmount succeeded but fstab update failed: %v", err)
		} else {
			logger.Infof("Removed %s from fstab", mountpoint)
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

// isSystemPath checks if a path is a critical system directory
func isSystemPath(path string) bool {
	systemPaths := []string{
		"/", "/bin", "/boot", "/dev", "/etc", "/lib", "/lib64",
		"/proc", "/root", "/run", "/sbin", "/sys", "/tmp", "/usr", "/var",
	}
	path = strings.TrimSuffix(path, "/")
	return slices.Contains(systemPaths, path)
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
