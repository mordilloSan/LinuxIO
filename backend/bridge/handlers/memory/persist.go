package memory

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/atomicfile"
)

// The files the memory settings write, as absolute host paths. Each is
// LinuxIO's alone and says so in its first line.
const (
	sysctlDropInPath   = "/etc/sysctl.d/90-linuxio-memory.conf"
	zswapTmpfilesPath  = "/etc/tmpfiles.d/linuxio-zswap.conf"
	zramswapConfigPath = "/etc/default/zramswap"
	// zramGeneratorConfigPath is zram-generator's main configuration file.
	// The one in /etc takes the place of the one a package puts in
	// /usr/lib/systemd, so a file here with no device turns zram off.
	zramGeneratorConfigPath = "/etc/systemd/zram-generator.conf"

	managedFileMode fs.FileMode = 0o644
	managedHeader               = "# Managed by LinuxIO (Dashboard, Memory); changes here are overwritten.\n"

	// zramConfigCopySuffix names the copy kept beside a zram configuration
	// file that is not LinuxIO's before a change replaces it. It does not
	// end in .conf, so neither zram tool reads it.
	zramConfigCopySuffix = ".before-linuxio"

	// unitConfigDir is where an admin's unit files and drop-ins go; only
	// root can write it.
	unitConfigDir = "/etc/systemd/system"
	// holdOffDropInName is the drop-in that holds the managed swap file's
	// unit off at boot (see holdOffDropInPath).
	holdOffDropInName = "90-linuxio-memory.conf"
)

// holdOffDropInPath is LinuxIO's drop-in for unit, a unit name
// systemd.EscapePath made from an fstab entry. With it the unit's condition
// never holds, so systemd skips the unit at boot and for a start, and a
// unit that requires it, such as swap.target, still starts, as it would not
// for a masked unit.
func holdOffDropInPath(unit string) string {
	return unitConfigDir + "/" + unit + ".d/" + holdOffDropInName
}

// makeHoldOffDropInDir makes the folder of unit's hold-off drop-in mode 0755
// whatever the umask: the job host runs with umask 077, and the bridge of a
// session without sudo, whose status reads whether the drop-in is there,
// must be able to look into it. A folder an earlier run left another mode
// holding nothing but LinuxIO's drop-in is made 0755 too; one that holds
// files of an admin's keeps its mode.
func (h hostRoot) makeHoldOffDropInDir(unit string) error {
	dir := h.path(filepath.Dir(holdOffDropInPath(unit)))
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return fmt.Errorf("make %s: %w", filepath.Dir(dir), err)
	}
	err := os.Mkdir(dir, 0o755)
	if errors.Is(err, fs.ErrExist) {
		info, statErr := os.Lstat(dir)
		if statErr != nil {
			return statErr
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a folder", dir)
		}
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			return readErr
		}
		for _, entry := range entries {
			if entry.Name() != holdOffDropInName {
				return nil
			}
		}
	} else if err != nil {
		return fmt.Errorf("make %s: %w", dir, err)
	}
	return os.Chmod(dir, 0o755)
}

// isHoldOffDropIn reports whether name is a hold-off drop-in's path.
func isHoldOffDropIn(name string) bool {
	unitDir, file := filepath.Split(name)
	unitDir = filepath.Clean(unitDir)
	return file == holdOffDropInName && filepath.Dir(unitDir) == unitConfigDir && strings.HasSuffix(unitDir, ".d")
}

// renderHoldOffDropIn is the hold-off drop-in: "/" always exists, so the
// negated condition never holds.
func renderHoldOffDropIn() []byte {
	return []byte(managedHeader +
		"# Keeps this swap off at boot. LinuxIO removes this file to turn it on again.\n" +
		"[Unit]\nConditionPathExists=!/\n")
}

// zramConfigPath is the configuration file the zram provider reads and a
// change writes, or empty without a provider.
func zramConfigPath(provider apischema.ZramProvider) string {
	switch provider {
	case apischema.ZramProviderTools:
		return zramswapConfigPath
	case apischema.ZramProviderGenerator:
		return zramGeneratorConfigPath
	default:
		return ""
	}
}

