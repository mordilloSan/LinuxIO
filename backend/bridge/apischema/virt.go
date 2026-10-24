package apischema

type VMDisk struct {
	Device     string `json:"device"`
	Target     string `json:"target"`
	Path       string `json:"path"`
	VolumeName string `json:"volumeName,omitempty"`
	SizeGB     int    `json:"sizeGB,omitzero"`
	// AllocatedBytes is the space the image really uses on disk.
	AllocatedBytes int64 `json:"allocatedBytes,omitzero"`
	Owned          bool  `json:"owned"`
}

type VMNIC struct {
	MAC            string   `json:"mac,omitempty"`
	Model          string   `json:"model,omitempty"`
	Network        string   `json:"network,omitempty"`
	AttachmentType string   `json:"attachmentType,omitempty"`
	IPAddresses    []string `json:"ipAddresses,omitempty"`
	// Target is the host-side tap device while the VM runs.
	Target                string   `json:"target,omitempty"`
	ReceiveBytesPerSecond *float64 `json:"receiveBytesPerSecond,omitempty"`
	SendBytesPerSecond    *float64 `json:"sendBytesPerSecond,omitempty"`
}

type VMNetwork struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	Active            bool   `json:"active"`
	HasPhysicalUplink bool   `json:"hasPhysicalUplink,omitzero"`
	// Bridge is the host interface backing the network.
	Bridge string `json:"bridge,omitempty"`
	// Uplink is the first physical NIC enslaved to the bridge, if any.
	Uplink string `json:"uplink,omitempty"`
	// Subnet is the NAT network's address and prefix, such as 192.168.122.1/24.
	Subnet string `json:"subnet,omitempty"`
}

type VirtualMachine struct {
	Name        string     `json:"name"`
	UUID        string     `json:"uuid,omitempty"`
	State       string     `json:"state"`
	VCPUs       int        `json:"vcpus"`
	MemoryMB    int        `json:"memoryMB"`
	DiskGB      int        `json:"diskGB"`
	Autostart   bool       `json:"autostart"`
	Firmware    VMFirmware `json:"firmware,omitempty"`
	HasGraphics bool       `json:"hasGraphics"`
	// OS is a distro id such as "ubuntu" or "win", from LinuxIO's own metadata
	// or libosinfo's; empty when unknown.
	OS         string   `json:"os,omitempty"`
	OwnedDisks []string `json:"ownedDisks"`
	Disks      []VMDisk `json:"disks,omitempty"`
	NICs       []VMNIC  `json:"nics,omitempty"`
	// Metrics is set only for a VM libvirt reported live stats for.
	Metrics *VMMetrics `json:"metrics,omitempty"`
}

// VMMetrics is one running VM's live usage. CPUPercent is relative to the
// VM's vCPUs; per-second rates are nil until this bridge has two samples.
type VMMetrics struct {
	CPUPercent                   *float64 `json:"cpuPercent,omitempty"`
	MemoryUsedMB                 int      `json:"memoryUsedMB"`
	NetworkReceiveBytesPerSecond *float64 `json:"networkReceiveBytesPerSecond,omitempty"`
	NetworkSendBytesPerSecond    *float64 `json:"networkSendBytesPerSecond,omitempty"`
	DiskReadBytesPerSecond       *float64 `json:"diskReadBytesPerSecond,omitempty"`
	DiskWriteBytesPerSecond      *float64 `json:"diskWriteBytesPerSecond,omitempty"`
}

// VMStoragePool is a libvirt storage pool. Sizes are zero while the pool is
// inactive, since libvirt only reports them for active pools.
type VMStoragePool struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Path            string `json:"path,omitempty"`
	Active          bool   `json:"active"`
	CapacityBytes   uint64 `json:"capacityBytes"`
	AllocationBytes uint64 `json:"allocationBytes"`
	AvailableBytes  uint64 `json:"availableBytes"`
}

type VMPreflightFirmware struct {
	UEFIAvailable bool `json:"uefiAvailable"`
	BIOSAvailable bool `json:"biosAvailable"`
	// Detected is the firmware create picks for this source when none is requested.
	Detected VMFirmware `json:"detected,omitempty"`
}

type VMManagedPaths struct {
	Root        string `json:"root"`
	ISOs        string `json:"isos"`
	CloudImages string `json:"cloudImages"`
}

