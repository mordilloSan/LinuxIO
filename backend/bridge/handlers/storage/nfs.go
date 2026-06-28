package storage

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

const (
	nfsExportCommandTimeout  = 10 * time.Second
	nfsMountCommandTimeout   = 30 * time.Second
	nfsUnmountCommandTimeout = 15 * time.Second
)

var nfsMountStore = &managedMountStore{path: "/var/lib/linuxio/nfs-mounts.json"}

var requiredNFSClientCommands = []string{"showmount", "mount.nfs"}

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

func checkNFSCommands(commands []string) (bool, error) {
	for _, command := range commands {
		if _, err := findMountCommand(command); err != nil {
			return false, fmt.Errorf("%s not found (install %s)", command, nfsCommandInstallHint(command))
		}
	}
	return true, nil
}

// CheckNFSClientAvailability verifies that the optional NFS client utilities are installed.
func CheckNFSClientAvailability() (bool, error) {
	return checkNFSCommands(requiredNFSClientCommands)
}

func nfsCommandInstallHint(command string) string {
	if command == "exportfs" {
		return "nfs-kernel-server or nfs-utils"
	}
	return "nfs-common or nfs-utils"
}

func requireNFSClientAvailability() error {
	ok, err := CheckNFSClientAvailability()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("NFS utilities are unavailable")
	}
	return nil
}

