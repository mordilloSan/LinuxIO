package apischema

type CPUInfoResponse struct {
	Cores     int     `json:"cores"`
	Family    string  `json:"family"`
	MHz       float64 `json:"mhz"`
	Model     string  `json:"model"`
	ModelName string  `json:"modelName"`
	VendorID  string  `json:"vendorId"`
}

type GpuDevice struct {
	Address                string   `json:"address"`
	BootVGA                *bool    `json:"boot_vga,omitzero"`
	ClassName              *string  `json:"class_name,omitzero"`
	DeviceID               string   `json:"device_id"`
	Driver                 string   `json:"driver"`
	DriverModule           *string  `json:"driver_module,omitzero"`
	DriverVersion          *string  `json:"driver_version,omitzero"`
	DRMCard                *string  `json:"drm_card,omitzero"`
	BoostFreqMHz           *float64 `json:"boost_freq_mhz,omitzero"`
	GTTTotalBytes          *uint64  `json:"gtt_total_bytes,omitzero"`
	MaxFreqMHz             *float64 `json:"max_freq_mhz,omitzero"`
	MaxLinkSpeed           *string  `json:"max_link_speed,omitzero"`
	MaxLinkWidth           *string  `json:"max_link_width,omitzero"`
	MemoryTotalBytes       *uint64  `json:"memory_total_bytes,omitzero"`
	MinFreqMHz             *float64 `json:"min_freq_mhz,omitzero"`
	Model                  string   `json:"model"`
	NUMANode               *int     `json:"numa_node,omitzero"`
	ProgrammingInterface   *string  `json:"programming_interface,omitzero"`
	RawClass               *string  `json:"raw_class,omitzero"`
	Revision               string   `json:"revision"`
	RP0FreqMHz             *float64 `json:"rp0_freq_mhz,omitzero"`
	RP1FreqMHz             *float64 `json:"rp1_freq_mhz,omitzero"`
	RPNFreqMHz             *float64 `json:"rpn_freq_mhz,omitzero"`
	SubclassName           *string  `json:"subclass_name,omitzero"`
	Subsystem              string   `json:"subsystem"`
	SubsystemID            string   `json:"subsystem_id"`
	Vendor                 string   `json:"vendor"`
	VendorID               string   `json:"vendor_id"`
	VisibleMemoryTotalByte *uint64  `json:"visible_memory_total_bytes,omitzero"`
}

type MotherboardInfo struct {
	Baseboard MotherboardBaseboard `json:"baseboard"`
	BIOS      MotherboardBIOS      `json:"bios"`
}

type MotherboardBaseboard struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
}

type MotherboardBIOS struct {
	Vendor  string `json:"vendor"`
	Version string `json:"version"`
}

type HostInfo struct {
	Hostname        string `json:"hostname"`
	KernelArch      string `json:"kernelArch"`
	KernelVersion   string `json:"kernelVersion"`
	OS              string `json:"os"`
	Platform        string `json:"platform"`
	PlatformVersion string `json:"platformVersion"`
}

type SystemInfo struct {
	BIOSDate       string `json:"biosDate"`
	BIOSVendor     string `json:"biosVendor"`
	BIOSVersion    string `json:"biosVersion"`
	ChassisType    string `json:"chassisType"`
	CPUSummary     string `json:"cpuSummary"`
	ProductName    string `json:"productName"`
	ProductVendor  string `json:"productVendor"`
	ProductVersion string `json:"productVersion"`
}

type SystemLastLogin struct {
	Source   *string `json:"source,omitzero"`
	Terminal *string `json:"terminal,omitzero"`
	Time     string  `json:"time"`
	Username string  `json:"username"`
}

type SystemFailedLoginAlert struct {
	Count         int              `json:"count"`
	ID            string           `json:"id"`
	LatestEvent   AccountUserLogin `json:"latestEvent"`
	LatestEventID string           `json:"latestEventId"`
	Scope         *string          `json:"scope,omitzero"`
	Username      string           `json:"username"`
}

type SystemHealthSummary struct {
	FailedLoginAlert      *SystemFailedLoginAlert `json:"failedLoginAlert,omitzero"`
	FailedServices        []string                `json:"failedServices,omitempty"`
	FailedServicesCount   int                     `json:"failedServicesCount"`
	LastLogin             *SystemLastLogin        `json:"lastLogin,omitzero"`
	RunningServicesCount  int                     `json:"runningServicesCount"`
	UncleanShutdown       bool                    `json:"uncleanShutdown"`
	UncleanShutdownBootID *string                 `json:"uncleanShutdownBootId,omitzero"`
	UpdatesAvailable      int                     `json:"updatesAvailable"`
	UpToDate              bool                    `json:"upToDate"`
}

type PCIDevice struct {
	Class  string `json:"class"`
	Model  string `json:"model"`
	Slot   string `json:"slot"`
	Vendor string `json:"vendor"`
}

type MemoryModule struct {
	ID         string `json:"id"`
	Rank       string `json:"rank"`
	Size       string `json:"size"`
	Speed      string `json:"speed"`
	State      string `json:"state"`
	Technology string `json:"technology"`
	Type       string `json:"type"`
}

type DistroInfo struct {
	Codename string `json:"codename"`
	Logo     string `json:"logo"`
	Name     string `json:"name"`
	Version  string `json:"version"`
}

type AlertIDRequest struct {
	AlertID string `json:"alertId"`
}

type BootIDRequest struct {
	BootID string `json:"bootId"`
}

type FailedLoginEventsRequest struct {
	Limit *string `json:"limit,omitzero"`
}
