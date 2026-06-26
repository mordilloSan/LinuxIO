package apischema

import (
	"reflect"
)

// TypeSpec is a route payload type reflected into generated TypeScript.
type TypeSpec struct {
	GoType reflect.Type
}

// NoRequest marks a route that takes no request payload.
type NoRequest struct{}

// NoResponse marks a route that returns no result payload.
type NoResponse struct{}

func TypeOf[T any]() TypeSpec {
	return TypeSpec{GoType: reflect.TypeFor[T]()}
}

// Common route contract fragments.
type MessageResponse struct {
	Message string `json:"message"`
}

type SuccessResponse struct {
	Success bool `json:"success"`
}

type SuccessPathResponse struct {
	Success bool   `json:"success"`
	Path    string `json:"path"`
}

type SuccessNameResponse struct {
	Success bool   `json:"success"`
	Name    string `json:"name"`
}

type NTPServersRequest struct {
	Servers []string `json:"servers"`
}

type PackageUpdateRequest struct {
	PackageIDs []string `json:"packageIds"`
}

type ContainerIDRequest struct {
	ContainerID string `json:"containerId"`
}

type UsernameRequest struct {
	Username string `json:"username"`
}

type GroupNameRequest struct {
	GroupName string `json:"groupName"`
}

type ChangePasswordRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type TerminateSessionRequest struct {
	SessionID string `json:"sessionId"`
	PID       string `json:"pid"`
}

type SessionIDRequest struct {
	SessionID string `json:"sessionID"`
}

type EnabledRequest struct {
	Enabled string `json:"enabled"`
}

type ISOTimeRequest struct {
	ISOTime string `json:"isoTime"`
}

type TimezoneRequest struct {
	Timezone string `json:"timezone"`
}

type NameRequest struct {
	Name string `json:"name"`
}

type VMSourceType string
type VMImagePresetID string

type VMCreateRequest struct {
	Name              string          `json:"name"`
	VCPUs             int             `json:"vcpus"`
	MemoryMB          int             `json:"memoryMB"`
	DiskGB            int             `json:"diskGB"`
	ISOPath           string          `json:"isoPath,omitempty"`
	SourceType        VMSourceType    `json:"sourceType,omitempty"`
	ImagePresetID     VMImagePresetID `json:"imagePresetId,omitempty"`
	CloudInitUsername string          `json:"cloudInitUsername,omitempty"`
	CloudInitPassword string          `json:"cloudInitPassword,omitempty"`
	CloudInitSSHKey   string          `json:"cloudInitSshKey,omitempty"`
	CloudInitHostname string          `json:"cloudInitHostname,omitempty"`
	Network           string          `json:"network,omitempty"`
	Start             bool            `json:"start"`
}

type VMDeleteRequest struct {
	Name        string `json:"name"`
	DeleteDisks bool   `json:"deleteDisks"`
}

type VMPreflightRequest struct {
	ISOPath       *string         `json:"isoPath,omitempty"`
	SourceType    VMSourceType    `json:"sourceType,omitempty"`
	ImagePresetID VMImagePresetID `json:"imagePresetId,omitempty"`
}

type ImageIDRequest struct {
	ImageID string `json:"imageId"`
}

type IDRequest struct {
	ID string `json:"id"`
}

type ProjectNameRequest struct {
	ProjectName string `json:"projectName"`
}

type StackNameRequest struct {
	StackName string `json:"stackName"`
}

type IdentifierRequest struct {
	Identifier string `json:"identifier"`
}

type DeleteStackRequest struct {
	ProjectName     string `json:"projectName"`
	DeleteFile      bool   `json:"deleteFile"`
	DeleteDirectory bool   `json:"deleteDirectory"`
}

type ContentRequest struct {
	Content string `json:"content"`
}

type DirPathRequest struct {
	DirPath string `json:"dirPath"`
}

type PathRequest struct {
	Path string `json:"path"`
}

type SourceDestinationRequest struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Overwrite   *bool  `json:"overwrite,omitempty"`
}

