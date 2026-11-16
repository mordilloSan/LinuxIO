package memory

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strconv"
	"strings"

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
)

// zramService is zram-tools' unit: it sets up the zram device from
// /etc/default/zramswap and swaps on it.
const zramService = "zramswap.service"

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
