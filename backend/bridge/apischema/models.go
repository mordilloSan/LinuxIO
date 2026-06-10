package apischema

import (
	"github.com/mordilloSan/LinuxIO/backend/common/session"
	"github.com/shirou/gopsutil/v4/load"
)

type AutoUpdateFrequency string
type AutoUpdateScope string
type AutoUpdateRebootPolicy string
type JobState string
type SensorReadingKind string
type TableCardViewMode string
type Theme string
type ValidationIssueType string

var StringEnums = map[string][]string{
	"AutoUpdateFrequency":    {"hourly", "daily", "weekly"},
	"AutoUpdateScope":        {"security", "updates", "all"},
	"AutoUpdateRebootPolicy": {"never", "if_needed", "always", "schedule"},
	"JobState":               {"queued", "running", "completed", "failed", "canceled"},
	"SensorReadingKind":      {"number", "boolean"},
	"TableCardViewMode":      {"card", "table"},
	"Theme":                  {"LIGHT", "DARK"},
	"ValidationIssueType":    {"error", "warning"},
}

const (
	SensorReadingKindNumber  SensorReadingKind = "number"
	SensorReadingKindBoolean SensorReadingKind = "boolean"
)

var ExtraTypes = []TypeSpec{
	TypeOf[InstallCapabilityResult](),
	TypeOf[JobEvent](),
}

type CPUInfoResponse struct {
	Cores              int                `json:"cores"`
	CurrentFrequencies []float64          `json:"currentFrequencies"`
	Family             string             `json:"family"`
	LoadAverage        *load.AvgStat      `json:"loadAverage,omitempty"`
	MHz                float64            `json:"mhz"`
	Model              string             `json:"model"`
	ModelName          string             `json:"modelName"`
	PerCoreUsage       []float64          `json:"perCoreUsage"`
	Temperature        map[string]float64 `json:"temperature"`
	VendorID           string             `json:"vendorId"`
}

type MemoryInfoResponse struct {
	Docker MemoryDockerInfo `json:"docker"`
	System MemorySystemInfo `json:"system"`
	ZFS    MemoryZFSInfo    `json:"zfs"`
}

type MemoryDockerInfo struct {
	Used uint64 `json:"used"`
}

type MemorySystemInfo struct {
	Total     uint64 `json:"total"`
	Active    uint64 `json:"active"`
	SwapTotal uint64 `json:"swapTotal"`
	SwapFree  uint64 `json:"swapFree"`
}

type MemoryZFSInfo struct {
	ARC uint64 `json:"arc"`
}

type GpuDevice struct {
	ActualFreqMHz          *float64 `json:"actual_freq_mhz,omitempty"`
	Address                string   `json:"address"`
	BoostFreqMHz           *float64 `json:"boost_freq_mhz,omitempty"`
	BootVGA                *bool    `json:"boot_vga,omitempty"`
	ClassName              *string  `json:"class_name,omitempty"`
	ConnectedDisplays      *int     `json:"connected_displays,omitempty"`
	CurrentFreqMHz         *float64 `json:"current_freq_mhz,omitempty"`
	DeviceID               string   `json:"device_id"`
	DisplayNames           []string `json:"display_names,omitempty"`
	Driver                 string   `json:"driver"`
	DriverModule           *string  `json:"driver_module,omitempty"`
	DriverVersion          *string  `json:"driver_version,omitempty"`
	DRMCard                *string  `json:"drm_card,omitempty"`
	FanPercent             *float64 `json:"fan_percent,omitempty"`
	FanRPM                 *float64 `json:"fan_rpm,omitempty"`
	GTTTotalBytes          *uint64  `json:"gtt_total_bytes,omitempty"`
	GTTUsedBytes           *uint64  `json:"gtt_used_bytes,omitempty"`
	LinkSpeed              *string  `json:"link_speed,omitempty"`
	LinkWidth              *string  `json:"link_width,omitempty"`
	MaxFreqMHz             *float64 `json:"max_freq_mhz,omitempty"`
	MaxLinkSpeed           *string  `json:"max_link_speed,omitempty"`
	MaxLinkWidth           *string  `json:"max_link_width,omitempty"`
	MemoryFreeBytes        *uint64  `json:"memory_free_bytes,omitempty"`
	MemoryTotalBytes       *uint64  `json:"memory_total_bytes,omitempty"`
	MemoryUsedBytes        *uint64  `json:"memory_used_bytes,omitempty"`
	MinFreqMHz             *float64 `json:"min_freq_mhz,omitempty"`
	Model                  string   `json:"model"`
	NUMANode               *int     `json:"numa_node,omitempty"`
	PowerDrawWatts         *float64 `json:"power_draw_watts,omitempty"`
	PowerLimitWatts        *float64 `json:"power_limit_watts,omitempty"`
	PowerState             *string  `json:"power_state,omitempty"`
	ProgrammingInterface   *string  `json:"programming_interface,omitempty"`
	RawClass               *string  `json:"raw_class,omitempty"`
	RC6ResidencyMS         *float64 `json:"rc6_residency_ms,omitempty"`
	RequestedFreqMHz       *float64 `json:"requested_freq_mhz,omitempty"`
	Revision               string   `json:"revision"`
	RP0FreqMHz             *float64 `json:"rp0_freq_mhz,omitempty"`
	RP1FreqMHz             *float64 `json:"rp1_freq_mhz,omitempty"`
	RPNFreqMHz             *float64 `json:"rpn_freq_mhz,omitempty"`
	RuntimeStatus          *string  `json:"runtime_status,omitempty"`
	SubclassName           *string  `json:"subclass_name,omitempty"`
	Subsystem              string   `json:"subsystem"`
	SubsystemID            string   `json:"subsystem_id"`
	TemperatureC           *float64 `json:"temperature_c,omitempty"`
	UtilizationPercent     *float64 `json:"utilization_percent,omitempty"`
	Vendor                 string   `json:"vendor"`
	VendorID               string   `json:"vendor_id"`
	VisibleMemoryTotalByte *uint64  `json:"visible_memory_total_bytes,omitempty"`
	VisibleMemoryUsedBytes *uint64  `json:"visible_memory_used_bytes,omitempty"`
}