type VMPreflight struct {
	KvmPresent           bool                `json:"kvmPresent"`
	QemuPresent          bool                `json:"qemuPresent"`
	LibvirtReachable     bool                `json:"libvirtReachable"`
	DefaultPoolExists    bool                `json:"defaultPoolExists"`
	DefaultPoolActive    bool                `json:"defaultPoolActive"`
	DefaultNetworkExists bool                `json:"defaultNetworkExists"`
	DefaultNetworkActive bool                `json:"defaultNetworkActive"`
	ISOReadable          bool                `json:"isoReadable"`
	Firmware             VMPreflightFirmware `json:"firmware"`
	ManagedPaths         VMManagedPaths      `json:"managedPaths"`
	Warnings             []string            `json:"warnings,omitempty"`
	Errors               []string            `json:"errors,omitempty"`
}

type VMCreateProgress struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Percent *int   `json:"percent,omitempty"`
}

type VMTemplate struct {
	ID            string          `json:"id"`
	ImagePresetID VMImagePresetID `json:"imagePresetId"`
	Label         string          `json:"label"`
	Version       string          `json:"version"`
	SourceURL     string          `json:"sourceUrl"`
	DownloadedAt  string          `json:"downloadedAt"`
	SizeBytes     int64           `json:"sizeBytes"`
	Path          string          `json:"path"`
}

type VMTemplateLibrary struct {
	Path      string       `json:"path"`
	Templates []VMTemplate `json:"templates"`
}

// VMUnusedDisk is a LinuxIO-created VM disk or cloud-init seed that no
// defined VM references, such as one kept when its VM was deleted.
type VMUnusedDisk struct {
	Name       string `json:"name"`
	Path       string `json:"path"`
	VMName     string `json:"vmName"`
	SizeBytes  int64  `json:"sizeBytes"`
	ModifiedAt string `json:"modifiedAt"`
}

func (p VMCreateProgress) ProgressEnvelope() TaskProgress {
	return TaskProgress{
		Percentage: p.Percent,
		Phase:      p.Phase,
		Message:    p.Message,
		Detail:     p,
	}
}

type VMDeleteResult struct {
	Removed   []string `json:"removed"`
	Preserved []string `json:"preserved"`
}

type VMSourceType string
type VMImagePresetID string
type VMFirmware string

type VMCreateRequest struct {
	Name              string          `json:"name"`
	VCPUs             int             `json:"vcpus"`
	MemoryMB          int             `json:"memoryMB"`
	DiskGB            int             `json:"diskGB"`
	ISOPath           string          `json:"isoPath,omitempty"`
	ImportPath        string          `json:"importPath,omitempty"`
	SourceType        VMSourceType    `json:"sourceType,omitempty"`
	ImagePresetID     VMImagePresetID `json:"imagePresetId,omitempty"`
	TemplateID        string          `json:"templateId,omitempty"`
	CloudInitUsername string          `json:"cloudInitUsername,omitempty"`
	CloudInitPassword string          `json:"cloudInitPassword,omitempty"`
	CloudInitSSHKey   string          `json:"cloudInitSshKey,omitempty"`
	CloudInitHostname string          `json:"cloudInitHostname,omitempty"`
	Network           string          `json:"network,omitempty"`
	// Firmware overrides the detected boot firmware; empty picks automatically.
	Firmware VMFirmware `json:"firmware,omitempty"`
	Start    bool       `json:"start"`
}

// VMUpdateRequest redefines a shut-off VM. An empty NewName, OS or Network
// keeps the current value, and DiskGB 0 keeps the disk size.
type VMUpdateRequest struct {
	Name     string     `json:"name"`
	NewName  string     `json:"newName,omitempty"`
	VCPUs    int        `json:"vcpus"`
	MemoryMB int        `json:"memoryMB"`
	DiskGB   int        `json:"diskGB,omitzero"`
	Firmware VMFirmware `json:"firmware"`
	Network  string     `json:"network,omitempty"`
	// OS is a distro id such as "ubuntu", stored in LinuxIO's metadata.
	OS string `json:"os,omitempty"`
}

type VMAutostartRequest struct {
	Name      string `json:"name"`
	Autostart bool   `json:"autostart"`
}

type VMDeleteRequest struct {
	Name        string `json:"name"`
	DeleteDisks bool   `json:"deleteDisks"`
}

type VMTemplateDownloadRequest struct {
	ImagePresetID VMImagePresetID `json:"imagePresetId"`
}

type VMTemplateRequest struct {
	ImagePresetID VMImagePresetID `json:"imagePresetId"`
	TemplateID    string          `json:"templateId"`
}

type VMPreflightRequest struct {
	ISOPath       *string         `json:"isoPath,omitempty"`
	ImportPath    *string         `json:"importPath,omitempty"`
	SourceType    VMSourceType    `json:"sourceType,omitempty"`
	ImagePresetID VMImagePresetID `json:"imagePresetId,omitempty"`
	Network       string          `json:"network,omitempty"`
}