// BatchTransferRequest copies or moves many sources into a single destination
// directory within one job. Each source's final name is its basename.
type BatchTransferRequest struct {
	Sources     []string `json:"sources"`
	Destination string   `json:"destination"`
	Overwrite   *bool    `json:"overwrite,omitempty"`
}

// BatchPathRequest deletes many paths within one job.
type BatchPathRequest struct {
	Paths []string `json:"paths"`
}

type ActionSourceDestinationRequest struct {
	Action string `json:"action"`
	Source string `json:"src"`
	Dest   string `json:"dst"`
}

type HostnameRequest struct {
	Hostname string `json:"hostname"`
}

type IntervalRequest struct {
	Interval string `json:"interval"`
}

type JobIDRequest struct {
	JobID string `json:"jobId"`
}

type JobDataRequest struct {
	JobID  string  `json:"jobId"`
	Offset *string `json:"offset,omitempty"`
}

type InterfaceRequest struct {
	Iface string `json:"iface"`
}

type InterfaceMethodRequest struct {
	Iface  string `json:"iface"`
	Method string `json:"method"`
}

type IPv4ManualRequest struct {
	Iface   string `json:"iface"`
	Address string `json:"address"`
	Gateway string `json:"gateway"`
	DNS     string `json:"dns"`
}

type InterfaceMTURequest struct {
	Iface string `json:"iface"`
	MTU   string `json:"mtu"`
}

type ProfileRequest struct {
	Profile string `json:"profile"`
}

type ServerRequest struct {
	Server string `json:"server"`
}

type ServerExportMountOptionsPersistRequest struct {
	Server     string `json:"server"`
	ExportPath string `json:"exportPath"`
	Mountpoint string `json:"mountpoint"`
	Options    string `json:"options"`
	Persist    string `json:"persist"`
}

type MountpointOptionsUpdateFstabRequest struct {
	Mountpoint  string `json:"mountpoint"`
	Options     string `json:"options"`
	UpdateFstab string `json:"updateFstab"`
}

type MountpointRequest struct {
	Mountpoint string `json:"mountpoint"`
}

