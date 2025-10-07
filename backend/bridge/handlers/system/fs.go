package system

import (
	"strings"

	"github.com/shirou/gopsutil/v4/disk"
)

type FilesystemMount struct {
	Device            string  `json:"device"`
	Mountpoint        string  `json:"mountpoint"`
	FSType            string  `json:"fstype"`
	ReadOnly          bool    `json:"readOnly"`
	Total             uint64  `json:"total"`
	Used              uint64  `json:"used"`
	Free              uint64  `json:"free"`
	UsedPercent       float64 `json:"usedPercent"`
	InodesTotal       uint64  `json:"inodesTotal,omitempty"`
	InodesUsed        uint64  `json:"inodesUsed,omitempty"`
	InodesFree        uint64  `json:"inodesFree,omitempty"`
	InodesUsedPercent float64 `json:"inodesUsedPercent,omitempty"`
}

func FetchFileSystemInfo(includeAll bool) ([]FilesystemMount, error) {
	parts, err := disk.Partitions(true)
	if err != nil {
		return nil, err
	}
	results := make([]FilesystemMount, 0, len(parts))
	for _, p := range parts {
		if !includeAll && isPseudoFS(p) {
			continue
		}
		usage, err := disk.Usage(p.Mountpoint)
		if err != nil {
			continue
		}
		results = append(results, FilesystemMount{
			Device:            p.Device,
			Mountpoint:        p.Mountpoint,
			FSType:            p.Fstype,
			ReadOnly:          hasReadOnlyOpt(p.Opts),
			Total:             usage.Total,
			Used:              usage.Used,
			Free:              usage.Free,
			UsedPercent:       usage.UsedPercent,
			InodesTotal:       usage.InodesTotal,
			InodesUsed:        usage.InodesUsed,
			InodesFree:        usage.InodesFree,
			InodesUsedPercent: usage.InodesUsedPercent,
		})
	}
	return results, nil
}

func isPseudoFS(p disk.PartitionStat) bool {
	if strings.HasPrefix(p.Device, "/dev/") {
		return false
	}
	switch p.Fstype {
	case "proc", "sysfs", "devtmpfs", "devpts",
		"tmpfs", "cgroup", "cgroup2", "pstore",
		"securityfs", "debugfs", "tracefs",
		"configfs", "overlay", "squashfs", "ramfs",
		"bpf", "nsfs", "autofs", "fusectl":
		return true
	}
	switch p.Fstype {
	case "ext2", "ext3", "ext4", "xfs", "btrfs", "zfs",
		"f2fs", "reiserfs", "jfs", "ntfs", "vfat", "exfat":
		return false
	}
	return false
}

func hasReadOnlyOpt(opts []string) bool {
	for _, o := range opts {
		if strings.TrimSpace(o) == "ro" {
			return true
		}
	}
	return false
}
