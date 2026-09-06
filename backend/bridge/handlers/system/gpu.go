package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jaypipes/ghw/pkg/gpu"
	"github.com/jaypipes/ghw/pkg/pci"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

var gpuDevicesCache hwSnapshotCache[[]apischema.GpuDevice]

func FetchGPUInfo(ctx context.Context) ([]apischema.GpuDevice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return gpuDevicesCache.get(func() ([]apischema.GpuDevice, error) {
		info, err := cachedGPUInfo()
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve GPU information: %w", err)
		}
		if info == nil {
			return nil, errors.New("failed to retrieve GPU information: empty result")
		}

		gpus := make([]apischema.GpuDevice, 0, len(info.GraphicsCards))
		for _, card := range info.GraphicsCards {
			entry := buildGPUEntry(card)
			enrichGPUStaticFromSysfs(card.Address, entry)
			device, err := gpuDeviceFromEntry(entry)
			if err != nil {
				return nil, fmt.Errorf("build GPU response: %w", err)
			}
			gpus = append(gpus, device)
		}
		return gpus, nil
	})
}

func gpuDeviceFromEntry(entry map[string]any) (apischema.GpuDevice, error) {
	var device apischema.GpuDevice
	data, err := json.Marshal(entry)
	if err != nil {
		return device, err
	}
	return device, json.Unmarshal(data, &device)
}

func buildGPUEntry(card *gpu.GraphicsCard) map[string]any {
	entry := map[string]any{"address": normalizePCIAddress(card.Address)}
	if card.DeviceInfo != nil {
		populateDeviceInfo(entry, card.DeviceInfo)
	}
	if card.Node != nil {
		entry["numa_node"] = card.Node.ID
	}
	return entry
}

func populateDeviceInfo(entry map[string]any, di *pci.Device) {
	setIfNonEmpty(entry, "revision", di.Revision)
	setIfNonEmpty(entry, "driver", di.Driver)
	if di.Vendor != nil {
		setIfNonEmpty(entry, "vendor", di.Vendor.Name)
		setIfNonEmpty(entry, "vendor_id", di.Vendor.ID)
	}
	if di.Product != nil {
		setIfNonEmpty(entry, "model", di.Product.Name)
		setIfNonEmpty(entry, "device_id", di.Product.ID)
	}
	if di.Subsystem != nil {
		setIfNonEmpty(entry, "subsystem", di.Subsystem.Name)
		setIfNonEmpty(entry, "subsystem_id", di.Subsystem.ID)
	}
	if di.Class != nil {
		setIfNonEmpty(entry, "class_name", di.Class.Name)
	}
	if di.Subclass != nil {
		setIfNonEmpty(entry, "subclass_name", di.Subclass.Name)
	}
	if di.ProgrammingInterface != nil {
		setIfNonEmpty(entry, "programming_interface", di.ProgrammingInterface.Name)
	}
}