type MountpointNameRequest struct {
	Mountpoint string `json:"mountpoint"`
	Name       string `json:"name"`
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

type AlertIDRequest struct {
	AlertID string `json:"alertId"`
}

type BootIDRequest struct {
	BootID string `json:"bootId"`
}

type CapabilityRequest struct {
	Capability string `json:"capability"`
}

type ServiceNameRequest struct {
	ServiceName string `json:"serviceName"`
}

type UnitNameRequest struct {
	UnitName string `json:"unitName"`
}

type PackageIDRequest struct {
	PackageID string `json:"packageId"`
}

type InterfaceNameRequest struct {
	InterfaceName string `json:"interfaceName"`
}

type InterfaceNamePeerNameRequest struct {
	InterfaceName string `json:"interfaceName"`
	PeerName      string `json:"peerName"`
}

type WireGuardAddInterfaceRequest struct {
	Name       string  `json:"name"`
	Addresses  string  `json:"addresses"`
	ListenPort string  `json:"listenPort"`
	EgressNic  string  `json:"egressNic"`
	DNS        *string `json:"dns,omitempty"`
	MTU        *string `json:"mtu,omitempty"`
	PeersJSON  *string `json:"peersJson,omitempty"`
	NumPeers   *string `json:"numPeers,omitempty"`
}

type DockerUpdateCheckResult struct {
	Checked int `json:"checked"`
	Errors  int `json:"errors"`
	Updates int `json:"updates"`
}

type DockerContainerUpdateResult struct {
	ContainerID     string `json:"containerId"`
	ContainerName   string `json:"containerName"`
	Error           string `json:"error,omitempty"`
	Image           string `json:"image"`
	NewImageID      string `json:"newImageId,omitempty"`
	PreviousImageID string `json:"previousImageId,omitempty"`
	Updated         bool   `json:"updated"`
}

type DockerLogsFollowRequest struct {
	ContainerID string  `json:"containerId"`
	Tail        *string `json:"tail,omitempty"`
}

type DockerStartedFailedResponse struct {
	Started int `json:"started"`
	Failed  int `json:"failed"`
}

type DockerStoppedFailedResponse struct {
	Stopped int `json:"stopped"`
	Failed  int `json:"failed"`
}

type CreateUserRequest struct {
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	FullName   *string  `json:"fullName,omitempty"`
	HomeDir    *string  `json:"homeDir,omitempty"`
	Shell      *string  `json:"shell,omitempty"`
	Groups     []string `json:"groups,omitempty"`
	CreateHome *bool    `json:"createHome,omitempty"`
}

type ModifyUserRequest struct {
	Username string   `json:"username"`
	FullName *string  `json:"fullName,omitempty"`
	HomeDir  *string  `json:"homeDir,omitempty"`
	Shell    *string  `json:"shell,omitempty"`
	Groups   []string `json:"groups,omitempty"`
}

type CreateGroupRequest struct {
	Name string `json:"name"`
	GID  *int   `json:"gid,omitempty"`
}

type ModifyGroupMembersRequest struct {
	GroupName string   `json:"groupName"`
	Members   []string `json:"members"`
}

type ConfigSetPayload struct {
	AppSettings *ConfigAppSettingsPayload `json:"appSettings,omitempty"`
	Docker      *ConfigDockerPayload      `json:"docker,omitempty"`
	Jobs        *ConfigJobSettingsPayload `json:"jobs,omitempty"`
	Dismissals  *ConfigDismissalsPayload  `json:"dismissals,omitempty"`
}

type ConfigAppSettingsPayload struct {
	Theme                   *string                         `json:"theme,omitempty"`
	PrimaryColor            *string                         `json:"primaryColor,omitempty"`
	ThemeColors             *ConfigThemeColorsByModePayload `json:"themeColors,omitempty"`
	SidebarCollapsed        *bool                           `json:"sidebarCollapsed,omitempty"`
	ShowHiddenFiles         *bool                           `json:"showHiddenFiles,omitempty"`
	DashboardOrder          []string                        `json:"dashboardOrder,omitempty"`
	HiddenCards             []string                        `json:"hiddenCards,omitempty"`
	ContainerOrder          []string                        `json:"containerOrder,omitempty"`
	DockerDashboardSections *ConfigDockerDashboardSections  `json:"dockerDashboardSections,omitempty"`
	HardwareSections        *ConfigHardwareSections         `json:"hardwareSections,omitempty"`
	ViewModes               map[string]string               `json:"viewModes,omitempty"`
	ChunkSizeMB             *int                            `json:"chunkSizeMB,omitempty"`
}

type ConfigThemeColorsByModePayload struct {
	Light *ConfigThemeColorsPayload `json:"light,omitempty"`
	Dark  *ConfigThemeColorsPayload `json:"dark,omitempty"`
}

type ConfigThemeColorsPayload struct {
	BackgroundDefault               *string `json:"backgroundDefault,omitempty"`
	BackgroundPaper                 *string `json:"backgroundPaper,omitempty"`
	HeaderBackground                *string `json:"headerBackground,omitempty"`
	FooterBackground                *string `json:"footerBackground,omitempty"`
	SidebarBackground               *string `json:"sidebarBackground,omitempty"`
	CardBackground                  *string `json:"cardBackground,omitempty"`
	DialogBorder                    *string `json:"dialogBorder,omitempty"`
	DialogGlow                      *string `json:"dialogGlow,omitempty"`
	DialogBackdrop                  *string `json:"dialogBackdrop,omitempty"`
	CodeBackground                  *string `json:"codeBackground,omitempty"`
	CodeText                        *string `json:"codeText,omitempty"`
	ChartRx                         *string `json:"chartRx,omitempty"`
	ChartTx                         *string `json:"chartTx,omitempty"`
	ChartNeutral                    *string `json:"chartNeutral,omitempty"`
	FileBrowserSurface              *string `json:"fileBrowserSurface,omitempty"`
	FileBrowserChrome               *string `json:"fileBrowserChrome,omitempty"`
	FileBrowserBreadcrumbBackground *string `json:"fileBrowserBreadcrumbBackground,omitempty"`
	FileBrowserBreadcrumbText       *string `json:"fileBrowserBreadcrumbText,omitempty"`
}

type ConfigDockerDashboardSections struct {
	Overview  bool `json:"overview"`
	Daemon    bool `json:"daemon"`
	Resources bool `json:"resources"`
}

type ConfigHardwareSections struct {
	Overview      bool `json:"overview"`
	Hardware      bool `json:"hardware"`
	Sensors       bool `json:"sensors"`
	SystemInfo    bool `json:"systemInfo"`
	GPU           bool `json:"gpu"`
	PCIDevices    bool `json:"pciDevices"`
	MemoryModules bool `json:"memoryModules"`
}

type ConfigDockerPayload struct {
	Folders []string                  `json:"folders,omitempty"`
	Proxy   *ConfigDockerProxyPayload `json:"proxy,omitempty"`
}

type ConfigDockerProxyPayload struct {
	CaddyEnabled *bool   `json:"caddyEnabled,omitempty"`
	BaseDomain   *string `json:"baseDomain,omitempty"`
	TLSEmail     *string `json:"tlsEmail,omitempty"`
}

type ConfigJobSettingsPayload struct {
	ProgressMinIntervalMs     *int `json:"progressMinIntervalMs,omitempty"`
	NotificationMinIntervalMs *int `json:"notificationMinIntervalMs,omitempty"`
	ProgressMinBytesMB        *int `json:"progressMinBytesMB,omitempty"`
	HeavyArchiveConcurrency   *int `json:"heavyArchiveConcurrency,omitempty"`
	ArchiveCompressionWorkers *int `json:"archiveCompressionWorkers,omitempty"`
	ArchiveExtractWorkers     *int `json:"archiveExtractWorkers,omitempty"`
}

type ConfigDismissalsPayload struct {
	UncleanShutdownBootID *string `json:"uncleanShutdownBootId,omitempty"`
	FailedLoginAlertID    *string `json:"failedLoginAlertId,omitempty"`
}

type DockerComposeRequest struct {
	Action      string  `json:"action"`
	ProjectName string  `json:"projectName"`
	ComposePath *string `json:"composePath,omitempty"`
}

type TerminalOpenRequest struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

type ContainerOpenRequest struct {
	ContainerID string `json:"containerId"`
	Shell       string `json:"shell"`
	Cols        int    `json:"cols"`
	Rows        int    `json:"rows"`
}

type AppUpdateRequest struct {
	RunID   string  `json:"runId"`
	Version *string `json:"version,omitempty"`
}

type ServiceLogsFollowRequest struct {
	ServiceName string  `json:"serviceName"`
	Lines       *string `json:"lines,omitempty"`
}

type GeneralLogsFollowRequest struct {
	Lines        *string  `json:"lines,omitempty"`
	TimePeriod   *string  `json:"timePeriod,omitempty"`
	Priority     *string  `json:"priority,omitempty"`
	Identifier   *string  `json:"identifier,omitempty"`
	FieldFilters []string `json:"fieldFilters,omitempty"`
}

type DockerSystemPruneRequest struct {
	Containers bool `json:"containers"`
	Images     bool `json:"images"`
	BuildCache bool `json:"buildCache"` //nolint:tagliatelle
	Networks   bool `json:"networks"`
	Volumes    bool `json:"volumes"`
}

type DockerSystemPruneResponse struct {
	ContainersDeleted []string `json:"containersDeleted,omitempty"`
	ImagesDeleted     []string `json:"imagesDeleted,omitempty"`
	NetworksDeleted   []string `json:"networksDeleted,omitempty"`
	VolumesDeleted    []string `json:"volumesDeleted,omitempty"`
	SpaceReclaimed    uint64   `json:"spaceReclaimed"`
}

type FileArchiveRequest struct {
	Format string   `json:"format"`
	Paths  []string `json:"paths"`
}

type FileChmodRequest struct {
	Path      string `json:"path"`
	Mode      string `json:"mode"`
	Owner     string `json:"owner"`
	Group     string `json:"group"`
	Recursive *bool  `json:"recursive,omitempty"`
}

type FileCompressRequest struct {
	Format     string   `json:"format"`
	TargetPath string   `json:"targetPath"`
	Paths      []string `json:"paths"`
}

type FileExtractRequest struct {
	ArchivePath string  `json:"archivePath"`
	Destination *string `json:"destination,omitempty"`
}

type OptionalPathRequest struct {
	Path *string `json:"path,omitempty"`
}

type FileResourceGetRequest struct {
	Path       string  `json:"path"`
	Unused     *string `json:"unused,omitempty"`
	GetContent *string `json:"getContent,omitempty"`
}

type FileResourcePostRequest struct {
	Path     string `json:"path"`
	Override *bool  `json:"override,omitempty"`
}

type FileSearchRequest struct {
	Query    string  `json:"query"`
	Limit    *string `json:"limit,omitempty"`
	BasePath *string `json:"basePath,omitempty"`
}

type FileUploadRequest struct {
	TargetPath string `json:"targetPath"`
	Size       string `json:"size"`
	Overwrite  *bool  `json:"overwrite,omitempty"`
}

type IndexerConfigPatch struct {
	IndexPath            *string `json:"index_path,omitempty"`
	IndexName            *string `json:"index_name,omitempty"`
	IncludeHidden        *bool   `json:"include_hidden,omitempty"`
	IncludeNetworkMounts *bool   `json:"include_network_mounts,omitempty"`
	FreshIndex           *bool   `json:"fresh_index,omitempty"`
	KeepIndexes          *int    `json:"keep_indexes,omitempty"`
	DBPath               *string `json:"db_path,omitempty"`
	DBBusyTimeout        *string `json:"db_busy_timeout,omitempty"`
	DBJournalMode        *string `json:"db_journal_mode,omitempty"`
	DBSynchronous        *string `json:"db_synchronous,omitempty"`
	DBAutoVacuum         *string `json:"db_auto_vacuum,omitempty"`
	DBMaxOpenConns       *int    `json:"db_max_open_conns,omitempty"`
	DBMaxIdleConns       *int    `json:"db_max_idle_conns,omitempty"`
	DBConnMaxIdleTime    *string `json:"db_conn_max_idle_time,omitempty"`
	SocketPath           *string `json:"socket_path,omitempty"`
	ListenAddr           *string `json:"listen_addr,omitempty"`
	Interval             *string `json:"interval,omitempty"`
}

type JobListRequest struct {
	Status *string `json:"status,omitempty"`
}

type NFSClient struct {
	Host    string   `json:"host"`
	Options []string `json:"options"`
}

type ShareNFSRequest struct {
	Path    string      `json:"path"`
	Clients []NFSClient `json:"clients"`
}

type ShareSambaRequest struct {
	Name       string            `json:"name"`
	Properties map[string]string `json:"properties"`
}

type ShareUpdateSambaRequest struct {
	OldName    string            `json:"oldName"`
	NewName    string            `json:"newName"`
	Properties map[string]string `json:"properties"`
}

type FailedLoginEventsRequest struct {
	Limit *string `json:"limit,omitempty"`
}

type UpdatesSetAutoUpdatesRequest struct {
	Enabled         bool     `json:"enabled"`
	Frequency       string   `json:"frequency"`
	Scope           string   `json:"scope"`
	DownloadOnly    bool     `json:"download_only"`
	RebootPolicy    string   `json:"reboot_policy"`
	ExcludePackages []string `json:"exclude_packages"`
}
