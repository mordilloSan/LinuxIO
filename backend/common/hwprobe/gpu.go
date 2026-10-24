package hwprobe

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jaypipes/ghw/pkg/gpu"
	"github.com/jaypipes/ghw/pkg/pci"
)

const drmBase = "/sys/class/drm"

// BuildGPUEntry maps a ghw graphics card to the JSON-shaped field map both
// processes decode into their own wire types.
func BuildGPUEntry(card *gpu.GraphicsCard) map[string]any {
	entry := map[string]any{"address": NormalizePCIAddress(card.Address)}
	if card.DeviceInfo != nil {
		populateDeviceInfo(entry, card.DeviceInfo)
	}
	if card.Node != nil {
		entry["numa_node"] = card.Node.ID
	}
	return entry
}

func populateDeviceInfo(entry map[string]any, di *pci.Device) {
	SetIfNonEmpty(entry, "revision", di.Revision)
	SetIfNonEmpty(entry, "driver", di.Driver)
	if di.Vendor != nil {
		SetIfNonEmpty(entry, "vendor", di.Vendor.Name)
		SetIfNonEmpty(entry, "vendor_id", di.Vendor.ID)
	}
	if di.Product != nil {
		SetIfNonEmpty(entry, "model", di.Product.Name)
		SetIfNonEmpty(entry, "device_id", di.Product.ID)
	}
	if di.Subsystem != nil {
		SetIfNonEmpty(entry, "subsystem", di.Subsystem.Name)
		SetIfNonEmpty(entry, "subsystem_id", di.Subsystem.ID)
	}
	if di.Class != nil {
		SetIfNonEmpty(entry, "class_name", di.Class.Name)
	}
	if di.Subclass != nil {
		SetIfNonEmpty(entry, "subclass_name", di.Subclass.Name)
	}
	if di.ProgrammingInterface != nil {
		SetIfNonEmpty(entry, "programming_interface", di.ProgrammingInterface.Name)
	}
}

// FindGPUCardDir returns the DRM card name, its /sys/class/drm directory and
// its PCI device directory for the given PCI address.
func FindGPUCardDir(pciAddr string) (cardName, cardDir, pciDir string, ok bool) {
	return findGPUCardDir(drmBase, pciAddr)
}

func findGPUCardDir(base, pciAddr string) (string, string, string, bool) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", "", "", false
	}

	normalizedPCI := NormalizePCIAddress(pciAddr)
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "card") || strings.Contains(e.Name(), "-") {
			continue
		}
		cardDir := filepath.Join(base, e.Name())
		if !PathIsDir(cardDir) {
			continue
		}
		deviceLink := Readlink(filepath.Join(cardDir, "device"))
		if NormalizePCIAddress(filepath.Base(deviceLink)) == normalizedPCI {
			return e.Name(), cardDir, filepath.Join(cardDir, "device"), true
		}
	}
	return "", "", "", false
}

// DriverModule returns the kernel module name bound to the PCI device, or "".
func DriverModule(pciDir string) string {
	link := Readlink(filepath.Join(pciDir, "driver", "module"))
	if link == "" {
		return ""
	}
	return filepath.Base(link)
}

// NormalizePCIAddress lower-cases the address and strips a PCI domain wider
// than four digits (nvidia-smi reports "00000000:01:00.0").
func NormalizePCIAddress(addr string) string {
	addr = strings.TrimSpace(strings.ToLower(addr))
	parts := strings.Split(addr, ":")
	if len(parts) == 3 && len(parts[0]) > 4 {
		parts[0] = parts[0][len(parts[0])-4:]
		return strings.Join(parts, ":")
	}
	return addr
}

func SetIfNonEmpty(entry map[string]any, key, value string) {
	value = SanitizeValue(value)
	if value != "" {
		entry[key] = value
	}
}

func ReadSysfsString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func ReadSysfsBool(path string) (bool, bool) {
	switch strings.ToLower(ReadSysfsString(path)) {
	case "1", "y", "yes", "true":
		return true, true
	case "0", "n", "no", "false":
		return false, true
	default:
		return false, false
	}
}

// The *Any readers return the first path that parses; empty paths are skipped.

func ReadSysfsIntAny(paths ...string) (int, bool) {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if value, err := strconv.Atoi(ReadSysfsString(path)); err == nil {
			return value, true
		}
	}
	return 0, false
}

func ReadSysfsUint64Any(paths ...string) (uint64, bool) {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if value, err := strconv.ParseUint(ReadSysfsString(path), 10, 64); err == nil {
			return value, true
		}
	}
	return 0, false
}

func ReadSysfsFloatAny(paths ...string) (float64, bool) {
	for _, path := range paths {
		if path == "" {
			continue
		}
		if value, err := strconv.ParseFloat(ReadSysfsString(path), 64); err == nil {
			return value, true
		}
	}
	return 0, false
}

// Readlink resolves symlinks fully and returns "" on error.
func Readlink(path string) string {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return target
}

func PathIsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func SanitizeUnknown(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "unknown") {
		return ""
	}
	return SanitizeValue(value)
}

func SanitizeZero(value string) string {
	value = SanitizeValue(value)
	if value == "0" {
		return ""
	}
	return value
}

func SanitizeValue(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "n/a", "[not supported]", "[not available]":
		return ""
	default:
		return value
	}
}
