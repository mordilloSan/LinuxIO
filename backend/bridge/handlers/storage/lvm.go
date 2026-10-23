package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

// Validation patterns
var (
	validVGName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	validLVName = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)
	validSize   = regexp.MustCompile(`^[0-9]+[KMGTkmgt]?$`)
)

// lvmCommand and listPartitions are test seams.
var (
	lvmCommand     = runLVMCommand
	listPartitions = disk.Partitions
)

// lvmExitNoObjects is the exit status pvs/vgs/lvs use when nothing matches.
const lvmExitNoObjects = 5

func runLVMCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	path, err := hostcmd.LookPath(name)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, path, args...).Output()
}

// lvmReport runs a pvs/vgs/lvs JSON report into report. A missing LVM toolset
// or an empty result (exit 5) leaves report empty; every other failure is an error.
func lvmReport(ctx context.Context, report any, name string, args ...string) error {
	out, err := lvmCommand(ctx, name, args...)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil
		}
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			if exitErr.ExitCode() == lvmExitNoObjects {
				return nil
			}
			out = exitErr.Stderr
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("run %s: %w", name, ctxErr)
		}
		return hostcmd.OutputError(name, args, out, err)
	}
	if parseErr := json.Unmarshal(out, report); parseErr != nil {
		return fmt.Errorf("failed to parse %s output: %w", name, parseErr)
	}
	return nil
}

