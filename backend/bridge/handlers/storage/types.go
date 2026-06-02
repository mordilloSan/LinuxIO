package storage

// DriveInfo represents a disk returned from lsblk plus best-effort SMART/NVMe
// enrichment while preserving the existing JSON payload shape.
type DriveInfo struct {
	Name       string             `json:"name"`
	Model      string             `json:"model"`
	Serial     string             `json:"serial"`
	Size       string             `json:"size"`
	Type       string             `json:"type"`
	Vendor     string             `json:"vendor"`
	RO         bool               `json:"ro"`
	Smart      map[string]any     `json:"smart,omitempty"`
	SmartError string             `json:"smartError,omitempty"`
	Power      *InferredPowerData `json:"power,omitempty"`
	PowerError string             `json:"powerError,omitempty"`
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
