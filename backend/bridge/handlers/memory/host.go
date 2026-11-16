package memory

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The host files the package reads, as absolute host paths.
const (
	procSwaps     = "/proc/swaps"
	procMeminfo   = "/proc/meminfo"
	procMountinfo = "/proc/self/mountinfo"
	vmSysctlDir   = "/proc/sys/vm"
	zswapParamDir = "/sys/module/zswap/parameters"
	sysBlockDir   = "/sys/block"
	etcFstab      = "/etc/fstab"

	// sysClassBlockDir holds one link per block device, partitions included.
	sysClassBlockDir = "/sys/class/block"
)

// The zswap parameters a mode writes, as absolute host paths.
var (
	zswapEnabledParam    = filepath.Join(zswapParamDir, "enabled")
	zswapCompressorParam = filepath.Join(zswapParamDir, "compressor")
)

// The vm sysctls the status reports and the mode presets set, as file names
// under vmSysctlDir.
const (
	sysctlSwappiness           = "swappiness"
	sysctlPageCluster          = "page-cluster"
	sysctlWatermarkBoostFactor = "watermark_boost_factor"
	sysctlWatermarkScaleFactor = "watermark_scale_factor"
)

// hostRoot is the directory the host's /proc, /sys and /etc are found under:
// "/" on the host, a fixture tree in tests.
type hostRoot struct {
	root string
}

var host = hostRoot{root: "/"}

// path returns the absolute host path name under the root.
func (h hostRoot) path(name string) string {
	return filepath.Join(h.root, name)
}

// exists reports whether the host path name exists under the root.
func (h hostRoot) exists(name string) bool {
	_, err := os.Stat(h.path(name))
	return err == nil
}

// readOptional reads name; ok is false when the file does not exist.
func (h hostRoot) readOptional(name string) (text string, ok bool, err error) {
	data, err := os.ReadFile(h.path(name))
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

// readOptionalUint reads a file holding one unsigned number, or 0 when the
// file does not exist.
func (h hostRoot) readOptionalUint(name string) (uint64, error) {
	text, ok, err := h.readOptional(name)
	if err != nil || !ok {
		return 0, err
	}
	n, err := strconv.ParseUint(strings.TrimSpace(text), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return n, nil
}

// readOptionalInt reads a file holding one number, or 0 when the file does
// not exist.
func (h hostRoot) readOptionalInt(name string) (int, error) {
	text, ok, err := h.readOptional(name)
	if err != nil || !ok {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return n, nil
}

// unescapeOctal decodes the \ooo escapes /proc/swaps, mountinfo and fstab
// write for a space, tab, newline or backslash in a path.
func unescapeOctal(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(n))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
