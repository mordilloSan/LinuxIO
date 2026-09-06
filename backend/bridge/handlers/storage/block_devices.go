package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/disk"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
	"github.com/mordilloSan/LinuxIO/backend/common/utils"
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
	Size        json.Number           `json:"size"`
	FSType      string                `json:"fstype"`
	Mountpoints []string              `json:"mountpoints"`
	Model       string                `json:"model"`
	Serial      string                `json:"serial"`
	Transport   string                `json:"tran"`
	ReadOnly    bool                  `json:"ro"`
	Children    []topologyBlockDevice `json:"children"`
}

func ListBlockDevices(ctx context.Context) ([]apischema.StorageBlockDevice, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	args := []string{"--json", "--bytes", "--tree", "--output", "NAME,KNAME,PATH,TYPE,SIZE,FSTYPE,MOUNTPOINTS,MODEL,SERIAL,TRAN,RO"}
	cmd := exec.CommandContext(ctx, "lsblk", args...)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("list block devices: %w", ctx.Err())
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			out = exitErr.Stderr
		}
		return nil, utils.CommandOutputError("lsblk", args, out, err)
	}
	return parseBlockDevices(out)
}

func parseBlockDevices(data []byte) ([]apischema.StorageBlockDevice, error) {
	var report struct {
		Devices []topologyBlockDevice `json:"blockdevices"`
	}
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parse block devices: %w", err)
	}
	devices := map[string]*apischema.StorageBlockDevice{}

	for _, raw := range report.Devices {
		if err := collectBlockDevice(raw, "", devices); err != nil {
			return nil, err
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
	return result, nil
}

func collectBlockDevice(raw topologyBlockDevice, parent string, devices map[string]*apischema.StorageBlockDevice) error {
	// Loop images are implementation detail, not physical storage inventory.
	if raw.Type == "loop" {
		return nil
	}
	if !strings.HasPrefix(raw.Path, "/dev/") || raw.KernelName == "" {
		return fmt.Errorf("block device has no path or kernel name")
	}
	device := devices[raw.Path]
	if device == nil {
		size, err := strconv.ParseUint(raw.Size.String(), 10, 64)
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
	if parent != "" && parent != raw.Path && !slices.Contains(device.Parents, parent) {
		device.Parents = append(device.Parents, parent)
	}
	for _, mount := range raw.Mountpoints {
		if strings.HasPrefix(mount, "/") && !slices.Contains(device.Mountpoints, mount) {
			device.Mountpoints = append(device.Mountpoints, mount)
		}
	}
	for _, child := range raw.Children {
		if err := collectBlockDevice(child, raw.Path, devices); err != nil {
			return err
		}
	}
	return nil
}
