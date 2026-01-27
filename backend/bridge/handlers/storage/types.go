package storage

// PhysicalVolume represents an LVM physical volume
type PhysicalVolume struct {
	Name       string `json:"name"`       // e.g., /dev/sda1
	VGName     string `json:"vgName"`     // Volume group name
	Size       uint64 `json:"size"`       // Size in bytes
	Free       uint64 `json:"free"`       // Free space in bytes
	Attributes string `json:"attributes"` // PV attributes
	Format     string `json:"format"`     // e.g., "lvm2"
}

// VolumeGroup represents an LVM volume group
type VolumeGroup struct {
	Name       string   `json:"name"`
	Size       uint64   `json:"size"`
	Free       uint64   `json:"free"`
	PVCount    int      `json:"pvCount"`
	LVCount    int      `json:"lvCount"`
	Attributes string   `json:"attributes"`
	PVNames    []string `json:"pvNames"`
}

// LogicalVolume represents an LVM logical volume
type LogicalVolume struct {
	Name       string  `json:"name"`
	VGName     string  `json:"vgName"`
	Size       uint64  `json:"size"`
	Path       string  `json:"path"` // e.g., /dev/vg0/lv0
	Attributes string  `json:"attributes"`
	Mountpoint string  `json:"mountpoint"` // If mounted
	FSType     string  `json:"fsType"`     // Filesystem type
	UsedPct    float64 `json:"usedPct"`    // Usage percentage if mounted
}

// NFSMount represents a mounted NFS share
type NFSMount struct {
	Source     string   `json:"source"`     // server:/path (full source)
	Server     string   `json:"server"`     // NFS server hostname/IP
	ExportPath string   `json:"exportPath"` // Path exported by the server
	Mountpoint string   `json:"mountpoint"` // Local mount point
	FSType     string   `json:"fsType"`     // nfs, nfs4
	Options    []string `json:"options"`    // Mount options
	Size       uint64   `json:"size"`
	Used       uint64   `json:"used"`
	Free       uint64   `json:"free"`
	UsedPct    float64  `json:"usedPct"`
	InFstab    bool     `json:"inFstab"` // Whether this mount is in /etc/fstab
}

// LVM command JSON output structures (for parsing pvs/vgs/lvs --reportformat json)

type pvsReport struct {
	Report []struct {
		PV []struct {
			PVName string `json:"pv_name"`
			VGName string `json:"vg_name"`
			PVSize string `json:"pv_size"`
			PVFree string `json:"pv_free"`
			PVAttr string `json:"pv_attr"`
			PVFmt  string `json:"pv_fmt"`
		} `json:"pv"`
	} `json:"report"`
}

type vgsReport struct {
	Report []struct {
		VG []struct {
			VGName  string `json:"vg_name"`
			VGSize  string `json:"vg_size"`
			VGFree  string `json:"vg_free"`
			PVCount string `json:"pv_count"`
			LVCount string `json:"lv_count"`
			VGAttr  string `json:"vg_attr"`
		} `json:"vg"`
	} `json:"report"`
}

type lvsReport struct {
	Report []struct {
		LV []struct {
			LVName string `json:"lv_name"`
			VGName string `json:"vg_name"`
			LVSize string `json:"lv_size"`
			LVPath string `json:"lv_path"`
			LVAttr string `json:"lv_attr"`
		} `json:"lv"`
	} `json:"report"`
}
