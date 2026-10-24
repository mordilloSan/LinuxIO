package apischema

import (
	"github.com/mordilloSan/LinuxIO/backend/common/btrfs"
)

type ApiDisk struct {
	Model string `json:"model"`
	Name  string `json:"name"`
	RO    bool   `json:"ro"`
	// Rotational is true for spinning disks, false for SSD and NVMe.
	Rotational bool    `json:"rotational"`
	Serial     *string `json:"serial,omitzero"`
	Size       string  `json:"size"`
	Type       *string `json:"type,omitzero"`
	Vendor     *string `json:"vendor,omitzero"`
}

// StorageBlockDevice describes kernel-reported backing relationships. A device
// can have several parents (RAID, multipath, or an LV spanning several PVs).
type StorageBlockDevice struct {
	Path        string   `json:"path"`
	Name        string   `json:"name"`
	KernelName  string   `json:"kernelName"`
	Type        string   `json:"type"`
	SizeBytes   uint64   `json:"sizeBytes"`
	FSType      string   `json:"fsType"`
	Mountpoints []string `json:"mountpoints"`
	Parents     []string `json:"parents"`
	Model       string   `json:"model"`
	Serial      string   `json:"serial"`
	Transport   string   `json:"transport"`
	ReadOnly    bool     `json:"readOnly"`
}

type StorageMount struct {
	Device   string `json:"device"`
	Path     string `json:"path"`
	FSType   string `json:"fsType"`
	ReadOnly bool   `json:"readOnly"`
}

type StorageTopologyInventory struct {
	Devices []StorageBlockDevice `json:"devices"`
	Mounts  []StorageMount       `json:"mounts"`
}

type PhysicalVolume struct {
	Attributes string `json:"attributes"`
	Format     string `json:"format"`
	Free       uint64 `json:"free"`
	Name       string `json:"name"`
	Size       uint64 `json:"size"`
	VGName     string `json:"vgName"`
}

type VolumeGroup struct {
	Attributes string   `json:"attributes"`
	Free       uint64   `json:"free"`
	LVCount    int      `json:"lvCount"`
	Name       string   `json:"name"`
	PVCount    int      `json:"pvCount"`
	PVNames    []string `json:"pvNames"`
	Size       uint64   `json:"size"`
}

type LogicalVolume struct {
	Attributes string  `json:"attributes"`
	FSType     string  `json:"fsType"`
	Mountpoint string  `json:"mountpoint"`
	Name       string  `json:"name"`
	Path       string  `json:"path"`
	Size       uint64  `json:"size"`
	UsedPct    float64 `json:"usedPct"`
	VGName     string  `json:"vgName"`
}

type NFSMount struct {
	ExportPath string   `json:"exportPath"`
	Free       uint64   `json:"free"`
	FSType     string   `json:"fsType"`
	InFstab    bool     `json:"inFstab"`
	Mounted    bool     `json:"mounted"`
	Mountpoint string   `json:"mountpoint"`
	Options    []string `json:"options"`
	Server     string   `json:"server"`
	Size       uint64   `json:"size"`
	Source     string   `json:"source"`
	Used       uint64   `json:"used"`
	UsedPct    float64  `json:"usedPct"`
}

// CIFSMount is a mounted (or persisted, fstab-backed) remote SMB/CIFS share. It
// never carries a password; Username/Domain are display metadata only.
type CIFSMount struct {
	Domain     string   `json:"domain"`
	Free       uint64   `json:"free"`
	FSType     string   `json:"fsType"`
	InFstab    bool     `json:"inFstab"`
	Mounted    bool     `json:"mounted"`
	Mountpoint string   `json:"mountpoint"`
	Options    []string `json:"options"`
	Server     string   `json:"server"`
	Share      string   `json:"share"`
	Size       uint64   `json:"size"`
	Source     string   `json:"source"`
	Used       uint64   `json:"used"`
	UsedPct    float64  `json:"usedPct"`
	Username   string   `json:"username"`
}

type StorageMountResult struct {
	Success    bool    `json:"success"`
	Mountpoint *string `json:"mountpoint,omitzero"`
	Warning    *string `json:"warning,omitzero"`
}

// BtrfsMaintenanceTask is one btrfsmaintenance timer (scrub, balance, trim,
// defrag): its shared period, the periods it accepts, and the mountpoints it
// targets. A task with period "none" targets nothing.
type BtrfsMaintenanceTask struct {
	Task        string   `json:"task"`
	Period      string   `json:"period"`
	Periods     []string `json:"periods"`
	Mountpoints []string `json:"mountpoints"`
}

type BtrfsMaintenanceSchedule struct {
	Tasks []BtrfsMaintenanceTask `json:"tasks"`
}

type StorageWarningResult struct {
	Success bool    `json:"success"`
	Warning *string `json:"warning,omitzero"`
}

type ServerRequest struct {
	Server string `json:"server"`
}

type ServerExportMountOptionsPersistRequest struct {
	Server     string   `json:"server"`
	ExportPath string   `json:"exportPath"`
	Mountpoint string   `json:"mountpoint"`
	Options    []string `json:"options"`
	Persist    string   `json:"persist"`
}

// CIFSMountRequest carries SMB mount input. Password is written only to the
// root-only credentials file referenced by fstab; it is never echoed back in
// any model. All CIFS mounts are persistent (fstab-backed).
type CIFSMountRequest struct {
	Server     string   `json:"server"`
	Share      string   `json:"share"`
	Mountpoint string   `json:"mountpoint"`
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	Domain     string   `json:"domain"`
	Options    []string `json:"options"`
}

type MountpointOptionsUpdateFstabRequest struct {
	Mountpoint  string   `json:"mountpoint"`
	Options     []string `json:"options"`
	UpdateFstab string   `json:"updateFstab"`
}

