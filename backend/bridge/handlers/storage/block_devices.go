package storage

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
	"github.com/mordilloSan/LinuxIO/backend/common/hostcmd"
)

func GetTopology(ctx context.Context) (apischema.StorageTopologyInventory, error) {
	devices, err := ListBlockDevices(ctx)
	if err != nil {
		return apischema.StorageTopologyInventory{}, err
	}
	partitions, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		return apischema.StorageTopologyInventory{}, fmt.Errorf("list topology mounts: %w", err)
	}
	mounts := make([]apischema.StorageMount, 0, len(partitions))
	for _, partition := range partitions {
		mounts = append(mounts, apischema.StorageMount{
			Device: partition.Device, Path: partition.Mountpoint, FSType: partition.Fstype,
			ReadOnly: slices.Contains(partition.Opts, "ro"),
		})
	}
	return apischema.StorageTopologyInventory{Devices: devices, Mounts: mounts}, nil
}

type topologyBlockDevice struct {
	Name        string                `json:"name"`
	KernelName  string                `json:"kname"`
	Path        string                `json:"path"`
	Type        string                `json:"type"`
	Size        jsontext.Value        `json:"size"`
	FSType      string                `json:"fstype"`
	Mountpoints []string              `json:"mountpoints"`
	Model       string                `json:"model"`
	Serial      string                `json:"serial"`
	Transport   string                `json:"tran"`
	ReadOnly    bool                  `json:"ro"`
	PTType      string                `json:"pttype"`
	Children    []topologyBlockDevice `json:"children"`
}

// runLsblk runs lsblk with args and returns stdout. Both the topology tree and
// the drive inventory go through it.
func runLsblk(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, "lsblk", args...).Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("run lsblk: %w", ctx.Err())
		}
		if exitErr, ok := errors.AsType[*exec.ExitError](err); ok {
			out = exitErr.Stderr
		}
		return nil, hostcmd.OutputError("lsblk", args, out, err)
	}
	return out, nil
}

func ListBlockDevices(ctx context.Context) ([]apischema.StorageBlockDevice, error) {
	out, err := runLsblk(ctx, "--json", "--bytes", "--tree", "--output", "NAME,KNAME,PATH,TYPE,SIZE,FSTYPE,MOUNTPOINTS,MODEL,SERIAL,TRAN,RO,PTTYPE")
	if err != nil {
		return nil, err
	}
	devices, gptDisks, err := parseBlockDevices(out)
	if err != nil {
		return nil, err
	}
	addFreeSpace(devices, gptDisks)
	return devices, nil
}

// parseBlockDevices flattens the lsblk tree and also returns the paths of
// the disks that carry a GPT.
func parseBlockDevices(data []byte) ([]apischema.StorageBlockDevice, map[string]bool, error) {
	var report struct {
		Devices []topologyBlockDevice `json:"blockdevices"`
	}
	if err := json.Unmarshal(data, &report, json.MatchCaseInsensitiveNames(true)); err != nil {
		return nil, nil, fmt.Errorf("parse block devices: %w", err)
	}
	devices := map[string]*apischema.StorageBlockDevice{}
	gptDisks := map[string]bool{}

	for _, raw := range report.Devices {
		if err := collectBlockDevice(raw, "", devices, gptDisks); err != nil {
			return nil, nil, err
		}
	}
	result := make([]apischema.StorageBlockDevice, 0, len(devices))
	for _, device := range devices {
		slices.Sort(device.Parents)
		slices.Sort(device.Mountpoints)
		result = append(result, *device)
	}
	slices.SortFunc(result, func(a, b apischema.StorageBlockDevice) int {
		return strings.Compare(a.Path, b.Path)
	})
	return result, gptDisks, nil
}