func mountFromFstabEntry(mountpoint string, entry fstabEntry) apischema.NFSMount {
	source := entry.source
	server, exportPath := parseNFSSource(source)

	return apischema.NFSMount{
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

func mountFromManagedEntry(entry managedMountEntry) apischema.NFSMount {
	server, exportPath := parseNFSSource(entry.Source)
	fstype := entry.FSType
	if !isNFSFSType(fstype) {
		fstype = "nfs"
	}

	return apischema.NFSMount{
		Source:     entry.Source,
		Server:     server,
		ExportPath: exportPath,
		Mountpoint: entry.Mountpoint,
		FSType:     fstype,
		Options:    append([]string(nil), entry.Options...),
		InFstab:    false,
		Mounted:    false,
	}
}

// ListNFSExports queries an NFS server for available exports using showmount -e
func ListNFSExports(ctx context.Context, server string) ([]string, error) {
	if err := requireNFSClientAvailability(); err != nil {
		return nil, err
	}

	// Validate server input
	if !validHostname.MatchString(server) {
		slog.Warn("invalid NFS server hostname", "server", server)
		return nil, fmt.Errorf("invalid NFS server hostname")
	}
	// Run showmount -e to list exports.
	slog.Debug("querying NFS exports", "server", server)
	showmount, err := findMountCommand("showmount")
	if err != nil {
		return nil, fmt.Errorf("%s not found (install %s)", "showmount", nfsCommandInstallHint("showmount"))
	}

	output, err := runMountOutput(ctx, nfsExportCommandTimeout, showmount, "-e", server, "--no-headers")
	if err != nil {
		slog.Error("failed to query NFS exports", "server", server, "error", err)
		return nil, fmt.Errorf("failed to query NFS exports: %w", err)
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
	slog.Debug("listed NFS exports", "server", server, "count", len(exports))
	return exports, scanner.Err()
}

// ListNFSMounts returns all NFS mount entries, including active mounts and
// persistent /etc/fstab entries that are currently inactive.
func ListNFSMounts(ctx context.Context) ([]apischema.NFSMount, error) {
	partitions, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		return nil, err
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	fstabEntries := getFstabEntries()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	managedEntries, err := nfsMountStore.load()
	if err != nil {
		slog.Warn("failed to read managed NFS mount registry", "error", err)
		managedEntries = make(map[string]managedMountEntry)
	}

	mountsByMountpoint := collectActiveMounts(partitions, fstabEntries)
	mergeFstabMounts(mountsByMountpoint, fstabEntries)
	mergeManagedMounts(mountsByMountpoint, managedEntries)

	mounts := make([]apischema.NFSMount, 0, len(mountsByMountpoint))
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

func collectActiveMounts(partitions []disk.PartitionStat, fstabEntries map[string]fstabEntry) map[string]apischema.NFSMount {
	result := make(map[string]apischema.NFSMount)
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
		mount := apischema.NFSMount{
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

		result[p.Mountpoint] = mount
	}
	return result
}

func mergeFstabMounts(mounts map[string]apischema.NFSMount, fstabEntries map[string]fstabEntry) {
	for mountpoint, entry := range fstabEntries {
		if !isNFSFSType(entry.fstype) {
			continue
		}

		existing, exists := mounts[mountpoint]
		if !exists {
			mounts[mountpoint] = mountFromFstabEntry(mountpoint, entry)
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
		mounts[mountpoint] = existing
	}
}

func mergeManagedMounts(mounts map[string]apischema.NFSMount, managedEntries map[string]managedMountEntry) {
	for mountpoint, entry := range managedEntries {
		existing, exists := mounts[mountpoint]
		if !exists {
			mounts[mountpoint] = mountFromManagedEntry(entry)
			continue
		}

		if existing.Source == "" || !strings.Contains(existing.Source, ":") {
			existing.Source = entry.Source
			existing.Server, existing.ExportPath = parseNFSSource(entry.Source)
		}
		if existing.FSType == "" {
			existing.FSType = entry.FSType
		}
		if len(existing.Options) == 0 {
			existing.Options = append([]string(nil), entry.Options...)
		}
		mounts[mountpoint] = existing
	}
}

// MountNFS mounts an NFS share
func MountNFS(ctx context.Context, server, exportPath, mountpoint string, options []string, persist bool) (map[string]any, error) {
	if err := requireNFSClientAvailability(); err != nil {
		return nil, err
	}

	if err := validateNFSMountRequest(server, exportPath, mountpoint); err != nil {
		return nil, err
	}

	source := fmt.Sprintf("%s:%s", server, exportPath)
	options = cleanMountOptions(options)

	// Create mountpoint if it doesn't exist
	if err := os.MkdirAll(mountpoint, 0755); err != nil {
		slog.Error("failed to create mountpoint", "mountpoint", mountpoint, "error", err)
		return nil, fmt.Errorf("failed to create mountpoint: %w", err)
	}

	args := buildNFSMountArgs(source, mountpoint, options)
	slog.Info("mounting NFS share", "source", source, "mountpoint", mountpoint, "options", args)
	out, err := runMountCombinedOutput(ctx, nfsMountCommandTimeout, "mount", args...)
	if err != nil {
		message := commandFailureMessage(out, err)
		slog.Error("NFS mount failed", "source", source, "mountpoint", mountpoint, "message", message)
		return nil, fmt.Errorf("mount failed: %s", message)
	}
	slog.Info("NFS share mounted", "source", source, "mountpoint", mountpoint)

	result := map[string]any{
		"success":    true,
		"mountpoint": mountpoint,
	}

	recordSuccessfulNFSMount(result, source, mountpoint, options, persist)
	return result, nil
}

func validateNFSMountRequest(server, exportPath, mountpoint string) error {
	if !validHostname.MatchString(server) {
		slog.Warn("invalid NFS server hostname", "server", server)
		return fmt.Errorf("invalid NFS server hostname")
	}
	if !validPath.MatchString(exportPath) {
		slog.Warn("invalid NFS export path", "path", exportPath)
		return fmt.Errorf("invalid export path")
	}
	if !validPath.MatchString(mountpoint) {
		slog.Warn("invalid mountpoint", "mountpoint", mountpoint)
		return fmt.Errorf("invalid mountpoint")
	}
	if isSystemPath(mountpoint) {
		slog.Warn("blocked mount to system path", "mountpoint", mountpoint)
		return fmt.Errorf("cannot mount to system path: %s", mountpoint)
	}
	return nil
}

func buildNFSMountArgs(source, mountpoint string, options []string) []string {
	args := []string{"-t", "nfs"}
	if len(options) > 0 {
		args = append(args, "-o", strings.Join(options, ","))
	}
	return append(args, source, mountpoint)
}

func recordSuccessfulNFSMount(result map[string]any, source, mountpoint string, options []string, persist bool) {
	if persist {
		recordPersistentNFSMount(result, source, mountpoint, options)
		return
	}
	if err := nfsMountStore.upsert(managedMountEntry{Source: source, Mountpoint: mountpoint, FSType: "nfs", Options: options}); err != nil {
		slog.Warn("mount succeeded but LinuxIO registry update failed", "mountpoint", mountpoint, "error", err)
		result["warning"] = fmt.Sprintf("mount succeeded but LinuxIO registry update failed: %v", err)
	}
}

func recordPersistentNFSMount(result map[string]any, source, mountpoint string, options []string) {
	if err := addToFstab(source, mountpoint, "nfs", options); err != nil {
		slog.Warn("mount succeeded but fstab update failed", "mountpoint", mountpoint, "error", err)
		result["warning"] = fmt.Sprintf("mount succeeded but fstab update failed: %v", err)
		if err := nfsMountStore.upsert(managedMountEntry{Source: source, Mountpoint: mountpoint, FSType: "nfs", Options: options}); err != nil {
			slog.Warn("failed to persist temporary NFS mount metadata", "mountpoint", mountpoint, "error", err)
		}
		return
	}
	slog.Info("added NFS mount to fstab", "mountpoint", mountpoint)
	if err := nfsMountStore.remove(mountpoint); err != nil {
		slog.Warn("failed to clean up temporary NFS mount metadata", "mountpoint", mountpoint, "error", err)
	}
}

// RemountNFS remounts an NFS share with new options
func RemountNFS(ctx context.Context, mountpoint string, newOptions []string, updateFstab bool) (map[string]any, error) {
	if err := requireNFSClientAvailability(); err != nil {
		return nil, err
	}

	// Validate input
	if !validPath.MatchString(mountpoint) {
		slog.Warn("invalid mountpoint", "mountpoint", mountpoint)
		return nil, fmt.Errorf("invalid mountpoint")
	}

	options := cleanMountOptions(newOptions)
	fstabEntries := getFstabEntries()
	entry, inFstab := fstabEntries[mountpoint]
	managedEntries, err := nfsMountStore.load()
	if err != nil {
		slog.Warn("failed to read managed NFS mount registry", "error", err)
		managedEntries = make(map[string]managedMountEntry)
	}
	managedEntry, inManagedRegistry := managedEntries[mountpoint]

	// Get current mount info
	partitions, err := disk.Partitions(true)
	if err != nil {
		slog.Error("failed to get NFS mount info", "error", err)
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
		return remountInactiveNFS(mountpoint, entry, inFstab, managedEntry, inManagedRegistry, updateFstab, options, result)
	}

	source := currentMount.Device
	fstype := currentMount.Fstype
	if (!strings.Contains(source, ":") || source == "none") && inFstab {
		source = entry.source
	}
	if fstype == "" && inFstab {
		fstype = entry.fstype
	}
	// Unmount first.
	slog.Info("remount step 1/2", "mountpoint", mountpoint, "action", "unmount")
	out, err := runMountCombinedOutput(ctx, nfsUnmountCommandTimeout, "umount", mountpoint)
	if err != nil {
		message := commandFailureMessage(out, err)
		slog.Error("NFS unmount during remount failed", "mountpoint", mountpoint, "message", message)
		return nil, fmt.Errorf("unmount failed: %s", message)
	}

	// Remount with new options
	args := []string{"-t", fstype}
	if len(options) > 0 {
		args = append(args, "-o", strings.Join(options, ","))
	}
	args = append(args, source, mountpoint)
	slog.Info("remount step 2/2", "source", source, "mountpoint", mountpoint, "options", args)
	out, err = runMountCombinedOutput(ctx, nfsMountCommandTimeout, "mount", args...)
	if err != nil {
		message := commandFailureMessage(out, err)
		slog.Error("NFS remount failed", "mountpoint", mountpoint, "message", message)
		return nil, fmt.Errorf("remount failed: %s", message)
	}
	slog.Info("NFS share remounted", "mountpoint", mountpoint)
	persistNFSConfig("remount", mountpoint, source, fstype, options, updateFstab, inFstab, result)
	return result, nil
}

func remountInactiveNFS(
	mountpoint string, entry fstabEntry, inFstab bool,
	managedEntry managedMountEntry, inManagedRegistry, updateFstab bool,
	options []string, result map[string]any,
) (map[string]any, error) {
	if (!inFstab || !isNFSFSType(entry.fstype)) && !inManagedRegistry {
		slog.Warn("NFS mount not found", "mountpoint", mountpoint)
		return nil, fmt.Errorf("NFS mount not found at %s", mountpoint)
	}

	source := entry.source
	fstype := entry.fstype
	if inManagedRegistry {
		if source == "" {
			source = managedEntry.Source
		}
		if fstype == "" {
			fstype = managedEntry.FSType
		}
	}
	if !isNFSFSType(fstype) {
		fstype = "nfs"
	}

	isConfiguredNFS := inFstab && isNFSFSType(entry.fstype)

	if updateFstab {
		if err := upsertFstabEntry(mountpoint, source, fstype, options, isConfiguredNFS); err != nil {
			slog.Error("failed to update stored NFS config", "mountpoint", mountpoint, "error", err)
			return nil, fmt.Errorf("failed to update stored NFS config: %w", err)
		}
		if err := nfsMountStore.remove(mountpoint); err != nil {
			slog.Warn("failed to clean up temporary NFS mount metadata", "mountpoint", mountpoint, "error", err)
		}
		result["warning"] = "mount is not currently active; saved configuration was updated only"
		return result, nil
	}

	if isConfiguredNFS {
		if err := removeFromFstab(mountpoint); err != nil {
			slog.Error("failed to remove stored NFS config", "mountpoint", mountpoint, "error", err)
			return nil, fmt.Errorf("failed to remove stored NFS config: %w", err)
		}
	}
	if err := nfsMountStore.upsert(managedMountEntry{Source: source, Mountpoint: mountpoint, FSType: fstype, Options: options}); err != nil {
		slog.Error("failed to update LinuxIO NFS registry", "mountpoint", mountpoint, "error", err)
		return nil, fmt.Errorf("failed to update LinuxIO NFS registry: %w", err)
	}
	result["warning"] = "mount is not currently active; saved configuration was updated only"
	return result, nil
}

// persistNFSConfig updates fstab and/or the managed NFS registry after a
// successful mount/unmount operation. Warnings are stored in result["warning"].
func persistNFSConfig(op, mountpoint, source, fstype string, options []string, wantFstab, inFstab bool, result map[string]any) {
	if wantFstab {
		if err := upsertFstabEntry(mountpoint, source, fstype, options, inFstab); err != nil {
			slog.Warn("NFS operation succeeded but fstab update failed", "operation", op, "mountpoint", mountpoint, "error", err)
			result["warning"] = fmt.Sprintf("%s succeeded but fstab update failed: %v", op, err)
		} else {
			if inFstab {
				slog.Info("updated fstab options", "mountpoint", mountpoint)
			} else {
				slog.Info("added mount to fstab", "mountpoint", mountpoint)
			}
			if err := nfsMountStore.remove(mountpoint); err != nil {
				slog.Warn("failed to clean up temporary NFS mount metadata", "mountpoint", mountpoint, "error", err)
			}
		}
		return
	}

	if inFstab {
		if err := removeFromFstab(mountpoint); err != nil {
			slog.Warn("NFS operation succeeded but fstab removal failed", "operation", op, "mountpoint", mountpoint, "error", err)
			result["warning"] = fmt.Sprintf("%s succeeded but fstab removal failed: %v", op, err)
			return
		}
		slog.Info("removed mount from fstab", "mountpoint", mountpoint)
	}
	if err := nfsMountStore.upsert(managedMountEntry{Source: source, Mountpoint: mountpoint, FSType: fstype, Options: options}); err != nil {
		slog.Warn("NFS operation succeeded but LinuxIO registry update failed", "operation", op, "mountpoint", mountpoint, "error", err)
		result["warning"] = fmt.Sprintf("%s succeeded but LinuxIO registry update failed: %v", op, err)
	}
}

// UnmountNFS unmounts an NFS share
func UnmountNFS(ctx context.Context, mountpoint string, removeFstab bool) (map[string]any, error) {
	// Validate input
	if !validPath.MatchString(mountpoint) {
		slog.Warn("invalid mountpoint", "mountpoint", mountpoint)
		return nil, fmt.Errorf("invalid mountpoint")
	}

	fstabEntries := getFstabEntries()
	entry, inFstab := fstabEntries[mountpoint]
	isConfiguredNFS := inFstab && isNFSFSType(entry.fstype)
	managedEntries, err := nfsMountStore.load()
	if err != nil {
		slog.Warn("failed to read managed NFS mount registry", "error", err)
		managedEntries = make(map[string]managedMountEntry)
	}
	managedEntry, inManagedRegistry := managedEntries[mountpoint]

	partitions, err := disk.Partitions(true)
	if err != nil {
		slog.Error("failed to get NFS mount info", "error", err)
		return nil, fmt.Errorf("failed to get mount info: %w", err)
	}

	var currentMount *disk.PartitionStat
	for _, p := range partitions {
		if p.Mountpoint == mountpoint && isNFSFSType(p.Fstype) {
			currentMount = &p
			break
		}
	}

	result := map[string]any{"success": true}

	if currentMount == nil {
		return unmountInactiveNFS(mountpoint, removeFstab, isConfiguredNFS, inManagedRegistry, result)
	}

	source, fstype, options := resolveNFSMountInfo(currentMount, entry, isConfiguredNFS, managedEntry, inManagedRegistry)
	slog.Info("unmounting NFS share", "mountpoint", mountpoint)
	out, err := runMountCombinedOutput(ctx, nfsUnmountCommandTimeout, "umount", mountpoint)
	if err != nil {
		message := commandFailureMessage(out, err)
		slog.Error("NFS unmount failed", "mountpoint", mountpoint, "message", message)
		return nil, fmt.Errorf("umount failed: %s", message)
	}
	slog.Info("NFS share unmounted", "mountpoint", mountpoint)
	persistUnmountConfig(mountpoint, source, fstype, options, removeFstab, isConfiguredNFS, result)
	return result, nil
}

func unmountInactiveNFS(mountpoint string, removeFstab, isConfiguredNFS, inManagedRegistry bool, result map[string]any) (map[string]any, error) {
	if removeFstab && isConfiguredNFS {
		if err := removeFromFstab(mountpoint); err != nil {
			slog.Warn("failed to remove mount from fstab", "mountpoint", mountpoint, "error", err)
			return nil, fmt.Errorf("failed to remove stored mount: %w", err)
		}
		if err := nfsMountStore.remove(mountpoint); err != nil {
			slog.Warn("failed to remove LinuxIO metadata", "mountpoint", mountpoint, "error", err)
		}
		slog.Info("removed inactive NFS entry from fstab", "mountpoint", mountpoint)
		result["warning"] = "mount was not active; removed saved configuration only"
		return result, nil
	}
	if removeFstab && inManagedRegistry {
		if err := nfsMountStore.remove(mountpoint); err != nil {
			slog.Warn("failed to remove LinuxIO metadata", "mountpoint", mountpoint, "error", err)
			return nil, fmt.Errorf("failed to remove saved mount: %w", err)
		}
		slog.Info("removed inactive temporary NFS entry from LinuxIO registry", "mountpoint", mountpoint)
		result["warning"] = "mount was not active; removed saved configuration only"
		return result, nil
	}
	slog.Warn("NFS mount not found", "mountpoint", mountpoint)
	return nil, fmt.Errorf("NFS mount not found at %s", mountpoint)
}

func resolveNFSMountInfo(
	mount *disk.PartitionStat, entry fstabEntry, isConfiguredNFS bool,
	managed managedMountEntry, inManagedRegistry bool,
) (source, fstype string, options []string) {
	source = mount.Device
	fstype = mount.Fstype
	options = parseOptions(mount.Opts)
	if (!strings.Contains(source, ":") || source == "none") && isConfiguredNFS {
		source = entry.source
	}
	if source == "" && inManagedRegistry {
		source = managed.Source
	}
	if !isNFSFSType(fstype) && isConfiguredNFS {
		fstype = entry.fstype
	}
	if !isNFSFSType(fstype) && inManagedRegistry {
		fstype = managed.FSType
	}
	if len(options) == 0 && inManagedRegistry {
		options = append([]string(nil), managed.Options...)
	}
	if !isNFSFSType(fstype) {
		fstype = "nfs"
	}
	return source, fstype, options
}

func persistUnmountConfig(mountpoint, source, fstype string, options []string, removeFstab, isConfiguredNFS bool, result map[string]any) {
	if removeFstab {
		if isConfiguredNFS {
			if err := removeFromFstab(mountpoint); err != nil {
				slog.Warn("unmount succeeded but fstab update failed", "mountpoint", mountpoint, "error", err)
				result["warning"] = fmt.Sprintf("unmount succeeded but fstab update failed: %v", err)
			} else {
				slog.Info("removed mount from fstab", "mountpoint", mountpoint)
			}
		}
		if err := nfsMountStore.remove(mountpoint); err != nil {
			slog.Warn("failed to remove LinuxIO metadata", "mountpoint", mountpoint, "error", err)
			if result["warning"] == nil {
				result["warning"] = fmt.Sprintf("unmount succeeded but LinuxIO registry update failed: %v", err)
			}
		}
		return
	}

	if isConfiguredNFS {
		if err := nfsMountStore.remove(mountpoint); err != nil {
			slog.Warn("failed to clean up temporary NFS mount metadata", "mountpoint", mountpoint, "error", err)
		}
		return
	}

	if err := nfsMountStore.upsert(managedMountEntry{Source: source, Mountpoint: mountpoint, FSType: fstype, Options: options}); err != nil {
		slog.Warn("unmount succeeded but LinuxIO registry update failed", "mountpoint", mountpoint, "error", err)
		result["warning"] = fmt.Sprintf("unmount succeeded but LinuxIO registry update failed: %v", err)
	} else {
		slog.Info("saved temporary NFS entry to LinuxIO registry", "mountpoint", mountpoint)
	}
}