// notLinuxIOs reports whether the text of the zram configuration file at path
// is not LinuxIO's to replace without asking: it does not start with
// LinuxIO's header and holds a line that is neither blank nor a comment, as
// an admin's settings do. A package's default of comments only is LinuxIO's
// to replace. A byte order mark and CRLF line ends are read past, and in
// zram-generator's INI file a line starting with ';' is a comment too.
func notLinuxIOs(path, text string) bool {
	text = strings.TrimPrefix(text, "\ufeff")
	header := strings.TrimSuffix(managedHeader, "\n")
	comment := "#"
	if path == zramGeneratorConfigPath {
		comment = "#;"
	}
	first := true
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if first && line == header {
			return false
		}
		first = false
		if line != "" && !strings.ContainsRune(comment, rune(line[0])) {
			return true
		}
	}
	return false
}

// asksToReplace reports whether the zram configuration file at path, holding
// data, needs the user's agreement before a change replaces it: it is not
// LinuxIO's, and it is not the file as its package shipped it, which nobody
// edited (unchangedConffile).
func (h hostRoot) asksToReplace(path string, data []byte) bool {
	return notLinuxIOs(path, string(data)) && !h.unchangedConffile(path, data)
}

// foreignZramConfig is the provider's configuration file when it is not
// LinuxIO's, with the path its copy is kept at, or nil. A file that cannot
// be read, as by a session user one only root may read, may hold anything
// and counts as not LinuxIO's: the dialog then asks, and the change, which
// reads it as root, copies it only if it is not.
func (h hostRoot) foreignZramConfig(provider apischema.ZramProvider) *apischema.ForeignZramConfig {
	path := zramConfigPath(provider)
	if path == "" {
		return nil
	}
	text, ok, err := h.readOptional(path)
	if err != nil {
		slog.Debug("read zram configuration failed", "path", path, "error", err)
	} else if !ok || !h.asksToReplace(path, []byte(text)) {
		return nil
	}
	return &apischema.ForeignZramConfig{Path: path, CopyPath: path + zramConfigCopySuffix}
}

// zramService is zram-tools' unit: it sets up the zram device from
// /etc/default/zramswap and swaps on it. zramDeviceName is the device LinuxIO's
// zram runs on with either tool: zram-tools' script names it itself, and
// LinuxIO's zram-generator configuration defines it alone.
const (
	zramService    = "zramswap.service"
	zramDeviceName = "zram0"
)

// The units zram-generator makes at a systemd reload for the zram0 device
// LinuxIO configures. The swap unit requires the setup service, which
// creates the device, and the setup service is bound to the swap unit:
// starting the swap unit sets the device up and swaps on it, stopping it
// runs swapoff and resets the device.
const (
	zramGeneratorDevice    = "/dev/zram0"
	zramGeneratorSwapUnit  = "dev-zram0.swap"
	zramGeneratorSetupUnit = "systemd-zram-setup@zram0.service"
)

// writeFile replaces the file name under the root atomically; it refuses to
// write through a symlink.
func (h hostRoot) writeFile(name string, data []byte, mode fs.FileMode) error {
	return atomicfile.Write(h.path(name), data, mode)
}

