package system

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/hwprobe"
)

var gpuDevicesCache hwprobe.SnapshotCache[[]apischema.GpuDevice]

func FetchGPUInfo(ctx context.Context) ([]apischema.GpuDevice, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return gpuDevicesCache.Get(func() ([]apischema.GpuDevice, error) {
		info, err := hwprobe.CachedGPUInfo()
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve GPU information: %w", err)
		}
		if info == nil {
			return nil, errors.New("failed to retrieve GPU information: empty result")
		}

		gpus := make([]apischema.GpuDevice, 0, len(info.GraphicsCards))
		for _, card := range info.GraphicsCards {
			entry := hwprobe.BuildGPUEntry(card)
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

func enrichGPUStaticFromSysfs(pciAddr string, entry map[string]any) {
	cardName, cardDir, pciDir, ok := hwprobe.FindGPUCardDir(pciAddr)
	if !ok {
		return
	}

	entry["drm_card"] = cardName
	hwprobe.SetIfNonEmpty(entry, "max_link_speed", hwprobe.SanitizeUnknown(hwprobe.ReadSysfsString(filepath.Join(pciDir, "max_link_speed"))))
	hwprobe.SetIfNonEmpty(entry, "max_link_width", hwprobe.SanitizeZero(hwprobe.ReadSysfsString(filepath.Join(pciDir, "max_link_width"))))
	if module := hwprobe.DriverModule(pciDir); module != "" {
		hwprobe.SetIfNonEmpty(entry, "driver_module", module)
		hwprobe.SetIfNonEmpty(entry, "driver_version", hwprobe.ReadSysfsString(filepath.Join("/sys/module", module, "version")))
	}
	hwprobe.SetIfNonEmpty(entry, "raw_class", hwprobe.ReadSysfsString(filepath.Join(pciDir, "class")))
	if value, ok := hwprobe.ReadSysfsBool(filepath.Join(pciDir, "boot_vga")); ok {
		entry["boot_vga"] = value
	}
	if value, ok := hwprobe.ReadSysfsUint64Any(filepath.Join(pciDir, "mem_info_vram_total")); ok {
		entry["memory_total_bytes"] = value
	}
	if value, ok := hwprobe.ReadSysfsUint64Any(filepath.Join(pciDir, "mem_info_vis_vram_total")); ok {
		entry["visible_memory_total_bytes"] = value
	}
	if value, ok := hwprobe.ReadSysfsUint64Any(filepath.Join(pciDir, "mem_info_gtt_total")); ok {
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

func setStaticFrequency(entry map[string]any, key, cardDir string, relativePaths ...string) {
	paths := make([]string, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		paths = append(paths, filepath.Join(cardDir, filepath.FromSlash(relativePath)))
	}
	if value, ok := hwprobe.ReadSysfsIntAny(paths...); ok && value > 0 {
		entry[key] = float64(value)
	}
}
