package system

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shirou/gopsutil/v4/disk"
)

// --------- Public JSON types ---------

type Drive struct {
	Name       string      `json:"name"`       // e.g., "nvme0n1"
	Path       string      `json:"path"`       // e.g., "/dev/nvme0n1"
	Model      string      `json:"model"`      // e.g., "Samsung SSD 970 EVO"
	Serial     string      `json:"serial"`     // may be empty (permission- or driver-dependent)
	Vendor     string      `json:"vendor"`     // may be empty
	Transport  string      `json:"transport"`  // "nvme" | "sata" | "sas" | "virtio" | ...
	Type       string      `json:"type"`       // "disk" | "rom" | "loop" | ...
	Rotational bool        `json:"rotational"` // true for HDD, false for SSD/NVMe
	Removable  bool        `json:"removable"`  // USB sticks, etc.
	SizeBytes  uint64      `json:"sizeBytes"`
	ReadBytes  uint64      `json:"readBytes"`  // from gopsutil (cumulative)
	WriteBytes uint64      `json:"writeBytes"` // from gopsutil (cumulative)
	Children   []Partition `json:"children"`   // partitions / dm devices
}

type Partition struct {
	Name       string           `json:"name"`       // e.g., "nvme0n1p1"
	Path       string           `json:"path"`       // e.g., "/dev/nvme0n1p1"
	FSType     string           `json:"fstype"`     // "ext4", "xfs", "btrfs", ...
	Label      string           `json:"label"`      // filesystem label
	UUID       string           `json:"uuid"`       // filesystem UUID
	Mountpoint string           `json:"mountpoint"` // "" if not mounted
	SizeBytes  uint64           `json:"sizeBytes"`
	Usage      *FilesystemUsage `json:"usage,omitempty"` // only when mounted and readable
}

type FilesystemUsage struct {
	Total       uint64  `json:"total"`
	Used        uint64  `json:"used"`
	Free        uint64  `json:"free"`
	UsedPercent float64 `json:"usedPercent"`
}

// --------- Handler ---------

// GET /system/disk
//   - default: hides loop/rom devices
//   - add ?all=1 (or true/yes) to include all devices
func getDisks(c *gin.Context) {
	includeAll := qBool(c.Query("all"))

	list, err := FetchDriveInfo(c.Request.Context(), includeAll)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "failed to get drive info",
			"details": err.Error(),
		})
		return
	}
	c.JSON(http.StatusOK, list)
}

func qBool(v string) bool {
	v = strings.TrimSpace(strings.ToLower(v))
	return v == "1" || v == "true" || v == "yes"
}

// --------- Fetcher (lsblk + gopsutil enrich) ---------

func FetchDriveInfo(ctx context.Context, includeAll bool) ([]Drive, error) {
	// Ensure we don’t hang on slow systems
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
	}

	// 1) Enumerate block devices with lsblk JSON (bytes)
	//    -J JSON, -O all props, -b sizes in bytes
	out, err := exec.CommandContext(ctx, "lsblk", "-J", "-O", "-b").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, errors.New("lsblk returned an error (is util-linux installed?)")
		}
		return nil, err
	}

	var tree lsblkJSON
	if err := json.Unmarshal(out, &tree); err != nil {
		return nil, err
	}

	// 2) Prepare IO counters for enrichment
	ioCounters, _ := disk.IOCounters() // key is device name ("sda", "nvme0n1", ...)

	// 3) Convert tree into our public API model
	var drives []Drive
	for _, bd := range tree.Blockdevices {
		if !includeAll && (bd.Type == "loop" || bd.Type == "rom") {
			continue
		}
		if bd.Name == "" || bd.Path == "" {
			// skip weird phantom entries
			continue
		}

		d := Drive{
			Name:       bd.Name,
			Path:       bd.Path,
			Model:      or(bd.Model, bd.Vendor), // model can be empty; fallback to vendor
			Serial:     bd.Serial,
			Vendor:     bd.Vendor,
			Transport:  bd.Tran,
			Type:       bd.Type,
			Rotational: bd.Rota,
			Removable:  bd.Rm,
			SizeBytes:  uint64(max64(bd.Size, 0)),
		}

		// Enrich with IO counters if available
		if io, ok := ioCounters[d.Name]; ok {
			d.ReadBytes = io.ReadBytes
			d.WriteBytes = io.WriteBytes
		}

		// Attach children (partitions / LVs)
		for _, ch := range bd.Children {
			p := Partition{
				Name:       ch.Name,
				Path:       ch.Path,
				FSType:     ch.FsType,
				Label:      ch.Label,
				UUID:       ch.UUID,
				Mountpoint: firstNonEmpty(ch.Mountpoint, ch.MountPoints...),
				SizeBytes:  uint64(max64(ch.Size, 0)),
			}
			// Filesystem usage if mounted
			if p.Mountpoint != "" {
				if u, err := disk.Usage(p.Mountpoint); err == nil {
					p.Usage = &FilesystemUsage{
						Total:       u.Total,
						Used:        u.Used,
						Free:        u.Free,
						UsedPercent: u.UsedPercent,
					}
				}
			}
			d.Children = append(d.Children, p)
		}

		drives = append(drives, d)
	}

	return drives, nil
}

// --------- lsblk JSON schema (subset we care about) ---------

type lsblkJSON struct {
	Blockdevices []lsblkDevice `json:"blockdevices"`
}

type lsblkDevice struct {
	Name        string        `json:"name"`  // "nvme0n1"
	Kname       string        `json:"kname"` // kernel name
	Path        string        `json:"path"`  // "/dev/nvme0n1"
	Type        string        `json:"type"`  // "disk" | "part" | "rom" | "loop" | "lvm" | ...
	Tran        string        `json:"tran"`  // "nvme" | "sata" | "sas" | "virtio" | "usb" | ...
	Vendor      string        `json:"vendor"`
	Model       string        `json:"model"`
	Serial      string        `json:"serial"`
	Rota        bool          `json:"rota"` // rotational
	Rm          bool          `json:"rm"`   // removable
	Size        int64         `json:"size"` // bytes
	FsType      string        `json:"fstype"`
	Label       string        `json:"label"`
	UUID        string        `json:"uuid"`
	Mountpoint  string        `json:"mountpoint"`  // single mountpoint (deprecated in newer lsblk)
	MountPoints []string      `json:"mountpoints"` // array form
	Children    []lsblkDevice `json:"children"`    // partitions / mapped devices
}

// --------- helpers ---------

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func firstNonEmpty(a string, rest ...string) string {
	if a != "" {
		return a
	}
	for _, s := range rest {
		if s != "" {
			return s
		}
	}
	return ""
}

func max64(n int64, floor int64) int64 {
	if n < floor {
		return floor
	}
	return n
}
