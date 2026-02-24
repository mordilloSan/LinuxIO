package docker

import (
	"context"
	"fmt"
	"sort"
	"syscall"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/mordilloSan/go-logger/logger"
)

// DockerSystemInfo holds the flattened Docker daemon system and version info.
type DockerSystemInfo struct {
	// System
	Name            string `json:"name"`
	ID              string `json:"id"`
	OperatingSystem string `json:"operating_system"`
	OSType          string `json:"os_type"`
	Architecture    string `json:"architecture"`
	KernelVersion   string `json:"kernel_version"`
	SystemTime      string `json:"system_time"`
	DockerRootDir   string `json:"docker_root_dir"`
	NCPU            int    `json:"ncpu"`
	MemTotal        int64  `json:"mem_total"`

	// Version
	ServerVersion string `json:"server_version"`
	APIVersion    string `json:"api_version"`
	GoVersion     string `json:"go_version"`
	GitCommit     string `json:"git_commit"`
	BuildTime     string `json:"build_time"`
	Experimental  bool   `json:"experimental"`

	// Configuration
	StorageDriver  string `json:"storage_driver"`
	LoggingDriver  string `json:"logging_driver"`
	CgroupDriver   string `json:"cgroup_driver"`
	CgroupVersion  string `json:"cgroup_version"`
	InitBinary     string `json:"init_binary"`
	DefaultRuntime string `json:"default_runtime"`

	// Network & Proxy
	IPv4Forwarding bool   `json:"ipv4_forwarding"`
	HTTPProxy      string `json:"http_proxy"`
	HTTPSProxy     string `json:"https_proxy"`
	NoProxy        string `json:"no_proxy"`

	// Security & Runtimes
	SecurityOptions []string `json:"security_options"`
	Runtimes        []string `json:"runtimes"`

	// Plugins
	VolumePlugins  []string `json:"volume_plugins"`
	NetworkPlugins []string `json:"network_plugins"`
	LogPlugins     []string `json:"log_plugins"`

	// Disk
	DiskUsed  int64 `json:"disk_used"`
	DiskTotal int64 `json:"disk_total"`
}

// GetDockerInfo returns combined system and version information from the Docker daemon.
func GetDockerInfo() (*DockerSystemInfo, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer func() {
		if cerr := cli.Close(); cerr != nil {
			logger.Warnf("failed to close Docker client: %v", cerr)
		}
	}()

	ctx := context.Background()

	info, err := cli.Info(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get docker info: %w", err)
	}

	version, err := cli.ServerVersion(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get docker version: %w", err)
	}

	// Collect runtime names, sorted for stable output.
	runtimes := make([]string, 0, len(info.Runtimes))
	for name := range info.Runtimes {
		runtimes = append(runtimes, name)
	}
	sort.Strings(runtimes)

	result := &DockerSystemInfo{
		// System
		Name:            info.Name,
		ID:              info.ID,
		OperatingSystem: info.OperatingSystem,
		OSType:          info.OSType,
		Architecture:    info.Architecture,
		KernelVersion:   info.KernelVersion,
		SystemTime:      info.SystemTime,
		DockerRootDir:   info.DockerRootDir,
		NCPU:            info.NCPU,
		MemTotal:        info.MemTotal,

		// Version
		ServerVersion: version.Version,
		APIVersion:    version.APIVersion,
		GoVersion:     version.GoVersion,
		GitCommit:     version.GitCommit,
		BuildTime:     version.BuildTime,
		Experimental:  version.Experimental,

		// Configuration
		StorageDriver:  info.Driver,
		LoggingDriver:  info.LoggingDriver,
		CgroupDriver:   info.CgroupDriver,
		CgroupVersion:  info.CgroupVersion,
		InitBinary:     info.InitBinary,
		DefaultRuntime: info.DefaultRuntime,

		// Network & Proxy
		IPv4Forwarding: info.IPv4Forwarding,
		HTTPProxy:      info.HTTPProxy,
		HTTPSProxy:     info.HTTPSProxy,
		NoProxy:        info.NoProxy,

		// Security & Runtimes
		SecurityOptions: info.SecurityOptions,
		Runtimes:        runtimes,

		// Plugins
		VolumePlugins:  info.Plugins.Volume,
		NetworkPlugins: info.Plugins.Network,
		LogPlugins:     info.Plugins.Log,
	}

	// Disk usage: sum image layers + build cache.
	if du, err := cli.DiskUsage(ctx, dockertypes.DiskUsageOptions{}); err == nil {
		result.DiskUsed = du.LayersSize
		for _, bc := range du.BuildCache {
			result.DiskUsed += int64(bc.Size)
		}
	}

	// Filesystem capacity for the Docker root dir.
	var fsStat syscall.Statfs_t
	if err := syscall.Statfs(info.DockerRootDir, &fsStat); err == nil {
		result.DiskTotal = int64(fsStat.Blocks) * int64(fsStat.Bsize)
	}

	return result, nil
}