func enrichGPUStaticFromSysfs(pciAddr string, entry map[string]any) {
	cardName, cardDir, pciDir, ok := findGPUCardDir(pciAddr)
	if !ok {
		return
	}

	entry["drm_card"] = cardName
	setIfNonEmpty(entry, "max_link_speed", sanitizeUnknown(readSysfsString(filepath.Join(pciDir, "max_link_speed"))))
	setIfNonEmpty(entry, "max_link_width", sanitizeZero(readSysfsString(filepath.Join(pciDir, "max_link_width"))))
	module := filepath.Base(readlink(filepath.Join(pciDir, "driver", "module")))
	if module != "." && module != "" {
		setIfNonEmpty(entry, "driver_module", module)
		setIfNonEmpty(entry, "driver_version", readSysfsString(filepath.Join("/sys/module", module, "version")))
	}
	setIfNonEmpty(entry, "raw_class", readSysfsString(filepath.Join(pciDir, "class")))
	if value, ok := readSysfsBool(filepath.Join(pciDir, "boot_vga")); ok {
		entry["boot_vga"] = value
	}
	if value, ok := readSysfsUint64Any(filepath.Join(pciDir, "mem_info_vram_total")); ok {
		entry["memory_total_bytes"] = value
	}
	if value, ok := readSysfsUint64Any(filepath.Join(pciDir, "mem_info_vis_vram_total")); ok {
		entry["visible_memory_total_bytes"] = value
	}
	if value, ok := readSysfsUint64Any(filepath.Join(pciDir, "mem_info_gtt_total")); ok {
		entry["gtt_total_bytes"] = value
	}
	setStaticFrequency(entry, "min_freq_mhz", cardDir,
		"gt_min_freq_mhz", "gt/gt0/rps_min_freq_mhz")
	setStaticFrequency(entry, "max_freq_mhz", cardDir,
		"gt_max_freq_mhz", "gt/gt0/rps_max_freq_mhz")
	setStaticFrequency(entry, "boost_freq_mhz", cardDir,
		"gt_boost_freq_mhz", "gt/gt0/rps_boost_freq_mhz")
	setStaticFrequency(entry, "rp0_freq_mhz", cardDir,
		"gt_RP0_freq_mhz", "gt/gt0/rps_RP0_freq_mhz")
	setStaticFrequency(entry, "rp1_freq_mhz", cardDir,
		"gt_RP1_freq_mhz", "gt/gt0/rps_RP1_freq_mhz")
	setStaticFrequency(entry, "rpn_freq_mhz", cardDir,
		"gt_RPn_freq_mhz", "gt/gt0/rps_RPn_freq_mhz")
}

func findGPUCardDir(pciAddr string) (string, string, string, bool) {
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return "", "", "", false
	}

	normalizedPCI := normalizePCIAddress(pciAddr)
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "card") || strings.Contains(e.Name(), "-") {
			continue
		}
		cardDir := filepath.Join("/sys/class/drm", e.Name())
		if !pathIsDir(cardDir) {
			continue
		}
		deviceLink := readlink(filepath.Join(cardDir, "device"))
		if normalizePCIAddress(filepath.Base(deviceLink)) == normalizedPCI {
			return e.Name(), cardDir, filepath.Join(cardDir, "device"), true
		}
	}
	return "", "", "", false
}

func normalizePCIAddress(addr string) string {
	parts := strings.Split(strings.TrimSpace(strings.ToLower(addr)), ":")
	if len(parts) == 3 && len(parts[0]) > 4 {
		parts[0] = parts[0][len(parts[0])-4:]
	}
	return strings.Join(parts, ":")
}

func setIfNonEmpty(entry map[string]any, key, value string) {
	value = sanitizeValue(value)
	if value != "" {
		entry[key] = value
	}
}

func readSysfsString(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func readSysfsBool(path string) (bool, bool) {
	switch strings.ToLower(readSysfsString(path)) {
	case "1", "y", "yes", "true":
		return true, true
	case "0", "n", "no", "false":
		return false, true
	default:
		return false, false
	}
}

func readSysfsIntAny(paths ...string) (int, bool) {
	for _, path := range paths {
		if value, err := strconv.Atoi(readSysfsString(path)); err == nil {
			return value, true
		}
	}
	return 0, false
}

func setStaticFrequency(entry map[string]any, key, cardDir string, relativePaths ...string) {
	paths := make([]string, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		paths = append(paths, filepath.Join(cardDir, filepath.FromSlash(relativePath)))
	}
	if value, ok := readSysfsIntAny(paths...); ok && value > 0 {
		entry[key] = float64(value)
	}
}

func readSysfsUint64Any(paths ...string) (uint64, bool) {
	for _, path := range paths {
		if value, err := strconv.ParseUint(readSysfsString(path), 10, 64); err == nil {
			return value, true
		}
	}
	return 0, false
}

func readlink(path string) string {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return target
}

func pathIsDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func sanitizeUnknown(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "unknown") {
		return ""
	}
	return sanitizeValue(value)
}

func sanitizeZero(value string) string {
	value = sanitizeValue(value)
	if value == "0" {
		return ""
	}
	return value
}

func sanitizeValue(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToLower(value) {
	case "", "n/a", "[not supported]", "[not available]":
		return ""
	default:
		return value
	}
}
