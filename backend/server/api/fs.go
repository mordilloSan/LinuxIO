package api

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/disk"
)

// ---------- Types ----------

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

// ---------- Fetcher ----------

// FetchFileSystemInfo lists mounted filesystems. If includeAll is false, it filters out
// common pseudo/virtual filesystems (proc, sysfs, tmpfs, cgroup, etc.).
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
			// Skip mounts we can't query (permission or transient errors)
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
	// Fast path: allow real block devices
	if strings.HasPrefix(p.Device, "/dev/") {
		return false
	}

	// Many pseudo filesystems are not backed by /dev/*
	// Keep a conservative deny-list; expand as needed.
	switch p.Fstype {
	case "proc", "sysfs", "devtmpfs", "devpts",
		"tmpfs", "cgroup", "cgroup2", "pstore",
		"securityfs", "debugfs", "tracefs",
		"configfs", "overlay", "squashfs", "ramfs",
		"bpf", "nsfs", "autofs", "fusectl":
		return true
	}

	// Allow known “real” FS types even if device isn’t /dev/*
	switch p.Fstype {
	case "ext2", "ext3", "ext4", "xfs", "btrfs", "zfs",
		"f2fs", "reiserfs", "jfs", "ntfs", "vfat", "exfat":
		return false
	}

	// Default: treat as real (don’t hide unexpectedly)
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

// ---------- Handler ----------

func getFileSystem(c *gin.Context) {
	// Query param: ?all=1 to include pseudo filesystems
	all := strings.EqualFold(c.Query("all"), "1") ||
		strings.EqualFold(c.Query("all"), "true") ||
		strings.EqualFold(c.Query("all"), "yes")

	data, err := FetchFileSystemInfo(all)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get filesystem info", "details": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}
