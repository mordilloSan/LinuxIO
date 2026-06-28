package storage

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

const (
	cifsMountCommandTimeout   = 30 * time.Second
	cifsUnmountCommandTimeout = 15 * time.Second
	cifsBrowseCommandTimeout  = 10 * time.Second
)

// cifsCredentialsDir holds the root-only (0600) credentials files referenced by
// fstab via credentials=. A var so tests can redirect it. This mirrors how
// OpenMediaVault stores SMB credentials for fstab-mounted shares.
var cifsCredentialsDir = "/var/lib/linuxio/cifs-credentials"

// validShareName allows SMB share names (letters, digits, and . _ $ -). Spaces
// are disallowed: fstab is whitespace-delimited and getFstabEntries splits on
// whitespace, so a space in the source would corrupt the entry.
var validShareName = regexp.MustCompile(`^[a-zA-Z0-9._$-]+$`)

// cifsClientAvailable is the availability gate; overridable in tests. The
// capability registry calls CheckCIFSClientAvailability directly.
var cifsClientAvailable = CheckCIFSClientAvailability

// cifsMountRunner runs the mount/umount command; overridable in tests.
var cifsMountRunner = runMountCombinedOutput

// cifsMountParams is the resolved input for creating a CIFS mount. All CIFS
// mounts are persistent (fstab-backed), like OpenMediaVault remote mounts.
type cifsMountParams struct {
	server, share, mountpoint  string
	username, password, domain string
	options                    []string
}

// CheckCIFSClientAvailability reports whether the SMB client mount helper
// (mount.cifs, from cifs-utils) is installed.
func CheckCIFSClientAvailability() (bool, error) {
	if _, err := findMountCommand("mount.cifs"); err != nil {
		return false, fmt.Errorf("mount.cifs not found (install cifs-utils)")
	}
	return true, nil
}

func requireCIFSClientAvailability() error {
	ok, err := cifsClientAvailable()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("SMB client utilities are unavailable")
	}
	return nil
}

func isCIFSFSType(fstype string) bool {
	return fstype == "cifs" || fstype == "smb3"
}

