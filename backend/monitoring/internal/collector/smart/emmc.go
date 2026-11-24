package smart

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mordilloSan/LinuxIO/backend/common/hwprobe"
	smartmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
)

// emmcSysfsRoot is a test hook; production value is "/sys".
var emmcSysfsRoot = "/sys"

type emmcHealth struct {
	model    string
	serial   string
	revision string
	capacity uint64
	preEOL   uint8
	lifeA    uint8
	lifeB    uint8
}

// scanEmmcDevices returns the /dev paths of the eMMC devices whose health
// sysfs exposes.
func scanEmmcDevices() []string {
	blockDir := filepath.Join(emmcSysfsRoot, "class", "block")
	entries, err := os.ReadDir(blockDir)
	if err != nil {
		return nil
	}

	var devices []string
	for _, ent := range entries {
		name := ent.Name()
		if !isEmmcBlockName(name) {
			continue
		}
		if !hasEmmcHealthFiles(filepath.Join(blockDir, name, "device")) {
			continue
		}
		devices = append(devices, filepath.Join("/dev", name))
	}
	return devices
}

// readEmmcSmartData reads an eMMC device's health from sysfs and returns its
// record and the key it is kept under (the serial, or the /dev path without
// one).
func readEmmcSmartData(path string) (string, *smartmodel.SmartData, bool) {
	base := filepath.Base(path)
	health, ok := readEmmcHealth(base)
	if !ok {
		return "", nil, false
	}

	key := health.serial
	if key == "" {
		key = filepath.Join("/dev", base)
	}
	return key, &smartmodel.SmartData{
		ModelName:       health.model,
		SerialNumber:    health.serial,
		FirmwareVersion: health.revision,
		Capacity:        health.capacity,
		SmartStatus:     emmcSmartStatus(health.preEOL),
		DiskName:        filepath.Join("/dev", base),
		DiskType:        "emmc",
		Attributes: []*smartmodel.SmartAttribute{
			{
				Name:      "PreEOLInfo",
				RawValue:  uint64(health.preEOL),
				RawString: emmcPreEOLString(health.preEOL),
			},
			{
				Name:      "DeviceLifeTimeEstA",
				RawValue:  uint64(health.lifeA),
				RawString: emmcLifeTimeString(health.lifeA),
			},
			{
				Name:      "DeviceLifeTimeEstB",
				RawValue:  uint64(health.lifeB),
				RawString: emmcLifeTimeString(health.lifeB),
			},
		},
	}, true
}

func readEmmcHealth(blockName string) (emmcHealth, bool) {
	var out emmcHealth

	if !isEmmcBlockName(blockName) {
		return out, false
	}

	deviceDir := filepath.Join(emmcSysfsRoot, "class", "block", blockName, "device")
	preEOL, okPre := readHexByteFile(filepath.Join(deviceDir, "pre_eol_info"))

	// Some kernels expose EXT_CSD lifetime via "life_time" (two bytes), others as
	// separate files. Support both.
	lifeA, lifeB, okLife := readLifeTime(deviceDir)

	if !okPre && !okLife {
		return out, false
	}

	out.preEOL = preEOL
	out.lifeA = lifeA
	out.lifeB = lifeB

	out.model = hwprobe.ReadSysfsString(filepath.Join(deviceDir, "name"))
	out.serial = hwprobe.ReadSysfsString(filepath.Join(deviceDir, "serial"))
	out.revision = hwprobe.ReadSysfsString(filepath.Join(deviceDir, "prv"))

	if capBytes, ok := readBlockCapacityBytes(blockName); ok {
		out.capacity = capBytes
	}

	return out, true
}

func readLifeTime(deviceDir string) (uint8, uint8, bool) {
	if content := hwprobe.ReadSysfsString(filepath.Join(deviceDir, "life_time")); content != "" {
		a, b, ok := parseHexBytePair(content)
		return a, b, ok
	}

	a, okA := readHexByteFile(filepath.Join(deviceDir, "device_life_time_est_typ_a"))
	b, okB := readHexByteFile(filepath.Join(deviceDir, "device_life_time_est_typ_b"))
	if okA || okB {
		return a, b, true
	}
	return 0, 0, false
}

func readBlockCapacityBytes(blockName string) (uint64, bool) {
	sizePath := filepath.Join(emmcSysfsRoot, "class", "block", blockName, "size")

	sectors, err := strconv.ParseUint(hwprobe.ReadSysfsString(sizePath), 10, 64)
	if err != nil || sectors == 0 {
		return 0, false
	}

	return sectors * 512, true
}

func readHexByteFile(path string) (uint8, bool) {
	return parseHexOrDecByte(hwprobe.ReadSysfsString(path))
}

func emmcPreEOLString(preEOL uint8) string {
	switch preEOL {
	case 0x01:
		return "0x01 (normal)"
	case 0x02:
		return "0x02 (warning)"
	case 0x03:
		return "0x03 (urgent)"
	default:
		return fmt.Sprintf("0x%02x", preEOL)
	}
}

func emmcLifeTimeString(v uint8) string {
	// JEDEC eMMC: 0x01..0x0A => 0-100% used in 10% steps, 0x0B => exceeded.
	switch {
	case v == 0:
		return "0x00 (not reported)"
	case v >= 0x01 && v <= 0x0A:
		low := int(v-1) * 10
		high := int(v) * 10
		return fmt.Sprintf("0x%02x (%d-%d%% used)", v, low, high)
	case v == 0x0B:
		return "0x0b (>100% used)"
	default:
		return fmt.Sprintf("0x%02x", v)
	}
}

func hasEmmcHealthFiles(deviceDir string) bool {
	entries, err := os.ReadDir(deviceDir)
	if err != nil {
		return false
	}
	for _, ent := range entries {
		switch ent.Name() {
		case "pre_eol_info", "life_time", "device_life_time_est_typ_a", "device_life_time_est_typ_b":
			return true
		}
	}
	return false
}