// ListPhysicalVolumes returns all LVM physical volumes
func ListPhysicalVolumes(ctx context.Context) ([]apischema.PhysicalVolume, error) {
	var report pvsReport
	if err := lvmReport(ctx, &report, "pvs", "--reportformat", "json", "--units", "b", "--nosuffix",
		"-o", "pv_name,vg_name,pv_size,pv_free,pv_attr,pv_fmt"); err != nil {
		return nil, err
	}

	pvs := make([]apischema.PhysicalVolume, 0)
	for _, r := range report.Report {
		for _, pv := range r.PV {
			size, _ := strconv.ParseUint(pv.PVSize, 10, 64)
			free, _ := strconv.ParseUint(pv.PVFree, 10, 64)
			pvs = append(pvs, apischema.PhysicalVolume{
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
func ListVolumeGroups(ctx context.Context) ([]apischema.VolumeGroup, error) {
	var report vgsReport
	if err := lvmReport(ctx, &report, "vgs", "--reportformat", "json", "--units", "b", "--nosuffix",
		"-o", "vg_name,vg_size,vg_free,pv_count,lv_count,vg_attr"); err != nil {
		return nil, err
	}

	pvNamesByVG, err := getPVNamesByVG(ctx)
	if err != nil {
		return nil, err
	}

	vgs := make([]apischema.VolumeGroup, 0)
	for _, r := range report.Report {
		for _, vg := range r.VG {
			size, _ := strconv.ParseUint(vg.VGSize, 10, 64)
			free, _ := strconv.ParseUint(vg.VGFree, 10, 64)
			pvCount, _ := strconv.Atoi(vg.PVCount)
			lvCount, _ := strconv.Atoi(vg.LVCount)

			vgs = append(vgs, apischema.VolumeGroup{
				Name:       vg.VGName,
				Size:       size,
				Free:       free,
				PVCount:    pvCount,
				LVCount:    lvCount,
				Attributes: vg.VGAttr,
				PVNames:    pvNamesByVG[vg.VGName],
			})
		}
	}
	return vgs, nil
}

// ListLogicalVolumes returns all LVM logical volumes with mount info
func ListLogicalVolumes(ctx context.Context) ([]apischema.LogicalVolume, error) {
	var report lvsReport
	if err := lvmReport(ctx, &report, "lvs", "--reportformat", "json", "--units", "b", "--nosuffix",
		"-o", "lv_name,vg_name,lv_size,lv_path,lv_dm_path,lv_attr"); err != nil {
		return nil, err
	}

	// Get mount info for all partitions
	partitions, _ := listPartitions(true)
	mountMap := make(map[string]disk.PartitionStat)
	for _, p := range partitions {
		mountMap[p.Device] = p
	}

	lvs := make([]apischema.LogicalVolume, 0)
	for _, r := range report.Report {
		for _, lv := range r.LV {
			size, _ := strconv.ParseUint(lv.LVSize, 10, 64)

			lvInfo := apischema.LogicalVolume{
				Name:       lv.LVName,
				VGName:     lv.VGName,
				Size:       size,
				Path:       lv.LVPath,
				Attributes: lv.LVAttr,
			}

			// Check if mounted; mountinfo reports the /dev/mapper name, not lv_path.
			if partition, ok := mountMap[lv.LVDMPath]; ok {
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
func CreateLogicalVolume(ctx context.Context, vgName, lvName, size string) (apischema.SuccessPathResponse, error) {
	// Validate inputs
	if !validVGName.MatchString(vgName) {
		slog.Warn("invalid volume group name", "volume_group", vgName)
		return apischema.SuccessPathResponse{}, fmt.Errorf("invalid volume group name")
	}
	if !validLVName.MatchString(lvName) {
		slog.Warn("invalid logical volume name", "name", lvName)
		return apischema.SuccessPathResponse{}, fmt.Errorf("invalid logical volume name")
	}
	if !validSize.MatchString(size) {
		slog.Warn("invalid logical volume size", "size", size)
		return apischema.SuccessPathResponse{}, fmt.Errorf("invalid size format (use e.g., 10G, 500M)")
	}
	slog.Info("creating logical volume", "volume_group", vgName, "name", lvName, "size", size)
	cmd := exec.CommandContext(ctx, "lvcreate", "-L", size, "-n", lvName, vgName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("lvcreate failed", "volume_group", vgName, "name", lvName, "output", strings.TrimSpace(string(out)))
		return apischema.SuccessPathResponse{}, fmt.Errorf("lvcreate failed: %s", strings.TrimSpace(string(out)))
	}
	path := fmt.Sprintf("/dev/%s/%s", vgName, lvName)
	slog.Info("logical volume created", "path", path, "size", size)
	return apischema.SuccessPathResponse{
		Success: true,
		Path:    path,
	}, nil
}

// DeleteLogicalVolume removes a logical volume
func DeleteLogicalVolume(ctx context.Context, vgName, lvName string) (apischema.SuccessResponse, error) {
	// Validate inputs
	if !validVGName.MatchString(vgName) {
		slog.Warn("invalid volume group name", "volume_group", vgName)
		return apischema.SuccessResponse{}, fmt.Errorf("invalid volume group name")
	}
	if !validLVName.MatchString(lvName) {
		slog.Warn("invalid logical volume name", "name", lvName)
		return apischema.SuccessResponse{}, fmt.Errorf("invalid logical volume name")
	}

	lvPath := fmt.Sprintf("/dev/%s/%s", vgName, lvName)

	// Check if mounted
	partitions, _ := disk.Partitions(true)
	for _, p := range partitions {
		if p.Device == lvPath {
			slog.Warn("cannot delete mounted logical volume", "path", lvPath, "mountpoint", p.Mountpoint)
			return apischema.SuccessResponse{}, fmt.Errorf("logical volume is mounted at %s - unmount first", p.Mountpoint)
		}
	}
	slog.Info("deleting logical volume", "path", lvPath)
	cmd := exec.CommandContext(ctx, "lvremove", "-f", lvPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("lvremove failed", "path", lvPath, "output", strings.TrimSpace(string(out)))
		return apischema.SuccessResponse{}, fmt.Errorf("lvremove failed: %s", strings.TrimSpace(string(out)))
	}
	slog.Info("logical volume deleted", "path", lvPath)
	return apischema.SuccessResponse{Success: true}, nil
}

// ResizeLogicalVolume resizes a logical volume (and its filesystem if mounted)
func ResizeLogicalVolume(ctx context.Context, vgName, lvName, newSize string) (apischema.SuccessResponse, error) {
	// Validate inputs
	if !validVGName.MatchString(vgName) {
		slog.Warn("invalid volume group name", "volume_group", vgName)
		return apischema.SuccessResponse{}, fmt.Errorf("invalid volume group name")
	}
	if !validLVName.MatchString(lvName) {
		slog.Warn("invalid logical volume name", "name", lvName)
		return apischema.SuccessResponse{}, fmt.Errorf("invalid logical volume name")
	}
	if !validSize.MatchString(newSize) {
		slog.Warn("invalid logical volume size", "size", newSize)
		return apischema.SuccessResponse{}, fmt.Errorf("invalid size format (use e.g., 10G, 500M)")
	}

	lvPath := fmt.Sprintf("/dev/%s/%s", vgName, lvName)
	// Use lvresize -r to also resize filesystem if present.
	slog.Info("resizing logical volume", "path", lvPath, "size", newSize)
	cmd := exec.CommandContext(ctx, "lvresize", "-r", "-L", newSize, lvPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.Error("lvresize failed", "path", lvPath, "output", strings.TrimSpace(string(out)))
		return apischema.SuccessResponse{}, fmt.Errorf("lvresize failed: %s", strings.TrimSpace(string(out)))
	}
	slog.Info("logical volume resized", "path", lvPath, "size", newSize)
	return apischema.SuccessResponse{Success: true}, nil
}

// getPVNamesByVG returns the physical volume names for all volume groups with
// a single pvs invocation, avoiding one subprocess per VG.
func getPVNamesByVG(ctx context.Context) (map[string][]string, error) {
	var report pvsReport
	if err := lvmReport(ctx, &report, "pvs", "--reportformat", "json", "-o", "pv_name,vg_name"); err != nil {
		return nil, err
	}

	namesByVG := make(map[string][]string)
	for _, r := range report.Report {
		for _, pv := range r.PV {
			if pv.VGName == "" {
				continue
			}
			namesByVG[pv.VGName] = append(namesByVG[pv.VGName], pv.PVName)
		}
	}
	return namesByVG, nil
}
