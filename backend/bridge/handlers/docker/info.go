package docker

import (
	"context"
	"fmt"
	"sort"
	"syscall"

	"github.com/moby/moby/api/types/system"
	"github.com/moby/moby/client"

	"github.com/mordilloSan/LinuxIO/backend/bridge/apischema"
)

// GetDockerInfo returns combined system and version information from the Docker daemon.
func GetDockerInfo(ctx context.Context) (*apischema.DockerSystemInfo, error) {
	cli, err := getClient()
	if err != nil {
		return nil, fmt.Errorf("docker client error: %w", err)
	}
	defer releaseClient(cli)

	infoResult, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get docker info: %w", err)
	}
	info := infoResult.Info

	version, err := cli.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get docker version: %w", err)
	}
	goVersion, gitCommit, buildTime := dockerVersionDetails(version.Components)
	experimental := false
	if ping, err := cli.Ping(ctx, client.PingOptions{}); err == nil {
		experimental = ping.Experimental
	}

	// Collect runtime names, sorted for stable output.
	runtimes := make([]string, 0, len(info.Runtimes))
	for name := range info.Runtimes {
		runtimes = append(runtimes, name)
	}
	sort.Strings(runtimes)

	result := &apischema.DockerSystemInfo{
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
		MemTotal:        nonNegativeUint64(info.MemTotal),

		// Version
		ServerVersion: version.Version,
		APIVersion:    version.APIVersion,
		GoVersion:     goVersion,
		GitCommit:     gitCommit,
		BuildTime:     buildTime,
		Experimental:  experimental,

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
	if du, err := cli.DiskUsage(ctx, client.DiskUsageOptions{Images: true, BuildCache: true}); err == nil {
		result.DiskUsed = nonNegativeUint64(du.Images.TotalSize + du.BuildCache.TotalSize)
	}

	// Filesystem capacity for the Docker root dir.
	var fsStat syscall.Statfs_t
	if err := syscall.Statfs(info.DockerRootDir, &fsStat); err == nil {
		result.DiskTotal = fsStat.Blocks * uint64(fsStat.Bsize)
	}

	return result, nil
}

func nonNegativeUint64(value int64) uint64 {
	if value < 0 {
		return 0
	}
	return uint64(value)
}

func dockerVersionDetails(components []system.ComponentVersion) (goVersion, gitCommit, buildTime string) {
	for _, component := range components {
		if component.Name != "Engine" && component.Name != "Docker Engine" {
			continue
		}
		goVersion = component.Details["GoVersion"]
		gitCommit = component.Details["GitCommit"]
		buildTime = component.Details["BuildTime"]
		return goVersion, gitCommit, buildTime
	}
	return "", "", ""
}