// parseCIFSSource splits a UNC source "//server/share" into server and share.
func parseCIFSSource(source string) (server, share string) {
	s := strings.TrimPrefix(strings.ReplaceAll(source, `\`, "/"), "//")
	idx := strings.Index(s, "/")
	if idx <= 0 {
		return "", ""
	}
	return s[:idx], s[idx+1:]
}

// ListCIFSShares browses a server for share names as guest (best-effort).
func ListCIFSShares(ctx context.Context, server string) ([]string, error) {
	if !validHostname.MatchString(server) {
		return nil, fmt.Errorf("invalid SMB server hostname")
	}
	smbclient, err := findMountCommand("smbclient")
	if err != nil {
		return nil, fmt.Errorf("smbclient not found (install smbclient or samba-client)")
	}
	out, err := runMountOutput(ctx, cifsBrowseCommandTimeout, smbclient, "-L", "//"+server, "-N", "-g")
	if err != nil {
		return nil, fmt.Errorf("failed to list SMB shares: %w", err)
	}

	var shares []string
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// smbclient -g output: "Disk|<name>|<comment>"
		rest, ok := strings.CutPrefix(line, "Disk|")
		if !ok {
			continue
		}
		name, _, _ := strings.Cut(rest, "|")
		name = strings.TrimSpace(name)
		if name != "" && !strings.HasSuffix(name, "$") { // skip hidden/admin shares
			shares = append(shares, name)
		}
	}
	return shares, scanner.Err()
}

// ListCIFSMounts returns all CIFS mounts: active ones plus persistent /etc/fstab
// entries that are currently inactive. Passwords are never surfaced.
func ListCIFSMounts(ctx context.Context) ([]apischema.CIFSMount, error) {
	partitions, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	fstabEntries := getFstabEntries()

	byMountpoint := make(map[string]apischema.CIFSMount)
	for _, p := range partitions {
		if mount, ok := cifsMountFromPartition(p, fstabEntries); ok {
			byMountpoint[p.Mountpoint] = mount
		}
	}

	addCIFSFstabMounts(byMountpoint, fstabEntries)
	return sortedCIFSMounts(byMountpoint), nil
}

func cifsMountFromPartition(p disk.PartitionStat, fstabEntries map[string]fstabEntry) (apischema.CIFSMount, bool) {
	if !isCIFSFSType(p.Fstype) {
		return apischema.CIFSMount{}, false
	}
	source := p.Device
	fe, has := fstabEntries[p.Mountpoint]
	inFstab := has && isCIFSFSType(fe.fstype)
	if (source == "" || source == "none") && inFstab {
		source = fe.source
	}
	authOptions := strings.Join(p.Opts, ",")
	if inFstab {
		authOptions = fe.options
	}
	username, domain := cifsDisplayAuth(authOptions)
	server, share := parseCIFSSource(source)
	mount := apischema.CIFSMount{
		Source:     source,
		Server:     server,
		Share:      share,
		Mountpoint: p.Mountpoint,
		FSType:     p.Fstype,
		Options:    displayCIFSOptions(parseOptions(p.Opts)),
		InFstab:    inFstab,
		Mounted:    true,
		Username:   username,
		Domain:     domain,
	}
	if usage, err := disk.Usage(p.Mountpoint); err == nil {
		mount.Size = usage.Total
		mount.Used = usage.Used
		mount.Free = usage.Free
		mount.UsedPct = usage.UsedPercent
	}
	return mount, true
}

func addCIFSFstabMounts(mounts map[string]apischema.CIFSMount, fstabEntries map[string]fstabEntry) {
	for mountpoint, fe := range fstabEntries {
		if !isCIFSFSType(fe.fstype) {
			continue
		}
		if existing, ok := mounts[mountpoint]; ok {
			existing.InFstab = true
			mounts[mountpoint] = existing
			continue
		}
		username, domain := cifsDisplayAuth(fe.options)
		server, share := parseCIFSSource(fe.source)
		mounts[mountpoint] = apischema.CIFSMount{
			Source:     fe.source,
			Server:     server,
			Share:      share,
			Mountpoint: mountpoint,
			FSType:     fe.fstype,
			Options:    displayCIFSOptions(parseOptionsString(fe.options)),
			InFstab:    true,
			Mounted:    false,
			Username:   username,
			Domain:     domain,
		}
	}
}

func sortedCIFSMounts(byMountpoint map[string]apischema.CIFSMount) []apischema.CIFSMount {
	mounts := make([]apischema.CIFSMount, 0, len(byMountpoint))
	for _, mount := range byMountpoint {
		mounts = append(mounts, mount)
	}
	sort.Slice(mounts, func(i, j int) bool {
		if mounts[i].Mounted != mounts[j].Mounted {
			return mounts[i].Mounted
		}
		return mounts[i].Mountpoint < mounts[j].Mountpoint
	})
	return mounts
}

// cifsDisplayAuth derives the display username/domain from a mount options
// string, reading the referenced credentials file when present. It never
// returns the password.
func cifsDisplayAuth(optionsCSV string) (username, domain string) {
	for o := range strings.SplitSeq(optionsCSV, ",") {
		key, val, _ := strings.Cut(o, "=")
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "user", "username":
			if username == "" {
				username = strings.TrimSpace(val)
			}
		case "domain", "dom":
			if domain == "" {
				domain = strings.TrimSpace(val)
			}
		case "credentials", "cred":
			u, d := readCredentialsMeta(strings.TrimSpace(val))
			if username == "" {
				username = u
			}
			if domain == "" {
				domain = d
			}
		}
	}
	return username, domain
}

// readCredentialsMeta reads username/domain (never the password) from a CIFS
// credentials file for display purposes.
func readCredentialsMeta(path string) (username, domain string) {
	f, err := os.Open(path)
	if err != nil {
		return "", ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, val, ok := strings.Cut(scanner.Text(), "=")
		if !ok {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "username":
			username = strings.TrimSpace(val)
		case "domain":
			domain = strings.TrimSpace(val)
		}
	}
	return username, domain
}

// displayCIFSOptions drops any sensitive keys so mount options are safe to show.
func displayCIFSOptions(opts []string) []string {
	out := make([]string, 0, len(opts))
	for _, o := range opts {
		key, _, _ := strings.Cut(o, "=")
		if sensitiveOptionKeys[strings.ToLower(strings.TrimSpace(key))] {
			continue
		}
		out = append(out, o)
	}
	return out
}

// MountCIFS mounts an SMB share and persists it to /etc/fstab (OpenMediaVault
// style). Authenticated mounts get a root-only 0600 credentials file referenced
// via credentials=; guest mounts use -o guest. If the mountpoint is already in
// fstab, it is simply (re)mounted using the existing entry — no credentials are
// needed, so this also covers re-activating an inactive row.
func MountCIFS(ctx context.Context, p cifsMountParams) (map[string]any, error) {
	if err := validateCIFSMountRequest(p.server, p.share, p.mountpoint); err != nil {
		return nil, err
	}
	// Preflight before any mkdir so an uninstalled client never leaves an orphan dir.
	if err := requireCIFSClientAvailability(); err != nil {
		return nil, err
	}

	source := fmt.Sprintf("//%s/%s", p.server, p.share)

	// If the mountpoint is already configured, only re-activate it when the
	// existing entry is the *same* CIFS source. Never silently mount a different
	// or foreign (non-CIFS) entry — addToFstab would no-op and we'd mount whatever
	// is already there.
	if fe, ok := getFstabEntries()[p.mountpoint]; ok {
		if !isCIFSFSType(fe.fstype) || fe.source != source {
			return nil, fmt.Errorf("mountpoint %s is already in use by a different mount", p.mountpoint)
		}
		if isCIFSMounted(p.mountpoint) {
			return map[string]any{"success": true, "mountpoint": p.mountpoint}, nil
		}
		if err := os.MkdirAll(p.mountpoint, 0755); err != nil {
			return nil, fmt.Errorf("failed to create mountpoint: %w", err)
		}
		if out, err := cifsMountRunner(ctx, cifsMountCommandTimeout, "mount", p.mountpoint); err != nil {
			return nil, fmt.Errorf("mount failed: %s", commandFailureMessage(out, err))
		}
		slog.Info("CIFS share mounted", "source", source, "mountpoint", p.mountpoint)
		return map[string]any{"success": true, "mountpoint": p.mountpoint}, nil
	}

	options := cleanMountOptions(p.options)
	if err := rejectSensitiveCustomOptions(options); err != nil {
		return nil, err
	}

	if err := os.MkdirAll(p.mountpoint, 0755); err != nil {
		return nil, fmt.Errorf("failed to create mountpoint: %w", err)
	}

	fstabOpts := []string{"_netdev", "nofail"}
	fstabOpts = append(fstabOpts, options...)
	if p.username == "" {
		fstabOpts = append([]string{"guest"}, fstabOpts...)
	} else {
		credPath, err := writeCredentialsFile(p.mountpoint, p.username, p.password, p.domain)
		if err != nil {
			return nil, err
		}
		fstabOpts = append([]string{"credentials=" + credPath}, fstabOpts...)
	}

	if err := addToFstab(source, p.mountpoint, "cifs", fstabOpts); err != nil {
		_ = deleteCredentialsFile(p.mountpoint)
		return nil, fmt.Errorf("failed to update fstab: %w", err)
	}

	// Password safety: log a fixed summary only — never the options/credentials.
	slog.Info("mounting CIFS share", "source", source, "mountpoint", p.mountpoint)
	out, err := cifsMountRunner(ctx, cifsMountCommandTimeout, "mount", p.mountpoint)
	if err != nil {
		// Roll back so we never leave a broken boot entry behind.
		_ = removeFromFstab(p.mountpoint)
		_ = deleteCredentialsFile(p.mountpoint)
		message := commandFailureMessage(out, err)
		slog.Error("CIFS mount failed", "source", source, "mountpoint", p.mountpoint, "message", message)
		return nil, fmt.Errorf("mount failed: %s", message)
	}
	slog.Info("CIFS share mounted", "source", source, "mountpoint", p.mountpoint)
	return map[string]any{"success": true, "mountpoint": p.mountpoint}, nil
}

func validateCIFSMountRequest(server, share, mountpoint string) error {
	if !validHostname.MatchString(server) {
		return fmt.Errorf("invalid SMB server hostname")
	}
	if share == "" || !validShareName.MatchString(share) {
		return fmt.Errorf("invalid share name")
	}
	if !validPath.MatchString(mountpoint) {
		return fmt.Errorf("invalid mountpoint")
	}
	if isSystemPath(mountpoint) {
		return fmt.Errorf("cannot mount to system path: %s", mountpoint)
	}
	return nil
}

// RemountCIFS unmounts and remounts an fstab-configured share with new options,
// preserving the existing auth (guest or credentials=<file>). No password is
// collected. Signature matches RemountNFS.
func RemountCIFS(ctx context.Context, mountpoint string, newOptions []string, updateFstab bool) (map[string]any, error) {
	if !validPath.MatchString(mountpoint) {
		return nil, fmt.Errorf("invalid mountpoint")
	}
	options := cleanMountOptions(newOptions)
	if err := rejectSensitiveCustomOptions(options); err != nil {
		return nil, err
	}
	if err := requireCIFSClientAvailability(); err != nil {
		return nil, err
	}

	fe, ok := getFstabEntries()[mountpoint]
	if !ok || !isCIFSFSType(fe.fstype) {
		return nil, fmt.Errorf("CIFS mount not found at %s", mountpoint)
	}

	fstabOpts := cifsRemountOptions(fe.options, options)
	wasMounted, err := unmountCIFSIfMounted(ctx, mountpoint)
	if err != nil {
		return nil, err
	}
	if updateFstab {
		if err := updateFstabEntry(mountpoint, fe.source, "cifs", fstabOpts); err != nil {
			return nil, fmt.Errorf("failed to update fstab: %w", err)
		}
	}
	if out, err := cifsMountRunner(ctx, cifsMountCommandTimeout, "mount", mountpoint); err != nil {
		return nil, rollbackFailedCIFSRemount(ctx, mountpoint, fe, updateFstab, wasMounted, out, err)
	}
	slog.Info("CIFS share remounted", "mountpoint", mountpoint)
	return map[string]any{"success": true, "mountpoint": mountpoint}, nil
}

func cifsRemountOptions(existingOptions string, options []string) []string {
	fstabOpts := []string{}
	if auth := preserveCIFSAuthOption(existingOptions); auth != "" {
		fstabOpts = append(fstabOpts, auth)
	}
	fstabOpts = append(fstabOpts, "_netdev", "nofail")
	return append(fstabOpts, options...)
}

func unmountCIFSIfMounted(ctx context.Context, mountpoint string) (bool, error) {
	if !isCIFSMounted(mountpoint) {
		return false, nil
	}
	out, err := cifsMountRunner(ctx, cifsUnmountCommandTimeout, "umount", mountpoint)
	if err != nil {
		return true, fmt.Errorf("unmount failed: %s", commandFailureMessage(out, err))
	}
	return true, nil
}

func rollbackFailedCIFSRemount(
	ctx context.Context,
	mountpoint string,
	previous fstabEntry,
	updateFstab bool,
	wasMounted bool,
	out []byte,
	err error,
) error {
	message := commandFailureMessage(out, err)
	if updateFstab {
		if rbErr := updateFstabEntry(mountpoint, previous.source, "cifs", parseOptionsString(previous.options)); rbErr != nil {
			slog.Warn("failed to roll back fstab options after remount failure", "mountpoint", mountpoint, "error", rbErr)
		}
	}
	if wasMounted {
		if _, rmErr := cifsMountRunner(ctx, cifsMountCommandTimeout, "mount", mountpoint); rmErr != nil {
			slog.Warn("failed to restore previous CIFS mount after remount failure", "mountpoint", mountpoint, "error", rmErr)
		}
	}
	slog.Error("CIFS remount failed", "mountpoint", mountpoint, "message", message)
	return fmt.Errorf("remount failed: %s", message)
}

// preserveCIFSAuthOption returns the auth-bearing option (guest or
// credentials=<file>) from an existing fstab options string, if any.
func preserveCIFSAuthOption(optionsCSV string) string {
	for o := range strings.SplitSeq(optionsCSV, ",") {
		key, _, _ := strings.Cut(o, "=")
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "guest", "credentials", "cred":
			return strings.TrimSpace(o)
		}
	}
	return ""
}

// UnmountCIFS unmounts a share. When removeFstab is set it also removes the
// fstab line and deletes the root-only credentials file.
func UnmountCIFS(ctx context.Context, mountpoint string, removeFstab bool) (map[string]any, error) {
	if !validPath.MatchString(mountpoint) {
		return nil, fmt.Errorf("invalid mountpoint")
	}

	result := map[string]any{"success": true}
	if isCIFSMounted(mountpoint) {
		if out, err := cifsMountRunner(ctx, cifsUnmountCommandTimeout, "umount", mountpoint); err != nil {
			return nil, fmt.Errorf("umount failed: %s", commandFailureMessage(out, err))
		}
		slog.Info("CIFS share unmounted", "mountpoint", mountpoint)
	}

	if removeFstab {
		if fe, ok := getFstabEntries()[mountpoint]; ok && isCIFSFSType(fe.fstype) {
			if err := removeFromFstab(mountpoint); err != nil {
				slog.Warn("unmount succeeded but fstab update failed", "mountpoint", mountpoint, "error", err)
				result["warning"] = fmt.Sprintf("unmount succeeded but fstab update failed: %v", err)
			}
		}
		if err := deleteCredentialsFile(mountpoint); err != nil {
			slog.Warn("failed to delete CIFS credentials file", "mountpoint", mountpoint, "error", err)
		}
	}
	return result, nil
}

// writeCredentialsFile writes a root-only (0600) credentials file in a 0700
// directory and returns its path.
func writeCredentialsFile(mountpoint, username, password, domain string) (string, error) {
	if err := os.MkdirAll(cifsCredentialsDir, 0o700); err != nil {
		return "", fmt.Errorf("create credentials dir: %w", err)
	}
	// MkdirAll does not tighten an already-existing directory; enforce 0700.
	if err := os.Chmod(cifsCredentialsDir, 0o700); err != nil {
		return "", fmt.Errorf("secure credentials dir: %w", err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "username=%s\n", username)
	fmt.Fprintf(&b, "password=%s\n", password)
	if domain != "" {
		fmt.Fprintf(&b, "domain=%s\n", domain)
	}
	path := credentialsFilePath(mountpoint)
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return "", fmt.Errorf("write credentials: %w", err)
	}
	_ = os.Chmod(path, 0o600)
	return path, nil
}

func credentialsFilePath(mountpoint string) string {
	sum := sha256.Sum256([]byte(mountpoint))
	return filepath.Join(cifsCredentialsDir, hex.EncodeToString(sum[:8])+".cred")
}

func deleteCredentialsFile(mountpoint string) error {
	if err := os.Remove(credentialsFilePath(mountpoint)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func isCIFSMounted(mountpoint string) bool {
	partitions, err := disk.Partitions(true)
	if err != nil {
		return false
	}
	for _, p := range partitions {
		if p.Mountpoint == mountpoint && isCIFSFSType(p.Fstype) {
			return true
		}
	}
	return false
}
