package storage

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/mordilloSan/go-logger/logger"
	"github.com/shirou/gopsutil/v4/disk"
)

// Validation patterns
var (
	validVGName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	validLVName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	validSize   = regexp.MustCompile(`^[0-9]+[KMGTkmgt]?$`)
)

// ListPhysicalVolumes returns all LVM physical volumes
func ListPhysicalVolumes() ([]PhysicalVolume, error) {
	cmd := exec.Command("pvs", "--reportformat", "json", "--units", "b", "--nosuffix",
		"-o", "pv_name,vg_name,pv_size,pv_free,pv_attr,pv_fmt")
	out, err := cmd.Output()
	if err != nil {
		// LVM not installed or no PVs - return empty list
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 5 {
				// No PVs found
				return []PhysicalVolume{}, nil
			}
		}
		return []PhysicalVolume{}, nil
	}

	var report pvsReport
	if err := json.Unmarshal(out, &report); err != nil {
		return nil, fmt.Errorf("failed to parse pvs output: %w", err)
	}

	var pvs []PhysicalVolume
	for _, r := range report.Report {
		for _, pv := range r.PV {
			size, _ := strconv.ParseUint(pv.PVSize, 10, 64)
			free, _ := strconv.ParseUint(pv.PVFree, 10, 64)
			pvs = append(pvs, PhysicalVolume{
				Name:       pv.PVName,
				VGName:     pv.VGName,
				Size:       size,
				Free:       free,
				Attributes: pv.PVAttr,
				Format:     pv.PVFmt,
			})
		}
	}
	return pvs, nil
}

// ListVolumeGroups returns all LVM volume groups
func ListVolumeGroups() ([]VolumeGroup, error) {
	cmd := exec.Command("vgs", "--reportformat", "json", "--units", "b", "--nosuffix",
		"-o", "vg_name,vg_size,vg_free,pv_count,lv_count,vg_attr")
	out, err := cmd.Output()
	if err != nil {
		return []VolumeGroup{}, nil
	}

	var report vgsReport
	if err := json.Unmarshal(out, &report); err != nil {
		return nil, fmt.Errorf("failed to parse vgs output: %w", err)
	}

	var vgs []VolumeGroup
	for _, r := range report.Report {
		for _, vg := range r.VG {
			size, _ := strconv.ParseUint(vg.VGSize, 10, 64)
			free, _ := strconv.ParseUint(vg.VGFree, 10, 64)
			pvCount, _ := strconv.Atoi(vg.PVCount)
			lvCount, _ := strconv.Atoi(vg.LVCount)

			// Get PV names for this VG
			pvNames := getPVNamesForVG(vg.VGName)

			vgs = append(vgs, VolumeGroup{
				Name:       vg.VGName,
				Size:       size,
				Free:       free,
				PVCount:    pvCount,
				LVCount:    lvCount,
				Attributes: vg.VGAttr,
				PVNames:    pvNames,
			})
		}
	}
	return vgs, nil
}

// ListLogicalVolumes returns all LVM logical volumes with mount info
func ListLogicalVolumes() ([]LogicalVolume, error) {
	cmd := exec.Command("lvs", "--reportformat", "json", "--units", "b", "--nosuffix",
		"-o", "lv_name,vg_name,lv_size,lv_path,lv_attr")
	out, err := cmd.Output()
	if err != nil {
		return []LogicalVolume{}, nil
	}

	var report lvsReport
	if err := json.Unmarshal(out, &report); err != nil {
		return nil, fmt.Errorf("failed to parse lvs output: %w", err)
	}

	// Get mount info for all partitions
	partitions, _ := disk.Partitions(true)
	mountMap := make(map[string]disk.PartitionStat)
	for _, p := range partitions {
		mountMap[p.Device] = p
	}

	var lvs []LogicalVolume
	for _, r := range report.Report {
		for _, lv := range r.LV {
			size, _ := strconv.ParseUint(lv.LVSize, 10, 64)

			lvInfo := LogicalVolume{
				Name:       lv.LVName,
				VGName:     lv.VGName,
				Size:       size,
				Path:       lv.LVPath,
				Attributes: lv.LVAttr,
			}

			// Check if mounted
			if partition, ok := mountMap[lv.LVPath]; ok {
				lvInfo.Mountpoint = partition.Mountpoint
				lvInfo.FSType = partition.Fstype

				// Get usage info
				if usage, err := disk.Usage(partition.Mountpoint); err == nil {
					lvInfo.UsedPct = usage.UsedPercent
				}
			}

			lvs = append(lvs, lvInfo)
		}
	}
	return lvs, nil
}