type SensorReading struct {
	Field string            `json:"-"`
	Kind  SensorReadingKind `json:"kind"`
	Label string            `json:"label"`
	Unit  string            `json:"unit"`
	Value any               `json:"value"`
}

type SensorGroup struct {
	Adapter  string          `json:"adapter"`
	Readings []SensorReading `json:"readings"`
}

type ApiDisk struct {
	Model  string  `json:"model"`
	Name   string  `json:"name"`
	Power  any     `json:"power,omitempty"`
	RO     bool    `json:"ro"`
	Serial *string `json:"serial,omitempty"`
	Size   string  `json:"size"`
	Smart  any     `json:"smart,omitempty"`
	Type   *string `json:"type,omitempty"`
	Vendor *string `json:"vendor,omitempty"`
}

type MotherboardInfo struct {
	Baseboard    MotherboardBaseboard     `json:"baseboard"`
	BIOS         MotherboardBIOS          `json:"bios"`
	Temperatures *MotherboardTemperatures `json:"temperatures,omitempty"`
}

type MotherboardBaseboard struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
}

type MotherboardBIOS struct {
	Vendor  string `json:"vendor"`
	Version string `json:"version"`
}

type MotherboardTemperatures struct {
	Sensors map[string]float64 `json:"sensors"`
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

type ProcessInfo struct {
	Running bool `json:"running"`
}

type InterfaceStats struct {
	IPv4    []string `json:"ipv4"`
	MAC     string   `json:"mac"`
	Name    string   `json:"name"`
	RXSpeed float64  `json:"rx_speed"`
	Speed   string   `json:"speed"`
	TXSpeed float64  `json:"tx_speed"`
}

type DiskThroughputDevice struct {
	Name             string  `json:"name"`
	ReadBytesPerSec  float64 `json:"readBytesPerSec"`
	ReadOpsPerSec    float64 `json:"readOpsPerSec"`
	WriteBytesPerSec float64 `json:"writeBytesPerSec"`
	WriteOpsPerSec   float64 `json:"writeOpsPerSec"`
}

type DiskThroughputResponse struct {
	Devices          []DiskThroughputDevice `json:"devices"`
	IntervalSeconds  float64                `json:"intervalSeconds"`
	ReadBytesPerSec  float64                `json:"readBytesPerSec"`
	ReadOpsPerSec    float64                `json:"readOpsPerSec"`
	WriteBytesPerSec float64                `json:"writeBytesPerSec"`
	WriteOpsPerSec   float64                `json:"writeOpsPerSec"`
}

type NetworkInterface struct {
	DNS        []string `json:"dns"`
	Duplex     string   `json:"duplex"`
	Gateway    string   `json:"gateway"`
	IPv4       []string `json:"ipv4"`
	IPv4Method *string  `json:"ipv4_method,omitempty"`
	IPv6       []string `json:"ipv6"`
	MAC        string   `json:"mac"`
	MTU        int      `json:"mtu"`
	Name       string   `json:"name"`
	RXSpeed    float64  `json:"rx_speed"`
	Speed      string   `json:"speed"`
	State      int      `json:"state"`
	TXSpeed    float64  `json:"tx_speed"`
	Type       string   `json:"type"`
}

type FilesystemInfo struct {
	Device            string   `json:"device"`
	Free              uint64   `json:"free"`
	FSType            string   `json:"fstype"`
	InodesFree        *uint64  `json:"inodesFree,omitempty"`
	InodesTotal       *uint64  `json:"inodesTotal,omitempty"`
	InodesUsed        *uint64  `json:"inodesUsed,omitempty"`
	InodesUsedPercent *float64 `json:"inodesUsedPercent,omitempty"`
	Mountpoint        string   `json:"mountpoint"`
	ReadOnly          *bool    `json:"readOnly,omitempty"`
	Total             uint64   `json:"total"`
	Used              uint64   `json:"used"`
	UsedPercent       float64  `json:"usedPercent"`
}

type ResourceStatData struct {
	Group       string `json:"group"`
	Mode        string `json:"mode"`
	Modified    string `json:"modified"`
	Name        string `json:"name"`
	Owner       string `json:"owner"`
	Path        string `json:"path"`
	Permissions string `json:"permissions"`
	Raw         string `json:"raw"`
	RealPath    string `json:"realPath"`
	Size        int64  `json:"size"`
}

type ContainerPort struct {
	IP          *string `json:"IP,omitempty"`
	PrivatePort int     `json:"PrivatePort"`
	PublicPort  *int    `json:"PublicPort,omitempty"`
	Type        string  `json:"Type"`
}

type ContainerMount struct {
	Destination string `json:"Destination"`
	Mode        string `json:"Mode"`
	RW          bool   `json:"RW"`
	Source      string `json:"Source"`
	Type        string `json:"Type"`
}

type ContainerEndpoint struct {
	Gateway           string  `json:"Gateway"`
	GlobalIPv6Address *string `json:"GlobalIPv6Address,omitempty"`
	IPAddress         string  `json:"IPAddress"`
	MACAddress        *string `json:"MacAddress,omitempty"`
}

type ContainerMetrics struct {
	CPUPercent float64 `json:"cpu_percent"`
	MemUsage   uint64  `json:"mem_usage"`
	MemLimit   uint64  `json:"mem_limit"`
	NetInput   uint64  `json:"net_input"`
	NetOutput  uint64  `json:"net_output"`
	BlockRead  uint64  `json:"block_read"`
	BlockWrite uint64  `json:"block_write"`
}

type ContainerHostConfig struct {
	NetworkMode *string `json:"NetworkMode,omitempty"`
}

type ContainerNetworkSettings struct {
	Networks map[string]ContainerEndpoint `json:"Networks,omitempty"`
}

type ContainerInfo struct {
	Created         int64                     `json:"Created"`
	HostConfig      *ContainerHostConfig      `json:"HostConfig,omitempty"`
	Icon            *string                   `json:"icon,omitempty"`
	ID              string                    `json:"Id"`
	Image           string                    `json:"Image"`
	Labels          map[string]string         `json:"Labels,omitempty"`
	Metrics         *ContainerMetrics         `json:"metrics,omitempty"`
	Mounts          []ContainerMount          `json:"Mounts,omitempty"`
	Names           []string                  `json:"Names"`
	NetworkSettings *ContainerNetworkSettings `json:"NetworkSettings,omitempty"`
	Ports           []ContainerPort           `json:"Ports,omitempty"`
	ProxyPort       *string                   `json:"proxyPort,omitempty"`
	State           string                    `json:"State"`
	Status          string                    `json:"Status"`
	UpdateAvailable *bool                     `json:"updateAvailable,omitempty"`
	UpdateCheckedAt *int64                    `json:"updateCheckedAt,omitempty"`
	UpdateError     *string                   `json:"updateError,omitempty"`
	URL             *string                   `json:"url,omitempty"`
}

type DockerImage struct {
	Containers      *int              `json:"Containers,omitempty"`
	Created         int64             `json:"Created"`
	ID              string            `json:"Id"`
	Labels          map[string]string `json:"Labels,omitempty"`
	RepoDigests     []string          `json:"RepoDigests,omitempty"`
	RepoTags        []string          `json:"RepoTags"`
	Size            int64             `json:"Size"`
	UpdateAvailable *bool             `json:"updateAvailable,omitempty"`
}

type DockerNetworkContainer struct {
	Name        string  `json:"Name"`
	IPv4Address *string `json:"IPv4Address,omitempty"`
	IPv6Address *string `json:"IPv6Address,omitempty"`
	MACAddress  *string `json:"MacAddress,omitempty"`
}

type DockerNetworkIPAMConfig struct {
	Subnet  string `json:"Subnet"`
	Gateway string `json:"Gateway"`
}

type DockerNetworkIPAM struct {
	Config []DockerNetworkIPAMConfig `json:"Config,omitempty"`
}

type DockerNetwork struct {
	Containers map[string]DockerNetworkContainer `json:"Containers,omitempty"`
	Driver     string                            `json:"Driver"`
	EnableIPv4 *bool                             `json:"EnableIPv4,omitempty"`
	EnableIPv6 *bool                             `json:"EnableIPv6,omitempty"`
	ID         string                            `json:"Id"`
	Internal   *bool                             `json:"Internal,omitempty"`
	IPAM       *DockerNetworkIPAM                `json:"IPAM,omitempty"`
	Labels     map[string]string                 `json:"Labels,omitempty"`
	Name       string                            `json:"Name"`
	Options    map[string]string                 `json:"Options,omitempty"`
	Scope      string                            `json:"Scope"`
}

type DockerVolume struct {
	CreatedAt  *string           `json:"CreatedAt,omitempty"`
	Driver     string            `json:"Driver"`
	Labels     map[string]string `json:"Labels,omitempty"`
	Mountpoint string            `json:"Mountpoint"`
	Name       string            `json:"Name"`
	Options    map[string]string `json:"Options,omitempty"`
	Scope      *string           `json:"Scope,omitempty"`
}

type DockerSystemInfo struct {
	APIVersion      string   `json:"api_version"`
	Architecture    string   `json:"architecture"`
	BuildTime       string   `json:"build_time"`
	CgroupDriver    string   `json:"cgroup_driver"`
	CgroupVersion   string   `json:"cgroup_version"`
	DefaultRuntime  string   `json:"default_runtime"`
	DiskTotal       uint64   `json:"disk_total"`
	DiskUsed        uint64   `json:"disk_used"`
	DockerRootDir   string   `json:"docker_root_dir"`
	Experimental    bool     `json:"experimental"`
	GitCommit       string   `json:"git_commit"`
	GoVersion       string   `json:"go_version"`
	HTTPProxy       string   `json:"http_proxy"`
	HTTPSProxy      string   `json:"https_proxy"`
	ID              string   `json:"id"`
	InitBinary      string   `json:"init_binary"`
	IPv4Forwarding  bool     `json:"ipv4_forwarding"`
	KernelVersion   string   `json:"kernel_version"`
	LogPlugins      []string `json:"log_plugins"`
	LoggingDriver   string   `json:"logging_driver"`
	MemTotal        uint64   `json:"mem_total"`
	Name            string   `json:"name"`
	NCPU            int      `json:"ncpu"`
	NetworkPlugins  []string `json:"network_plugins"`
	NoProxy         string   `json:"no_proxy"`
	OperatingSystem string   `json:"operating_system"`
	OSType          string   `json:"os_type"`
	Runtimes        []string `json:"runtimes"`
	SecurityOptions []string `json:"security_options"`
	ServerVersion   string   `json:"server_version"`
	StorageDriver   string   `json:"storage_driver"`
	SystemTime      string   `json:"system_time"`
	VolumePlugins   []string `json:"volume_plugins"`
}

type ComposeService struct {
	ContainerCount int      `json:"container_count"`
	ContainerIDs   []string `json:"container_ids"`
	Icon           string   `json:"icon,omitempty"`
	Image          string   `json:"image"`
	Name           string   `json:"name"`
	Ports          []string `json:"ports"`
	State          string   `json:"state"`
	Status         string   `json:"status"`
	URL            string   `json:"url,omitempty"`
}

type ComposeProject struct {
	ConfigFiles     []string                   `json:"config_files"`
	Containers      []ContainerInfo            `json:"containers"`
	Icon            string                     `json:"icon,omitempty"`
	Name            string                     `json:"name"`
	Services        map[string]*ComposeService `json:"services"`
	Status          string                     `json:"status"`
	UpdateAvailable bool                       `json:"update_available"`
	WorkingDir      string                     `json:"working_dir"`
}

type AutoUpdateOptions struct {
	DownloadOnly    bool                   `json:"download_only"`
	Enabled         bool                   `json:"enabled"`
	ExcludePackages []string               `json:"exclude_packages"`
	Frequency       AutoUpdateFrequency    `json:"frequency"`
	RebootPolicy    AutoUpdateRebootPolicy `json:"reboot_policy"`
	Scope           AutoUpdateScope        `json:"scope"`
}

type AutoUpdateState struct {
	Backend string            `json:"backend"`
	Notes   []string          `json:"notes,omitempty"`
	Options AutoUpdateOptions `json:"options"`
}

type Service struct {
	ActiveEnterTimestamp   int64   `json:"active_enter_timestamp"`
	ActiveState            string  `json:"active_state"`
	Description            *string `json:"description,omitempty"`
	InactiveEnterTimestamp int64   `json:"inactive_enter_timestamp"`
	LoadState              string  `json:"load_state"`
	Name                   string  `json:"name"`
	SubState               string  `json:"sub_state"`
	UnitFileState          string  `json:"unit_file_state"`
}

type UnitInfo struct {
	ActiveEnterTimestamp   *int64   `json:"ActiveEnterTimestamp,omitempty"`
	ActiveState            *string  `json:"ActiveState,omitempty"`
	After                  []string `json:"After,omitempty"`
	Before                 []string `json:"Before,omitempty"`
	Conflicts              []string `json:"Conflicts,omitempty"`
	Description            *string  `json:"Description,omitempty"`
	ExecMainStatus         *int     `json:"ExecMainStatus,omitempty"`
	FragmentPath           *string  `json:"FragmentPath,omitempty"`
	ID                     *string  `json:"Id,omitempty"`
	InactiveEnterTimestamp *int64   `json:"InactiveEnterTimestamp,omitempty"`
	LastTriggerUSec        *int64   `json:"LastTriggerUSec,omitempty"`
	Listen                 []string `json:"Listen,omitempty"`
	LoadState              *string  `json:"LoadState,omitempty"`
	MainPID                *int     `json:"MainPID,omitempty"`
	MemoryCurrent          *int64   `json:"MemoryCurrent,omitempty"`
	NAccepted              *int     `json:"NAccepted,omitempty"`
	NConnections           *int     `json:"NConnections,omitempty"`
	NextElapseUSec         *int64   `json:"NextElapseUSec,omitempty"`
	PartOf                 []string `json:"PartOf,omitempty"`
	Requires               []string `json:"Requires,omitempty"`
	SubState               *string  `json:"SubState,omitempty"`
	TriggeredBy            []string `json:"TriggeredBy,omitempty"`
	Unit                   *string  `json:"Unit,omitempty"`
	UnitFileState          *string  `json:"UnitFileState,omitempty"`
	WantedBy               []string `json:"WantedBy,omitempty"`
	Wants                  []string `json:"Wants,omitempty"`
}

type Timer struct {
	ActiveEnterTimestamp   int64   `json:"active_enter_timestamp"`
	ActiveState            string  `json:"active_state"`
	Description            *string `json:"description,omitempty"`
	InactiveEnterTimestamp int64   `json:"inactive_enter_timestamp"`
	LastTriggerUSec        int64   `json:"last_trigger_usec"`
	LoadState              string  `json:"load_state"`
	Name                   string  `json:"name"`
	NextElapseUSec         int64   `json:"next_elapse_usec"`
	SubState               string  `json:"sub_state"`
	Unit                   string  `json:"unit"`
	UnitFileState          string  `json:"unit_file_state"`
}

type Socket struct {
	ActiveEnterTimestamp   int64    `json:"active_enter_timestamp"`
	ActiveState            string   `json:"active_state"`
	Description            *string  `json:"description,omitempty"`
	InactiveEnterTimestamp int64    `json:"inactive_enter_timestamp"`
	Listen                 []string `json:"listen"`
	LoadState              string   `json:"load_state"`
	NAccepted              int      `json:"n_accepted"`
	NConnections           int      `json:"n_connections"`
	Name                   string   `json:"name"`
	SubState               string   `json:"sub_state"`
	UnitFileState          string   `json:"unit_file_state"`
}

type UpgradeItem struct {
	Package string `json:"package"`
	Version string `json:"version,omitempty"`
}

type UpdateHistoryRow struct {
	Date     string        `json:"date"`
	Upgrades []UpgradeItem `json:"upgrades"`
}

type TunedProfile struct {
	Active      bool    `json:"active"`
	Description *string `json:"description,omitempty"`
	Name        string  `json:"name"`
	Recommended bool    `json:"recommended"`
}

type PowerStatus struct {
	ActiveProfile             string         `json:"active_profile"`
	Backend                   string         `json:"backend"`
	Error                     *string        `json:"error,omitempty"`
	InstallCommand            string         `json:"install_command"`
	Notes                     []string       `json:"notes,omitempty"`
	PackageName               string         `json:"package_name"`
	PowerProfilesDaemonActive bool           `json:"power_profiles_daemon_active"`
	Profiles                  []TunedProfile `json:"profiles"`
	RecommendedProfile        string         `json:"recommended_profile"`
	TunedActivatable          bool           `json:"tuned_activatable"`
	TunedActive               bool           `json:"tuned_active"`
	TunedAvailable            bool           `json:"tuned_available"`
	TunedStartable            bool           `json:"tuned_startable"`
	TunedUnitAvailable        bool           `json:"tuned_unit_available"`
	TunedUnitFileState        string         `json:"tuned_unit_file_state"`
}

type ApiResource struct {
	Content   *string       `json:"content,omitempty"`
	Extension string        `json:"extension"`
	IsDir     bool          `json:"isDir"`
	IsSymlink bool          `json:"isSymlink"`
	Items     []ApiResource `json:"items,omitempty"`
	Mode      string        `json:"mode"`
	Modified  string        `json:"modified"`
	Name      string        `json:"name"`
	Path      string        `json:"path"`
	Size      int64         `json:"size"`
	Type      string        `json:"type"`
}

type DirectorySizeData struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type SubfolderData struct {
	ModTime string `json:"mod_time"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

type SubfoldersResponse struct {
	Count      int             `json:"count"`
	Path       string          `json:"path"`
	Subfolders []SubfolderData `json:"subfolders"`
}

type SearchResult struct {
	Path     string  `json:"path"`
	Name     string  `json:"name"`
	Type     *string `json:"type,omitempty"`
	IsDir    *bool   `json:"isDir,omitempty"`
	Size     int64   `json:"size"`
	ModTime  *string `json:"mod_time,omitempty"`
	ModTime2 *string `json:"modTime,omitempty"`
	Modified *string `json:"modified,omitempty"`
}

type SearchResponse struct {
	Count   int            `json:"count"`
	Query   string         `json:"query"`
	Results []SearchResult `json:"results"`
}

type UsersGroupsResponse struct {
	Groups []string `json:"groups"`
	Users  []string `json:"users"`
}

type IndexerStatusResponse struct {
	DirsIndexed  int     `json:"dirs_indexed"`
	FilesIndexed int     `json:"files_indexed"`
	LastIndexed  *string `json:"last_indexed,omitempty"`
	Running      bool    `json:"running"`
	Status       string  `json:"status"`
	TotalSize    int64   `json:"total_size"`
	Warning      *string `json:"warning,omitempty"`
}

type AccountUser struct {
	Gecos        string   `json:"gecos"`
	GID          int      `json:"gid"`
	Groups       []string `json:"groups"`
	HomeDir      string   `json:"homeDir"`
	IsLocked     bool     `json:"isLocked"`
	IsSystem     bool     `json:"isSystem"`
	LastLogin    string   `json:"lastLogin"`
	PrimaryGroup string   `json:"primaryGroup"`
	Shell        string   `json:"shell"`
	UID          int      `json:"uid"`
	Username     string   `json:"username"`
}

type AccountUserLogin struct {
	ID        string  `json:"id"`
	Source    string  `json:"source"`
	StartedAt *string `json:"startedAt,omitempty"`
	Status    string  `json:"status"`
	Terminal  string  `json:"terminal"`
	Time      string  `json:"time"`
	Username  string  `json:"username"`
}

type AccountActiveSession struct {
	Idle      *string `json:"idle,omitempty"`
	PID       *int    `json:"pid,omitempty"`
	SessionID *string `json:"sessionId,omitempty"`
	Source    *string `json:"source,omitempty"`
	StartedAt string  `json:"startedAt"`
	Terminal  string  `json:"terminal"`
}

type AccountPasswordState struct {
	Error         *string `json:"error,omitempty"`
	Expires       *string `json:"expires,omitempty"`
	ExpiresInDays *int    `json:"expiresInDays,omitempty"`
	HasPassword   bool    `json:"hasPassword"`
	LastChanged   *string `json:"lastChanged,omitempty"`
	Locked        bool    `json:"locked"`
	MaxDays       *int    `json:"maxDays,omitempty"`
	WarningDays   *int    `json:"warningDays,omitempty"`
}

type AccountAdminAccess struct {
	Groups  []string `json:"groups"`
	IsAdmin bool     `json:"isAdmin"`
}

type AccountHomeHealth struct {
	Error        *string `json:"error,omitempty"`
	Exists       bool    `json:"exists"`
	GroupGID     *int    `json:"groupGid,omitempty"`
	GroupName    *string `json:"groupName,omitempty"`
	IsDirectory  bool    `json:"isDirectory"`
	Mode         *string `json:"mode,omitempty"`
	OwnerMatches bool    `json:"ownerMatches"`
	OwnerUID     *int    `json:"ownerUid,omitempty"`
}

type AccountSSHAccess struct {
	AuthorizedKeysCount        int     `json:"authorizedKeysCount"`
	AuthorizedKeysExists       bool    `json:"authorizedKeysExists"`
	AuthorizedKeysMode         *string `json:"authorizedKeysMode,omitempty"`
	AuthorizedKeysOwnerMatches bool    `json:"authorizedKeysOwnerMatches"`
	Error                      *string `json:"error,omitempty"`
	SSHDirExists               bool    `json:"sshDirExists"`
	SSHDirMode                 *string `json:"sshDirMode,omitempty"`
}

type AccountUserProcess struct {
	Command string  `json:"command"`
	CPU     float64 `json:"cpu"`
	Memory  float64 `json:"memory"`
	PID     int     `json:"pid"`
}

type AccountProcessSummary struct {
	Count int                  `json:"count"`
	Error *string              `json:"error,omitempty"`
	Top   []AccountUserProcess `json:"top"`
}

type AccountUserDetails struct {
	ActiveSessions               []AccountActiveSession `json:"activeSessions"`
	Admin                        AccountAdminAccess     `json:"admin"`
	FailedLoginAttempts          int                    `json:"failedLoginAttempts"`
	FailedLoginAttemptsAvailable bool                   `json:"failedLoginAttemptsAvailable"`
	FailedLoginAttemptsError     *string                `json:"failedLoginAttemptsError,omitempty"`
	Home                         AccountHomeHealth      `json:"home"`
	Password                     AccountPasswordState   `json:"password"`
	Processes                    AccountProcessSummary  `json:"processes"`
	SSH                          AccountSSHAccess       `json:"ssh"`
	Username                     string                 `json:"username"`
}

type AccountGroup struct {
	GID      int      `json:"gid"`
	IsSystem bool     `json:"isSystem"`
	Members  []string `json:"members"`
	Name     string   `json:"name"`
}

type NFSExport struct {
	Active  bool        `json:"active"`
	Clients []NFSClient `json:"clients"`
	Path    string      `json:"path"`
}

type SambaShare struct {
	Name       string            `json:"name"`
	Properties map[string]string `json:"properties"`
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

type VersionResponse struct {
	CheckedAt       string `json:"checked_at"`
	CurrentVersion  string `json:"current_version"`
	Error           string `json:"error,omitempty"`
	LatestVersion   string `json:"latest_version,omitempty"`
	UpdateAvailable bool   `json:"update_available"`
}

type WireGuardInterface struct {
	Address     string `json:"address"`
	IsConnected string `json:"isConnected"`
	IsEnabled   bool   `json:"isEnabled"`
	Name        string `json:"name"`
	PeerCount   int    `json:"peerCount"`
	Port        int    `json:"port"`
}

type Peer struct {
	AllowedIPs          []string `json:"allowed_ips,omitempty"`
	Endpoint            *string  `json:"endpoint,omitempty"`
	LastHandshake       *string  `json:"last_handshake,omitempty"`
	LastHandshakeUnix   *int64   `json:"last_handshake_unix,omitempty"`
	Name                string   `json:"name"`
	PersistentKeepalive *int     `json:"persistent_keepalive,omitempty"`
	PresharedKey        *string  `json:"preshared_key,omitempty"`
	PublicKey           string   `json:"public_key"`
	RXBPS               *float64 `json:"rx_bps,omitempty"`
	RXBytes             *int64   `json:"rx_bytes,omitempty"`
	TXBPS               *float64 `json:"tx_bps,omitempty"`
	TXBytes             *int64   `json:"tx_bytes,omitempty"`
}

type PeerConfigDownload struct {
	Content  string `json:"content"`
	Filename string `json:"filename"`
}

type QRCodeResponse struct {
	QRCode string `json:"qrcode"`
}

type DeleteStackResult struct {
	DeletedPath  string `json:"deleted_path"`
	DirDeleted   bool   `json:"dir_deleted"`
	FilesDeleted bool   `json:"files_deleted"`
	Message      string `json:"message"`
	Project      string `json:"project"`
}

type ConfigSetResult struct {
	Message string `json:"message"`
	Path    string `json:"path"`
}

type InstallCapabilityResult struct {
	Available bool    `json:"available"`
	Error     *string `json:"error,omitempty"`
}

type IndexerConfig struct {
	DBAutoVacuum         string `json:"db_auto_vacuum"`
	DBBusyTimeout        string `json:"db_busy_timeout"`
	DBConnMaxIdleTime    string `json:"db_conn_max_idle_time"`
	DBJournalMode        string `json:"db_journal_mode"`
	DBMaxIdleConns       int    `json:"db_max_idle_conns"`
	DBMaxOpenConns       int    `json:"db_max_open_conns"`
	DBPath               string `json:"db_path"`
	DBSynchronous        string `json:"db_synchronous"`
	FreshIndex           bool   `json:"fresh_index"`
	IncludeHidden        bool   `json:"include_hidden"`
	IncludeNetworkMounts bool   `json:"include_network_mounts"`
	IndexName            string `json:"index_name"`
	IndexPath            string `json:"index_path"`
	Interval             string `json:"interval"`
	KeepIndexes          int    `json:"keep_indexes"`
	ListenAddr           string `json:"listen_addr"`
	SocketPath           string `json:"socket_path"`
}

type IndexerConfigSetResult struct {
	Config          IndexerConfig `json:"config"`
	RestartRequired bool          `json:"restart_required"`
}

type IndexerTimerSetResult struct {
	Config    IndexerConfig `json:"config"`
	Interval  string        `json:"interval"`
	TimerUnit string        `json:"timer_unit"`
}

type IndexerDaemonStatus struct {
	ActiveOperation *string `json:"active_operation,omitempty"`
	ActivePath      *string `json:"active_path,omitempty"`
	DatabaseSize    int64   `json:"database_size"`
	LastIndexed     *string `json:"last_indexed,omitempty"`
	NumDirs         int     `json:"num_dirs"`
	NumFiles        int     `json:"num_files"`
	Running         bool    `json:"running"`
	SHMSize         int64   `json:"shm_size"`
	Status          string  `json:"status"`
	TotalEntries    int     `json:"total_entries"`
	TotalIndexes    int     `json:"total_indexes"`
	TotalOnDisk     int64   `json:"total_on_disk"`
	TotalSize       int64   `json:"total_size"`
	WALSize         int64   `json:"wal_size"`
	Warning         *string `json:"warning,omitempty"`
}

type DirectoryValidationResult struct {
	CanCreate   bool   `json:"canCreate"`
	CanWrite    bool   `json:"canWrite"`
	Error       string `json:"error,omitempty"`
	Exists      bool   `json:"exists"`
	IsDirectory bool   `json:"isDirectory"`
	Valid       bool   `json:"valid"`
}

type JobError struct {
	Code    *int   `json:"code,omitempty"`
	Message string `json:"message"`
}

type JobOwner struct {
	SessionID *string `json:"session_id,omitempty"`
	Username  *string `json:"username,omitempty"`
	UID       *int    `json:"uid,omitempty"`
}

type JobSnapshot struct {
	CreatedAt  string    `json:"created_at"`
	Error      *JobError `json:"error,omitempty"`
	FinishedAt *string   `json:"finished_at,omitempty"`
	ID         string    `json:"id"`
	Owner      *JobOwner `json:"owner,omitempty"`
	Progress   any       `json:"progress,omitempty"`
	Request    any       `json:"request,omitempty"`
	Result     any       `json:"result,omitempty"`
	StartedAt  *string   `json:"started_at,omitempty"`
	State      JobState  `json:"state"`
	Type       string    `json:"type"`
	UpdatedAt  string    `json:"updated_at"`
}

type JobEvent struct {
	Error    *JobError   `json:"error,omitempty"`
	Job      JobSnapshot `json:"job"`
	Progress any         `json:"progress,omitempty"`
	Result   any         `json:"result,omitempty"`
	Type     string      `json:"type"`
}

type Update struct {
	Changelog string   `json:"changelog"`
	CVE       []string `json:"cve"`
	Issued    string   `json:"issued"`
	PackageID string   `json:"package_id"`
	Restart   int      `json:"restart"`
	State     int      `json:"state"`
	Summary   string   `json:"summary"`
	Version   string   `json:"version"`
}

type UpdateItem struct {
	Arch           string `json:"arch,omitempty"`
	CurrentVersion string `json:"currentVersion,omitempty"`
	Name           string `json:"name"`
	NewVersion     string `json:"newVersion,omitempty"`
	Repo           string `json:"repo,omitempty"`
}

type UpdatesFastResponse struct {
	Updates []UpdateItem `json:"updates,omitempty"`
}

type CaddyStatusResponse struct {
	Enabled    bool         `json:"enabled"`
	BaseDomain string       `json:"baseDomain"`
	Running    bool         `json:"running"`
	Routes     []CaddyRoute `json:"routes"`
}

type CaddyRoute struct {
	Host      string `json:"host"`
	Container string `json:"container"`
	Port      string `json:"port"`
}

type ComposeFilePathResponse struct {
	Path      string `json:"path"`
	Exists    bool   `json:"exists"`
	Directory string `json:"directory"`
}

type DockerFoldersResponse struct {
	Folders []string `json:"folders"`
}

type DockerIconDataResponse struct {
	Data string `json:"data"`
}

type DockerIconInfoResponse struct {
	Type       string `json:"type"`
	Identifier string `json:"identifier"`
	Cached     bool   `json:"cached"`
}

type DockerIconURIResponse struct {
	URI string `json:"uri"`
}

type ValidateComposeError struct {
	Line    int                 `json:"line,omitempty"`
	Column  int                 `json:"column,omitempty"`
	Field   string              `json:"field,omitempty"`
	Message string              `json:"message"`
	Type    ValidationIssueType `json:"type"`
}

type ValidateComposeResponse struct {
	Valid             bool                   `json:"valid"`
	Errors            []ValidateComposeError `json:"errors"`
	NormalizedContent string                 `json:"normalized_content,omitempty"`
}

type StoragePathResult struct {
	Success    bool    `json:"success"`
	Mountpoint *string `json:"mountpoint,omitempty"`
	Path       *string `json:"path,omitempty"`
}

type StorageCreateLVResult struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
}

type StorageMountResult struct {
	Success    bool    `json:"success"`
	Mountpoint *string `json:"mountpoint,omitempty"`
	Warning    *string `json:"warning,omitempty"`
}

type StorageWarningResult struct {
	Success bool    `json:"success"`
	Warning *string `json:"warning,omitempty"`
}

type OfflineUpdatesResponse struct {
	Status *string `json:"status,omitempty"`
	Error  *string `json:"error,omitempty"`
}

type CapabilitiesResponse struct {
	session.CapabilitiesAvailable
	session.CapabilitiesError
}

type AppConfig struct {
	AppSettings AppSettings    `json:"appSettings"`
	Dismissals  *Dismissals    `json:"dismissals,omitempty"`
	Docker      DockerSettings `json:"docker"`
	Jobs        JobSettings    `json:"jobs"`
}

type AppSettings struct {
	ChunkSizeMB             *int                            `json:"chunkSizeMB,omitempty"`
	ContainerOrder          []string                        `json:"containerOrder,omitempty"`
	DashboardOrder          []string                        `json:"dashboardOrder,omitempty"`
	DockerDashboardSections *ConfigDockerDashboardSections  `json:"dockerDashboardSections,omitempty"`
	HardwareSections        *ConfigHardwareSections         `json:"hardwareSections,omitempty"`
	HiddenCards             []string                        `json:"hiddenCards,omitempty"`
	PrimaryColor            string                          `json:"primaryColor"`
	ShowHiddenFiles         bool                            `json:"showHiddenFiles"`
	SidebarCollapsed        bool                            `json:"sidebarCollapsed"`
	Theme                   Theme                           `json:"theme"`
	ThemeColors             *ConfigThemeColorsByModePayload `json:"themeColors,omitempty"`
	ViewModes               map[string]TableCardViewMode    `json:"viewModes,omitempty"`
}

type DockerProxySettings struct {
	BaseDomain   *string `json:"baseDomain,omitempty"`
	CaddyEnabled bool    `json:"caddyEnabled"`
	TLSEmail     *string `json:"tlsEmail,omitempty"`
}

type DockerSettings struct {
	Folders []string            `json:"folders"`
	Proxy   DockerProxySettings `json:"proxy"`
}

type JobSettings struct {
	ArchiveCompressionWorkers int `json:"archiveCompressionWorkers"`
	ArchiveExtractWorkers     int `json:"archiveExtractWorkers"`
	HeavyArchiveConcurrency   int `json:"heavyArchiveConcurrency"`
	NotificationMinIntervalMs int `json:"notificationMinIntervalMs"`
	ProgressMinBytesMB        int `json:"progressMinBytesMB"`
	ProgressMinIntervalMs     int `json:"progressMinIntervalMs"`
}

type Dismissals struct {
	FailedLoginAlertID    *string `json:"failedLoginAlertId,omitempty"`
	UncleanShutdownBootID *string `json:"uncleanShutdownBootId,omitempty"`
}