type MountpointRequest struct {
	Mountpoint string `json:"mountpoint"`
}

type UUIDRequest struct {
	UUID string `json:"uuid"`
}

type BtrfsSubvolumeRequest struct {
	UUID string `json:"uuid"`
	Path string `json:"path"`
}

type BtrfsSnapshotsRequest struct {
	UUID string  `json:"uuid"`
	Path *string `json:"path,omitzero"`
}

type BtrfsSnapshotRequest struct {
	UUID         string `json:"uuid"`
	SnapshotPath string `json:"snapshotPath"`
}

type BtrfsSnapshotRuleRequest struct {
	Rule btrfs.BtrfsSnapshotRule `json:"rule"`
}

type BtrfsReplicationJobRequest struct {
	Job btrfs.BtrfsReplicationJob `json:"job"`
}

type BtrfsJobIDRequest struct {
	ID string `json:"id"`
}

// BtrfsSSHTrustRequest scans a host; with Confirm it trusts only the keys
// matching Fingerprints, the ones shown to the user by the unconfirmed scan.
type BtrfsSSHTrustRequest struct {
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	Confirm      bool     `json:"confirm"`
	Fingerprints []string `json:"fingerprints,omitempty"`
}

type BtrfsSubvolumeReadOnlyRequest struct {
	UUID     string `json:"uuid"`
	Path     string `json:"path"`
	ReadOnly bool   `json:"readOnly"`
}

type BtrfsQuotaRequest struct {
	UUID    string `json:"uuid"`
	Enabled bool   `json:"enabled"`
}

// BtrfsQgroupLimitRequest limits the referenced size of one subvolume; nil
// removes the limit.
type BtrfsQgroupLimitRequest struct {
	UUID               string  `json:"uuid"`
	Path               string  `json:"path"`
	MaxReferencedBytes *uint64 `json:"maxReferencedBytes"`
}

// BtrfsCompressionRequest sets the filesystem-wide compression option and
// persists it in every fstab entry of the filesystem.
type BtrfsCompressionRequest struct {
	UUID      string `json:"uuid"`
	Algorithm string `json:"algorithm"`
	Level     *int   `json:"level,omitzero"`
	Force     bool   `json:"force"`
}

type BtrfsSubvolumeCompressionRequest struct {
	UUID      string `json:"uuid"`
	Path      string `json:"path"`
	Algorithm string `json:"algorithm"`
}

type BtrfsDeviceAddRequest struct {
	UUID   string `json:"uuid"`
	Device string `json:"device"`
	Force  bool   `json:"force"`
}

type BtrfsDeviceRequest struct {
	UUID     string `json:"uuid"`
	DeviceID int    `json:"deviceId"`
}

type BtrfsDeviceReplaceRequest struct {
	UUID      string `json:"uuid"`
	DeviceID  int    `json:"deviceId"`
	NewDevice string `json:"newDevice"`
}

// BtrfsBalanceRequest converts to the given profiles; a nil profile is left
// as it is. DataUsage limits the balance to data chunks used at most that
// percent, which reclaims unallocated space. With nothing set everything is
// balanced.
type BtrfsBalanceRequest struct {
	UUID            string  `json:"uuid"`
	DataProfile     *string `json:"dataProfile,omitzero"`
	MetadataProfile *string `json:"metadataProfile,omitzero"`
	DataUsage       *int    `json:"dataUsage,omitzero"`
}

type BtrfsLabelRequest struct {
	UUID  string `json:"uuid"`
	Label string `json:"label"`
}

// BtrfsPoolCreateRequest formats Devices as one btrfs filesystem, mounts it
// at Mountpoint and records it in /etc/fstab. Force overwrites foreign
// filesystem signatures on the devices. Partition first gives each disk a
// GPT with one partition that leaves room for a slightly smaller
// replacement disk, and builds the pool from the partitions.
type BtrfsPoolCreateRequest struct {
	Label           string   `json:"label"`
	Mountpoint      string   `json:"mountpoint"`
	DataProfile     string   `json:"dataProfile"`
	MetadataProfile string   `json:"metadataProfile"`
	Devices         []string `json:"devices"`
	Force           bool     `json:"force"`
	Partition       bool     `json:"partition"`
}

// BtrfsPoolMountRequest mounts an unmounted pool at its fstab mountpoint, or
// at /mnt/<label> which is then recorded in /etc/fstab; Degraded allows a
// pool with missing devices.
type BtrfsPoolMountRequest struct {
	UUID     string `json:"uuid"`
	Degraded bool   `json:"degraded"`
}

type MountpointRemoveFstabRequest struct {
	Mountpoint  string `json:"mountpoint"`
	RemoveFstab string `json:"removeFstab"`
}

type VolumeGroupLogicalVolumeRequest struct {
	VGName string `json:"vgName"`
	LVName string `json:"lvName"`
}

type CreateLogicalVolumeRequest struct {
	VGName string `json:"vgName"`
	LVName string `json:"lvName"`
	Size   string `json:"size"`
}

type ResizeLogicalVolumeRequest struct {
	VGName  string `json:"vgName"`
	LVName  string `json:"lvName"`
	NewSize string `json:"newSize"`
}

type DeviceTestTypeRequest struct {
	Device   string `json:"device"`
	TestType string `json:"testType"`
}

// BtrfsMaintenanceRequest sets one btrfsmaintenance task's period for one
// btrfs mountpoint; "none" removes the mountpoint from that task.
type BtrfsMaintenanceRequest struct {
	Mountpoint string `json:"mountpoint"`
	Task       string `json:"task"`
	Period     string `json:"period"`
}
