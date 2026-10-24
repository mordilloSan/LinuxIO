package apischema

import (
	"github.com/mordilloSan/LinuxIO/backend/bridge/internal/bridgeipc"
	"github.com/mordilloSan/LinuxIO/backend/common/dockerupdate"
)

// The Docker update wire types are owned by the update engine, which also
// persists and returns them from the root worker. They are aliases so the
// generated contract still names them by their Go type name.
type (
	DockerContainerAutoUpdateMode    = dockerupdate.DockerContainerAutoUpdateMode
	DockerUpdateCheckState           = dockerupdate.DockerUpdateCheckState
	DockerContainerAutoUpdateOptions = dockerupdate.DockerContainerAutoUpdateOptions
	DockerContainerAutoUpdateTarget  = dockerupdate.DockerContainerAutoUpdateTarget
	DockerContainerAutoUpdateState   = dockerupdate.DockerContainerAutoUpdateState
	DockerContainerUpdateRequest     = dockerupdate.DockerContainerUpdateRequest
	DockerUpdateCheckResult          = dockerupdate.DockerUpdateCheckResult
	DockerContainerUpdateResult      = dockerupdate.DockerContainerUpdateResult
)

type ContainerMetricsStatus string
type ValidationIssueType string

const (
	DockerUpdateCheckStateCurrent     = dockerupdate.DockerUpdateCheckStateCurrent
	DockerUpdateCheckStateAvailable   = dockerupdate.DockerUpdateCheckStateAvailable
	DockerUpdateCheckStateUncheckable = dockerupdate.DockerUpdateCheckStateUncheckable
	DockerUpdateCheckStateError       = dockerupdate.DockerUpdateCheckStateError
)

const (
	ContainerMetricsStatusAvailable   ContainerMetricsStatus = "available"
	ContainerMetricsStatusStale       ContainerMetricsStatus = "stale"
	ContainerMetricsStatusUnavailable ContainerMetricsStatus = "unavailable"
	ContainerMetricsStatusNotRunning  ContainerMetricsStatus = "not_running"
)

type ContainerPort struct {
	IP          *string `json:"IP,omitempty"`
	PrivatePort int     `json:"PrivatePort"`
	PublicPort  *int    `json:"PublicPort,omitempty"`
	Type        string  `json:"Type"`
}

type ContainerMount struct {
	Destination string `json:"Destination"`
	Driver      string `json:"Driver,omitempty"`
	Mode        string `json:"Mode"`
	Name        string `json:"Name,omitempty"`
	Propagation string `json:"Propagation,omitempty"`
	RW          bool   `json:"RW"`
	Source      string `json:"Source"`
	Type        string `json:"Type"`
}

type ContainerEndpoint struct {
	Aliases           []string `json:"Aliases,omitempty"`
	Gateway           string   `json:"Gateway"`
	GlobalIPv6Address *string  `json:"GlobalIPv6Address,omitempty"`
	IPAddress         string   `json:"IPAddress"`
	MACAddress        *string  `json:"MacAddress,omitempty"`
}

type ContainerMetrics struct {
	BlockReadBytesPerSecond      *float64               `json:"block_read_bytes_per_second,omitempty"`
	BlockWriteBytesPerSecond     *float64               `json:"block_write_bytes_per_second,omitempty"`
	CapturedAtMs                 *int64                 `json:"captured_at_ms,omitempty"`
	CPUPercent                   *float64               `json:"cpu_percent,omitempty"`
	MemoryLimitBytes             *uint64                `json:"memory_limit_bytes,omitempty"`
	MemoryUsageBytes             *uint64                `json:"memory_usage_bytes,omitempty"`
	NetworkReceiveBytesPerSecond *float64               `json:"network_receive_bytes_per_second,omitempty"`
	NetworkSendBytesPerSecond    *float64               `json:"network_send_bytes_per_second,omitempty"`
	Status                       ContainerMetricsStatus `json:"status"`
}

type ContainerHostConfig struct {
	NetworkMode *string `json:"NetworkMode,omitempty"`
}

type ContainerNetworkSettings struct {
	Networks map[string]ContainerEndpoint `json:"Networks,omitempty"`
}