// removeFile removes the file name under the root; a missing file is fine.
func (h hostRoot) removeFile(name string) error {
	if err := os.Remove(h.path(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// sysctlEntry is one "key = value" line of a sysctl.d file.
type sysctlEntry struct {
	key, value string
}

// sysctlKey is the sysctl.d key of a vm sysctl file name.
func sysctlKey(name string) string {
	return "vm." + name
}

// heldSysctls is the vm sysctls, by file name, that drop-in entries set.
func heldSysctls(entries []sysctlEntry) map[string]bool {
	held := map[string]bool{}
	for _, entry := range entries {
		if name, ok := strings.CutPrefix(entry.key, "vm."); ok {
			held[name] = true
		}
	}
	return held
}

// renderSysctlDropIn is the drop-in holding entries, in order.
func renderSysctlDropIn(entries []sysctlEntry) []byte {
	var b strings.Builder
	b.WriteString(managedHeader)
	for _, entry := range entries {
		fmt.Fprintf(&b, "%s = %s\n", entry.key, entry.value)
	}
	return []byte(b.String())
}

// parseSysctlDropIn reads the "key = value" lines of a sysctl.d file,
// skipping comments, blank lines and lines without "=".
func parseSysctlDropIn(text string) []sysctlEntry {
	var entries []sysctlEntry
	for line := range strings.Lines(text) {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		entries = append(entries, sysctlEntry{key: strings.TrimSpace(key), value: strings.TrimSpace(value)})
	}
	return entries
}

// readSysctlDropIn reads LinuxIO's drop-in; a missing one has no entries.
func (h hostRoot) readSysctlDropIn() ([]sysctlEntry, error) {
	text, _, err := h.readOptional(sysctlDropInPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", sysctlDropInPath, err)
	}
	return parseSysctlDropIn(text), nil
}

// mergeSysctlDropIn sets each setting in entries, in place when its key is
// there and at the end otherwise, and drops the keys of the remove names.
// Every other entry stays where it is.
func mergeSysctlDropIn(entries []sysctlEntry, set []sysctlSetting, remove []string) []sysctlEntry {
	entries = slices.DeleteFunc(entries, func(e sysctlEntry) bool {
		return slices.ContainsFunc(remove, func(name string) bool { return e.key == sysctlKey(name) })
	})
	for _, setting := range set {
		entry := sysctlEntry{key: sysctlKey(setting.name), value: strconv.Itoa(setting.value)}
		if i := slices.IndexFunc(entries, func(e sysctlEntry) bool { return e.key == entry.key }); i >= 0 {
			entries[i] = entry
		} else {
			entries = append(entries, entry)
		}
	}
	return entries
}

// renderZswapTmpfiles is the tmpfiles.d file that sets zswap's compressor
// and turns zswap on at boot, so the kernel command line is never touched.
func renderZswapTmpfiles(compressor string) []byte {
	return fmt.Appendf(nil, "%sw %s - - - - %s\nw %s - - - - %s\n", managedHeader,
		zswapCompressorParam, compressor, zswapEnabledParam, zswapSwitch(true))
}

// renderZswapOffTmpfiles is the tmpfiles.d file that turns zswap off at boot
// on a kernel that starts with it on.
func renderZswapOffTmpfiles() []byte {
	return fmt.Appendf(nil, "%sw %s - - - - %s\n", managedHeader, zswapEnabledParam, zswapSwitch(false))
}

// zswapSwitch is how the zswap enabled parameter spells a state.
func zswapSwitch(enabled bool) string {
	if enabled {
		return "Y"
	}
	return "N"
}

// renderZramswapConfig is zram-tools' /etc/default/zramswap for a zram
// device of size bytes, rounded down to whole MiB.
func renderZramswapConfig(size uint64) []byte {
	return fmt.Appendf(nil, "%sALGO=zstd\nSIZE=%d\nPRIORITY=100\n", managedHeader, size>>20)
}

// renderZramGeneratorConfig is zram-generator's configuration for one zram
// swap device of size bytes, rounded down to whole MiB, with the same
// algorithm and priority as renderZramswapConfig.
func renderZramGeneratorConfig(size uint64) []byte {
	return fmt.Appendf(nil, "%s[zram0]\nzram-size = %d\ncompression-algorithm = zstd\nswap-priority = 100\n", managedHeader, size>>20)
}

// renderZramGeneratorOff is zram-generator's configuration with no device,
// which makes the generator create no unit.
func renderZramGeneratorOff() []byte {
	return []byte(managedHeader + "# No device: zram is off. This file takes the place of /usr/lib/systemd/zram-generator.conf.\n")
}