func collectBlockDevice(raw topologyBlockDevice, parent string, devices map[string]*apischema.StorageBlockDevice, gptDisks map[string]bool) error {
	// Loop images are implementation detail, not physical storage inventory.
	if raw.Type == "loop" {
		return nil
	}
	if !strings.HasPrefix(raw.Path, "/dev/") || raw.KernelName == "" {
		return fmt.Errorf("block device has no path or kernel name")
	}
	device := devices[raw.Path]
	if device == nil {
		size, err := strconv.ParseUint(strings.Trim(string(raw.Size), `"`), 10, 64)
		if err != nil {
			return fmt.Errorf("parse size of %s: %w", raw.Path, err)
		}
		device = &apischema.StorageBlockDevice{
			Path: raw.Path, Name: raw.Name, KernelName: raw.KernelName,
			Type: raw.Type, SizeBytes: size, FSType: raw.FSType,
			Model: strings.TrimSpace(raw.Model), Serial: strings.TrimSpace(raw.Serial),
			Transport: raw.Transport, ReadOnly: raw.ReadOnly,
			Mountpoints: []string{}, Parents: []string{},
		}
		devices[raw.Path] = device
	}
	if raw.Type == "disk" && raw.PTType == "gpt" {
		gptDisks[raw.Path] = true
	}
	if parent != "" && parent != raw.Path && !slices.Contains(device.Parents, parent) {
		device.Parents = append(device.Parents, parent)
	}
	for _, mount := range raw.Mountpoints {
		if strings.HasPrefix(mount, "/") && !slices.Contains(device.Mountpoints, mount) {
			device.Mountpoints = append(device.Mountpoints, mount)
		}
	}
	for _, child := range raw.Children {
		if err := collectBlockDevice(child, raw.Path, devices, gptDisks); err != nil {
			return err
		}
	}
	return nil
}

// sysClassBlockRoot is where the kernel describes block devices; a var so
// tests can point it at a fake tree.
var sysClassBlockRoot = "/sys/class/block"

// addFreeSpace sets FreeBytes on every GPT disk. A disk whose sysfs entries
// cannot be read keeps 0: it is merely not offered, so the topology still works.
func addFreeSpace(devices []apischema.StorageBlockDevice, gptDisks map[string]bool) {
	for i, d := range devices {
		if !gptDisks[d.Path] {
			continue
		}
		free, err := gptFreeBytes(d, devices)
		if err != nil {
			slog.Debug("read free space of gpt disk failed", "device", d.Path, "error", err)
			continue
		}
		devices[i].FreeBytes = free
	}
}

// gptFreeBytes is the size of the partition btrfs.AddPartition would add to
// disk, from sysfs alone so no privilege is needed to read the raw device.
func gptFreeBytes(disk apischema.StorageBlockDevice, devices []apischema.StorageBlockDevice) (uint64, error) {
	ss, err := readSysUint(disk.KernelName, "queue/logical_block_size")
	if err != nil {
		return 0, err
	}
	if ss == 0 {
		return 0, fmt.Errorf("logical block size of %s is 0", disk.KernelName)
	}
	var used [][2]uint64
	for _, p := range devices {
		if p.Type != "part" || !slices.Contains(p.Parents, disk.Path) {
			continue
		}
		start, err := readSysUint(p.KernelName, "start")
		if err != nil {
			return 0, err
		}
		size, err := readSysUint(p.KernelName, "size")
		if err != nil {
			return 0, err
		}
		if size == 0 {
			continue
		}
		// sysfs counts 512-byte units whatever the logical sector size.
		used = append(used, [2]uint64{start * 512 / ss, (start+size)*512/ss - 1})
	}
	// The GPT defaults: two header sectors and 128 entries of 128 bytes at each end.
	entrySectors, sectors := 128*128/ss, disk.SizeBytes/ss
	if sectors < 2*(2+entrySectors) {
		return 0, nil
	}
	first, last, ok := btrfs.LargestFreeRegion(2+entrySectors, sectors-2-entrySectors, used, ss)
	if !ok {
		return 0, nil
	}
	return (last - first + 1) * ss, nil
}

// readSysUint reads one number from /sys/class/block/<kname>/<file>.
func readSysUint(kname, file string) (uint64, error) {
	b, err := os.ReadFile(filepath.Join(sysClassBlockRoot, kname, file))
	if err != nil {
		return 0, err
	}
	n, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s of %s: %w", file, kname, err)
	}
	return n, nil
}
