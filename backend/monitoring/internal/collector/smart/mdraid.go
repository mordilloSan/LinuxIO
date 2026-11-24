package smart

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/mordilloSan/LinuxIO/backend/common/hwprobe"
	smartmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
)

// mdraidSysfsRoot is a test hook; production value is "/sys".
var mdraidSysfsRoot = "/sys"

type mdraidHealth struct {
	level         string
	arrayState    string
	degraded      uint64
	raidDisks     uint64
	syncAction    string
	syncCompleted string
	syncSpeed     string
	mismatchCnt   uint64
	capacity      uint64
}

// scanMdraidDevices returns the /dev paths of the Linux md arrays exposed in
// sysfs.
func scanMdraidDevices() []string {
	blockDir := filepath.Join(mdraidSysfsRoot, "block")
	entries, err := os.ReadDir(blockDir)
	if err != nil {
		return nil
	}

	var devices []string
	for _, ent := range entries {
		name := ent.Name()
		if !isMdraidBlockName(name) {
			continue
		}
		if _, err := os.Stat(filepath.Join(blockDir, name, "md", "array_state")); err != nil {
			continue
		}
		devices = append(devices, filepath.Join("/dev", name))
	}
	return devices
}

// readMdraidSmartData reads an md array's health from sysfs and returns its
// record and the key it is kept under ("mdraid:<name>").
func readMdraidSmartData(path string) (string, *smartmodel.SmartData, bool) {
	base := filepath.Base(path)
	health, ok := readMdraidHealth(base)
	if !ok {
		return "", nil, false
	}

	attrs := make([]*smartmodel.SmartAttribute, 0, 10)
	if health.arrayState != "" {
		attrs = append(attrs, &smartmodel.SmartAttribute{Name: "ArrayState", RawString: health.arrayState})
	}
	if health.level != "" {
		attrs = append(attrs, &smartmodel.SmartAttribute{Name: "RaidLevel", RawString: health.level})
	}
	if health.raidDisks > 0 {
		attrs = append(attrs, &smartmodel.SmartAttribute{Name: "RaidDisks", RawValue: health.raidDisks})
	}
	if health.degraded > 0 {
		attrs = append(attrs, &smartmodel.SmartAttribute{Name: "Degraded", RawValue: health.degraded})
	}
	if health.syncAction != "" {
		attrs = append(attrs, &smartmodel.SmartAttribute{Name: "SyncAction", RawString: health.syncAction})
	}
	if health.syncCompleted != "" {
		attrs = append(attrs, &smartmodel.SmartAttribute{Name: "SyncCompleted", RawString: health.syncCompleted})
	}
	if health.syncSpeed != "" {
		attrs = append(attrs, &smartmodel.SmartAttribute{Name: "SyncSpeed", RawString: health.syncSpeed})
	}
	if health.mismatchCnt > 0 {
		attrs = append(attrs, &smartmodel.SmartAttribute{Name: "MismatchCount", RawValue: health.mismatchCnt})
	}

	model := "Linux MD RAID"
	if health.level != "" {
		model = "Linux MD RAID (" + health.level + ")"
	}
	return fmt.Sprintf("mdraid:%s", base), &smartmodel.SmartData{
		ModelName:   model,
		Capacity:    health.capacity,
		SmartStatus: mdraidSmartStatus(health),
		DiskName:    filepath.Join("/dev", base),
		DiskType:    "mdraid",
		Attributes:  attrs,
	}, true
}

// readMdraidHealth reads md array health fields from sysfs.
func readMdraidHealth(blockName string) (mdraidHealth, bool) {
	var out mdraidHealth

	if !isMdraidBlockName(blockName) {
		return out, false
	}

	mdDir := filepath.Join(mdraidSysfsRoot, "block", blockName, "md")
	arrayState := hwprobe.ReadSysfsString(filepath.Join(mdDir, "array_state"))
	if arrayState == "" {
		return out, false
	}

	out.arrayState = arrayState
	out.level = hwprobe.ReadSysfsString(filepath.Join(mdDir, "level"))
	out.syncAction = hwprobe.ReadSysfsString(filepath.Join(mdDir, "sync_action"))
	out.syncCompleted = hwprobe.ReadSysfsString(filepath.Join(mdDir, "sync_completed"))
	out.syncSpeed = hwprobe.ReadSysfsString(filepath.Join(mdDir, "sync_speed"))

	if val, ok := hwprobe.ReadSysfsUint64Any(filepath.Join(mdDir, "raid_disks")); ok {
		out.raidDisks = val
	}
	if val, ok := hwprobe.ReadSysfsUint64Any(filepath.Join(mdDir, "degraded")); ok {
		out.degraded = val
	}
	if val, ok := hwprobe.ReadSysfsUint64Any(filepath.Join(mdDir, "mismatch_cnt")); ok {
		out.mismatchCnt = val
	}

	if capBytes, ok := readMdraidBlockCapacityBytes(blockName, mdraidSysfsRoot); ok {
		out.capacity = capBytes
	}

	return out, true
}

// mdraidSmartStatus maps md state/sync signals to a SMART-like status.
func mdraidSmartStatus(health mdraidHealth) string {
	state := strings.ToLower(strings.TrimSpace(health.arrayState))
	switch state {
	case "inactive", "faulty", "broken", "stopped":
		return "FAILED"
	}
	// During rebuild/recovery, arrays are often temporarily degraded; report as
	// warning instead of hard failure while synchronization is in progress.
	syncAction := strings.ToLower(strings.TrimSpace(health.syncAction))
	switch syncAction {
	case "resync", "recover", "reshape":
		return "WARNING"
	}
	if health.degraded > 0 {
		return "FAILED"
	}
	switch syncAction {
	case "check", "repair":
		return "WARNING"
	}
	switch state {
	case "clean", "active", "active-idle", "write-pending", "read-auto", "readonly":
		return "PASSED"
	}
	return "UNKNOWN"
}

// isMdraidBlockName matches /dev/mdN-style block device names.
func isMdraidBlockName(name string) bool {
	if !strings.HasPrefix(name, "md") {
		return false
	}
	suffix := strings.TrimPrefix(name, "md")
	if suffix == "" {
		return false
	}
	for _, c := range suffix {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// readMdraidBlockCapacityBytes converts sysfs size (always 512-byte units) into bytes.
func readMdraidBlockCapacityBytes(blockName, root string) (uint64, bool) {
	sizePath := filepath.Join(root, "block", blockName, "size")

	sectors, err := strconv.ParseUint(hwprobe.ReadSysfsString(sizePath), 10, 64)
	if err != nil || sectors == 0 {
		return 0, false
	}

	return sectors * 512, true
}
