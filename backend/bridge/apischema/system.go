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
	BootVGA                *bool    `json:"boot_vga,omitempty"`
	ClassName              *string  `json:"class_name,omitempty"`
	DeviceID               string   `json:"device_id"`
	Driver                 string   `json:"driver"`
	DriverModule           *string  `json:"driver_module,omitempty"`
	DriverVersion          *string  `json:"driver_version,omitempty"`
	DRMCard                *string  `json:"drm_card,omitempty"`
	BoostFreqMHz           *float64 `json:"boost_freq_mhz,omitempty"`
	GTTTotalBytes          *uint64  `json:"gtt_total_bytes,omitempty"`
	MaxFreqMHz             *float64 `json:"max_freq_mhz,omitempty"`
	MaxLinkSpeed           *string  `json:"max_link_speed,omitempty"`
	MaxLinkWidth           *string  `json:"max_link_width,omitempty"`
	MemoryTotalBytes       *uint64  `json:"memory_total_bytes,omitempty"`
	MinFreqMHz             *float64 `json:"min_freq_mhz,omitempty"`
	Model                  string   `json:"model"`
	NUMANode               *int     `json:"numa_node,omitempty"`
	ProgrammingInterface   *string  `json:"programming_interface,omitempty"`
	RawClass               *string  `json:"raw_class,omitempty"`
	Revision               string   `json:"revision"`
	RP0FreqMHz             *float64 `json:"rp0_freq_mhz,omitempty"`
	RP1FreqMHz             *float64 `json:"rp1_freq_mhz,omitempty"`
	RPNFreqMHz             *float64 `json:"rpn_freq_mhz,omitempty"`
	SubclassName           *string  `json:"subclass_name,omitempty"`
	Subsystem              string   `json:"subsystem"`
	SubsystemID            string   `json:"subsystem_id"`
	Vendor                 string   `json:"vendor"`
	VendorID               string   `json:"vendor_id"`
	VisibleMemoryTotalByte *uint64  `json:"visible_memory_total_bytes,omitempty"`
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
	Source   *string `json:"source,omitempty"`
	Terminal *string `json:"terminal,omitempty"`
	Time     string  `json:"time"`
	Username string  `json:"username"`
}

type SystemFailedLoginAlert struct {
	Count         int              `json:"count"`
	ID            string           `json:"id"`
	LatestEvent   AccountUserLogin `json:"latestEvent"`
	LatestEventID string           `json:"latestEventId"`
	Scope         *string          `json:"scope,omitempty"`
	Username      string           `json:"username"`
}

type SystemHealthSummary struct {
	FailedLoginAlert      *SystemFailedLoginAlert `json:"failedLoginAlert,omitempty"`
	FailedServices        []string                `json:"failedServices,omitempty"`
	FailedServicesCount   int                     `json:"failedServicesCount"`
	LastLogin             *SystemLastLogin        `json:"lastLogin,omitempty"`
	RunningServicesCount  int                     `json:"runningServicesCount"`
	UncleanShutdown       bool                    `json:"uncleanShutdown"`
	UncleanShutdownBootID *string                 `json:"uncleanShutdownBootId,omitempty"`
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
	Limit *string `json:"limit,omitempty"`
}