type ContainerInfo struct {
	Created           int64                     `json:"Created"`
	HostConfig        *ContainerHostConfig      `json:"HostConfig,omitempty"`
	Icon              *string                   `json:"icon,omitempty"`
	ID                string                    `json:"Id"`
	Image             string                    `json:"Image"`
	Labels            map[string]string         `json:"Labels,omitempty"`
	Metrics           *ContainerMetrics         `json:"metrics,omitempty"`
	Mounts            []ContainerMount          `json:"Mounts,omitempty"`
	Names             []string                  `json:"Names"`
	NetworkSettings   *ContainerNetworkSettings `json:"NetworkSettings,omitempty"`
	Ports             []ContainerPort           `json:"Ports,omitempty"`
	State             string                    `json:"State"`
	Status            string                    `json:"Status"`
	UpdateAvailable   *bool                     `json:"updateAvailable,omitempty"`
	UpdateCheckedAt   *int64                    `json:"updateCheckedAt,omitempty"`
	UpdateCheckReason *string                   `json:"updateCheckReason,omitempty"`
	UpdateCheckState  *DockerUpdateCheckState   `json:"updateCheckState,omitempty"`
	UpdateError       *string                   `json:"updateError,omitempty"`
	URL               *string                   `json:"url,omitempty"`
}

type ContainerInspectState struct {
	Dead       bool   `json:"dead"`
	Error      string `json:"error"`
	ExitCode   int    `json:"exitCode"`
	FinishedAt string `json:"finishedAt"`
	OOMKilled  bool   `json:"oomKilled"`
	Paused     bool   `json:"paused"`
	Restarting bool   `json:"restarting"`
	Running    bool   `json:"running"`
	StartedAt  string `json:"startedAt"`
	Status     string `json:"status"`
}

type ContainerInspectHealth struct {
	FailingStreak int    `json:"failingStreak"`
	Status        string `json:"status"`
}

type ContainerRestartPolicy struct {
	MaximumRetryCount int    `json:"maximumRetryCount"`
	Name              string `json:"name"`
}

type ContainerEnvironmentVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type ContainerPortBinding struct {
	ContainerPort int    `json:"containerPort"`
	HostIP        string `json:"hostIp"`
	HostPort      string `json:"hostPort"`
	Protocol      string `json:"protocol"`
}

type ContainerInspectInfo struct {
	Command          []string                       `json:"command,omitempty"`
	Created          string                         `json:"created"`
	Entrypoint       []string                       `json:"entrypoint,omitempty"`
	Environment      []ContainerEnvironmentVariable `json:"environment,omitempty"`
	Health           *ContainerInspectHealth        `json:"health,omitempty"`
	ID               string                         `json:"id"`
	Image            string                         `json:"image"`
	ImageID          string                         `json:"imageId"`
	Labels           map[string]string              `json:"labels,omitempty"`
	Mounts           []ContainerMount               `json:"mounts,omitempty"`
	Name             string                         `json:"name"`
	Networks         map[string]ContainerEndpoint   `json:"networks,omitempty"`
	Ports            []ContainerPortBinding         `json:"ports,omitempty"`
	RestartCount     int                            `json:"restartCount"`
	RestartPolicy    ContainerRestartPolicy         `json:"restartPolicy"`
	State            ContainerInspectState          `json:"state"`
	User             string                         `json:"user"`
	WorkingDirectory string                         `json:"workingDirectory"`
}

type ContainerConfigurationResult struct {
	ContainerID string `json:"containerId"`
	Name        string `json:"name"`
}

type DockerImage struct {
	Containers        *int                    `json:"Containers,omitempty"`
	Created           int64                   `json:"Created"`
	ID                string                  `json:"Id"`
	Labels            map[string]string       `json:"Labels,omitempty"`
	RepoDigests       []string                `json:"RepoDigests,omitempty"`
	RepoTags          []string                `json:"RepoTags"`
	Size              int64                   `json:"Size"`
	UpdateAvailable   *bool                   `json:"updateAvailable,omitempty"`
	UpdateCheckReason *string                 `json:"updateCheckReason,omitempty"`
	UpdateCheckState  *DockerUpdateCheckState `json:"updateCheckState,omitempty"`
}