// CreateLogicalVolume creates a new logical volume
func CreateLogicalVolume(vgName, lvName, size string) (map[string]any, error) {
	// Validate inputs
	if !validVGName.MatchString(vgName) {
		logger.Warnf("[LVM] Invalid volume group name: %s", vgName)
		return nil, fmt.Errorf("invalid volume group name")
	}
	if !validLVName.MatchString(lvName) {
		logger.Warnf("[LVM] Invalid logical volume name: %s", lvName)
		return nil, fmt.Errorf("invalid logical volume name")
	}
	if !validSize.MatchString(size) {
		logger.Warnf("[LVM] Invalid size format: %s", size)
		return nil, fmt.Errorf("invalid size format (use e.g., 10G, 500M)")
	}

	logger.Debugf("[LVM] Executing: lvcreate -L %s -n %s %s", size, lvName, vgName)
	cmd := exec.Command("lvcreate", "-L", size, "-n", lvName, vgName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("[LVM] lvcreate failed: %s", strings.TrimSpace(string(out)))
		return nil, fmt.Errorf("lvcreate failed: %s", strings.TrimSpace(string(out)))
	}

	logger.Infof("[LVM] Created logical volume /dev/%s/%s with size %s", vgName, lvName, size)
	return map[string]any{
		"success": true,
		"path":    fmt.Sprintf("/dev/%s/%s", vgName, lvName),
	}, nil
}

// DeleteLogicalVolume removes a logical volume
func DeleteLogicalVolume(vgName, lvName string) (map[string]any, error) {
	// Validate inputs
	if !validVGName.MatchString(vgName) {
		logger.Warnf("[LVM] Invalid volume group name: %s", vgName)
		return nil, fmt.Errorf("invalid volume group name")
	}
	if !validLVName.MatchString(lvName) {
		logger.Warnf("[LVM] Invalid logical volume name: %s", lvName)
		return nil, fmt.Errorf("invalid logical volume name")
	}

	lvPath := fmt.Sprintf("/dev/%s/%s", vgName, lvName)

	// Check if mounted
	partitions, _ := disk.Partitions(true)
	for _, p := range partitions {
		if p.Device == lvPath {
			logger.Warnf("[LVM] Cannot delete %s - mounted at %s", lvPath, p.Mountpoint)
			return nil, fmt.Errorf("logical volume is mounted at %s - unmount first", p.Mountpoint)
		}
	}

	logger.Debugf("[LVM] Executing: lvremove -f %s", lvPath)
	cmd := exec.Command("lvremove", "-f", lvPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("[LVM] lvremove failed: %s", strings.TrimSpace(string(out)))
		return nil, fmt.Errorf("lvremove failed: %s", strings.TrimSpace(string(out)))
	}

	logger.Infof("[LVM] Deleted logical volume %s", lvPath)
	return map[string]any{"success": true}, nil
}

// ResizeLogicalVolume resizes a logical volume (and its filesystem if mounted)
func ResizeLogicalVolume(vgName, lvName, newSize string) (map[string]any, error) {
	// Validate inputs
	if !validVGName.MatchString(vgName) {
		logger.Warnf("[LVM] Invalid volume group name: %s", vgName)
		return nil, fmt.Errorf("invalid volume group name")
	}
	if !validLVName.MatchString(lvName) {
		logger.Warnf("[LVM] Invalid logical volume name: %s", lvName)
		return nil, fmt.Errorf("invalid logical volume name")
	}
	if !validSize.MatchString(newSize) {
		logger.Warnf("[LVM] Invalid size format: %s", newSize)
		return nil, fmt.Errorf("invalid size format (use e.g., 10G, 500M)")
	}

	lvPath := fmt.Sprintf("/dev/%s/%s", vgName, lvName)

	// Use lvresize -r to also resize filesystem if present
	logger.Debugf("[LVM] Executing: lvresize -r -L %s %s", newSize, lvPath)
	cmd := exec.Command("lvresize", "-r", "-L", newSize, lvPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		logger.Errorf("[LVM] lvresize failed: %s", strings.TrimSpace(string(out)))
		return nil, fmt.Errorf("lvresize failed: %s", strings.TrimSpace(string(out)))
	}

	logger.Infof("[LVM] Resized logical volume %s to %s", lvPath, newSize)
	return map[string]any{"success": true}, nil
}

// getPVNamesForVG returns the physical volume names for a given volume group
func getPVNamesForVG(vgName string) []string {
	cmd := exec.Command("pvs", "--reportformat", "json", "-o", "pv_name", "-S", fmt.Sprintf("vg_name=%s", vgName))
	out, err := cmd.Output()
	if err != nil {
		return []string{}
	}

	var report pvsReport
	if err := json.Unmarshal(out, &report); err != nil {
		return []string{}
	}

	var names []string
	for _, r := range report.Report {
		for _, pv := range r.PV {
			names = append(names, pv.PVName)
		}
	}
	return names
}