type DockerNetworkContainer struct {
	EndpointID  string  `json:"EndpointID,omitempty"`
	Name        string  `json:"Name"`
	IPv4Address *string `json:"IPv4Address,omitempty"`
	IPv6Address *string `json:"IPv6Address,omitempty"`
	MACAddress  *string `json:"MacAddress,omitempty"`
}

type DockerNetworkIPAMConfig struct {
	AuxiliaryAddresses map[string]string `json:"AuxiliaryAddresses,omitempty"`
	Gateway            string            `json:"Gateway"`
	IPRange            string            `json:"IPRange,omitempty"`
	Subnet             string            `json:"Subnet"`
}

type DockerNetworkIPAM struct {
	Config  []DockerNetworkIPAMConfig `json:"Config,omitempty"`
	Driver  string                    `json:"Driver,omitempty"`
	Options map[string]string         `json:"Options,omitempty"`
}

type DockerNetwork struct {
	Attachable bool                              `json:"Attachable"`
	ConfigOnly bool                              `json:"ConfigOnly"`
	Containers map[string]DockerNetworkContainer `json:"Containers,omitempty"`
	Created    *string                           `json:"Created,omitempty"`
	Driver     string                            `json:"Driver"`
	EnableIPv4 *bool                             `json:"EnableIPv4,omitempty"`
	EnableIPv6 *bool                             `json:"EnableIPv6,omitempty"`
	ID         string                            `json:"Id"`
	Internal   *bool                             `json:"Internal,omitempty"`
	IPAM       *DockerNetworkIPAM                `json:"IPAM,omitempty"`
	Ingress    bool                              `json:"Ingress"`
	Labels     map[string]string                 `json:"Labels,omitempty"`
	Name       string                            `json:"Name"`
	Options    map[string]string                 `json:"Options,omitempty"`
	Protected  bool                              `json:"Protected,omitzero"`
	Scope      string                            `json:"Scope"`
}

type DockerVolumeUsageData struct {
	RefCount int64 `json:"RefCount"`
	Size     int64 `json:"Size"`
}

type DockerVolumeContainer struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	State string `json:"State"`
}

type DockerVolume struct {
	ClusterVolume        map[string]any          `json:"ClusterVolume,omitempty"`
	Containers           []DockerVolumeContainer `json:"Containers,omitempty"`
	CreatedAt            *string                 `json:"CreatedAt,omitempty"`
	Driver               string                  `json:"Driver"`
	Labels               map[string]string       `json:"Labels,omitempty"`
	Mountpoint           string                  `json:"Mountpoint"`
	MountpointAccessible bool                    `json:"MountpointAccessible,omitzero"`
	Name                 string                  `json:"Name"`
	Options              map[string]string       `json:"Options,omitempty"`
	Scope                *string                 `json:"Scope,omitempty"`
	Status               map[string]any          `json:"Status,omitempty"`
	UsageData            *DockerVolumeUsageData  `json:"UsageData,omitempty"`
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

// DockerDaemonConfig is the subset of /etc/docker/daemon.json LinuxIO edits.
// Keys the form does not cover are preserved untouched on save.
type DockerDaemonConfig struct {
	AddressPoolBase string `json:"address_pool_base"`
	AddressPoolSize int    `json:"address_pool_size"`
	LiveRestore     bool   `json:"live_restore"`
	LogMaxFile      string `json:"log_max_file"`
	LogMaxSize      string `json:"log_max_size"`
	UserlandProxy   bool   `json:"userland_proxy"`
}

type DockerDaemonConfigSetResult struct {
	Config          DockerDaemonConfig `json:"config"`
	RestartRequired bool               `json:"restart_required"`
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

type ComposeActionResult struct {
	Message string `json:"message"`
	Output  string `json:"output"`
}

type DeleteStackResult struct {
	DeletedPath  string `json:"deleted_path"`
	DirDeleted   bool   `json:"dir_deleted"`
	FilesDeleted bool   `json:"files_deleted"`
	Message      string `json:"message"`
	Project      string `json:"project"`
}

type DirectoryValidationResult struct {
	CanCreate   bool   `json:"canCreate"`
	CanWrite    bool   `json:"canWrite"`
	Error       string `json:"error,omitempty"`
	Exists      bool   `json:"exists"`
	IsDirectory bool   `json:"isDirectory"`
	Valid       bool   `json:"valid"`
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
	Line    int                 `json:"line,omitzero"`
	Column  int                 `json:"column,omitzero"`
	Field   string              `json:"field,omitempty"`
	Message string              `json:"message"`
	Type    ValidationIssueType `json:"type"`
}

type ValidateComposeResponse struct {
	Valid             bool                   `json:"valid"`
	Errors            []ValidateComposeError `json:"errors"`
	NormalizedContent string                 `json:"normalized_content,omitempty"`
}

type DockerSettings struct {
	Folders                 []string `json:"folders"`
	RequireMountsForFolders bool     `json:"requireMountsForFolders"`
}

type ContainerIDRequest struct {
	ContainerID string `json:"containerId"`
}

type ContainerRemoveRequest struct {
	ContainerID string `json:"containerId"`
	Force       bool   `json:"force"`
}

type ContainerConfiguration struct {
	Name             string                         `json:"name"`
	Image            string                         `json:"image"`
	Command          []string                       `json:"command"`
	Entrypoint       []string                       `json:"entrypoint"`
	Environment      []ContainerEnvironmentVariable `json:"environment"`
	Ports            []ContainerPortBinding         `json:"ports"`
	Mounts           []ContainerMountConfiguration  `json:"mounts"`
	Networks         []ContainerNetworkAttachment   `json:"networks"`
	RestartPolicy    ContainerRestartPolicy         `json:"restartPolicy"`
	User             string                         `json:"user"`
	WorkingDirectory string                         `json:"workingDirectory"`
}

type ContainerMountConfiguration struct {
	Type        string `json:"type"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	ReadOnly    bool   `json:"readOnly"`
}

type ContainerNetworkAttachment struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
}

type ContainerCreateRequest struct {
	Configuration ContainerConfiguration `json:"configuration"`
	Start         bool                   `json:"start"`
}

type ContainerEditRequest struct {
	ContainerID   string                 `json:"containerId"`
	Configuration ContainerConfiguration `json:"configuration"`
}

type DockerVolumeCreateRequest struct {
	Name   string            `json:"name"`
	Driver string            `json:"driver"`
	Labels map[string]string `json:"labels,omitempty"`
}

type DockerNetworkCreateRequest struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Internal   bool              `json:"internal"`
	Attachable bool              `json:"attachable"`
	EnableIPv6 bool              `json:"enableIpv6"`
	Subnet     string            `json:"subnet,omitempty"`
	Gateway    string            `json:"gateway,omitempty"`
	Options    map[string]string `json:"options,omitempty"`
}

type DockerNetworkConnectRequest struct {
	NetworkID   string   `json:"networkId"`
	ContainerID string   `json:"containerId"`
	Aliases     []string `json:"aliases,omitempty"`
}

type DockerNetworkDisconnectRequest struct {
	NetworkID   string `json:"networkId"`
	ContainerID string `json:"containerId"`
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

type ValidateComposeRequest struct {
	Content    string `json:"content"`
	EnvContent string `json:"envContent,omitempty"`
	WorkingDir string `json:"workingDir,omitempty"`
}

type DirPathRequest struct {
	DirPath string `json:"dirPath"`
}

type DockerContainerUpdateProgress struct {
	Phase   string `json:"phase"`
	Message string `json:"message"`
}

func (p DockerContainerUpdateProgress) ProgressEnvelope() bridgeipc.TaskProgress {
	return bridgeipc.TaskProgress{Phase: p.Phase, Message: p.Message, Detail: p}
}

type DockerLogsFollowRequest struct {
	ContainerID string  `json:"containerId"`
	Tail        *string `json:"tail,omitempty"`
}

type DockerLogsFollowAllRequest struct {
	Tail *string `json:"tail,omitempty"`
}

type DockerStartedFailedResponse struct {
	Started int `json:"started"`
	Failed  int `json:"failed"`
}

type DockerStoppedFailedResponse struct {
	Stopped int `json:"stopped"`
	Failed  int `json:"failed"`
}

type DockerComposeRequest struct {
	Action      string  `json:"action"`
	ProjectName string  `json:"projectName"`
	ComposePath *string `json:"composePath,omitempty"`
}

type DockerSystemPruneRequest struct {
	Containers bool `json:"containers"`
	Images     bool `json:"images"`
	BuildCache bool `json:"buildCache"`
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
