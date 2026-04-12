package main

import (
	"encoding/json"
	"maps"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mordilloSan/go-logger/logger"

	commonconfig "github.com/mordilloSan/LinuxIO/backend/common/config"
	internalpcp "github.com/mordilloSan/LinuxIO/backend/common/pcp"
	commonpcpapi "github.com/mordilloSan/LinuxIO/backend/common/pcpapi"

	bridgeSystem "github.com/mordilloSan/LinuxIO/backend/bridge/handlers/system"
)

var (
	networkIncludeRE = regexp.MustCompile(`^(en|eth|wl|ww|bond|team|br|wg|tun|tap|tailscale|zt).*$`)
	networkExcludeRE = regexp.MustCompile(`^(lo|veth.*|docker.*)$`)
	diskDeviceRE     = regexp.MustCompile(`^(sd[a-z]+|hd[a-z]+|vd[a-z]+|xvd[a-z]+|nvme[0-9]+n[0-9]+|mmcblk[0-9]+)$`)
)

type runtimeState struct {
	Config commonpcpapi.Config
	Token  string
}

type app struct {
	collector *internalpcp.LiveCollector
	runtime   atomic.Pointer[runtimeState]

	cpuRates           cpuRateStore
	networkRates       ioRateStore
	diskRates          ioRateStore
	summaryCPURates    cpuRateStore
	summaryNetworkRate ioRateStore
	summaryDiskRate    ioRateStore
}

type envelope struct {
	TSMS      int64  `json:"ts_ms"`
	WindowMS  int64  `json:"window_ms,omitempty"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type loadAverages struct {
	Load1  float64 `json:"load1"`
	Load5  float64 `json:"load5"`
	Load15 float64 `json:"load15"`
}

type cpuEnvelope struct {
	envelope
	CPU cpuPayload `json:"cpu"`
}

type cpuPayload struct {
	Cores           int                `json:"cores"`
	UsagePercent    float64            `json:"usage_percent"`
	Load            loadAverages       `json:"load"`
	PerCPUUsage     map[string]float64 `json:"percpu_usage_percent,omitempty"`
	RawIdleMillis   uint64             `json:"raw_idle_millis,omitempty"`
	RawPerCPUIDLEMS map[string]uint64  `json:"raw_percpu_idle_millis,omitempty"`
}

type memoryEnvelope struct {
	envelope
	Memory memoryPayload `json:"memory"`
}

type memoryPayload struct {
	TotalBytes      uint64  `json:"total_bytes"`
	AvailableBytes  uint64  `json:"available_bytes"`
	UsedBytes       uint64  `json:"used_bytes"`
	UsedPercent     float64 `json:"used_percent"`
	SwapTotalBytes  uint64  `json:"swap_total_bytes"`
	SwapFreeBytes   uint64  `json:"swap_free_bytes"`
	SwapUsedBytes   uint64  `json:"swap_used_bytes"`
	SwapUsedPercent float64 `json:"swap_used_percent"`
	RawTotalKib     uint64  `json:"raw_total_kib,omitempty"`
	RawAvailableKib uint64  `json:"raw_available_kib,omitempty"`
	RawSwapTotalKib uint64  `json:"raw_swap_total_kib,omitempty"`
	RawSwapFreeKib  uint64  `json:"raw_swap_free_kib,omitempty"`
}

type networkEnvelope struct {
	envelope
	Network networkPayload `json:"network"`
}

type networkPayload struct {
	Interfaces []networkInterfacePayload `json:"interfaces"`
}

type networkInterfacePayload struct {
	Name          string  `json:"name"`
	RXBytesPerSec float64 `json:"rx_bytes_per_sec"`
	TXBytesPerSec float64 `json:"tx_bytes_per_sec"`
	RXTotalBytes  uint64  `json:"rx_total_bytes"`
	TXTotalBytes  uint64  `json:"tx_total_bytes"`
}

type diskEnvelope struct {
	envelope
	Disk diskPayload `json:"disk"`
}

type diskPayload struct {
	Devices []diskDevicePayload `json:"devices"`
}

type diskDevicePayload struct {
	Name             string  `json:"name"`
	ReadBytesPerSec  float64 `json:"read_bytes_per_sec"`
	WriteBytesPerSec float64 `json:"write_bytes_per_sec"`
	ReadTotalBytes   uint64  `json:"read_total_bytes"`
	WriteTotalBytes  uint64  `json:"write_total_bytes"`
}

type filesystemsEnvelope struct {
	envelope
	Filesystems filesystemsPayload `json:"filesystems"`
}

type filesystemsPayload struct {
	Mounts []filesystemPayload `json:"mounts"`
}

type filesystemPayload struct {
	Device      string  `json:"device"`
	Mountpoint  string  `json:"mountpoint"`
	FSType      string  `json:"fstype"`
	TotalBytes  uint64  `json:"total_bytes"`
	FreeBytes   uint64  `json:"free_bytes"`
	UsedBytes   uint64  `json:"used_bytes"`
	UsedPercent float64 `json:"used_percent"`
	ReadOnly    bool    `json:"read_only"`
	Source      string  `json:"source"`
}

type thermalEnvelope struct {
	envelope
	Thermal thermalPayload `json:"thermal"`
}

type thermalPayload struct {
	Groups []bridgeSystem.SensorGroup `json:"groups"`
}

type systemEnvelope struct {
	envelope
	System systemPayload `json:"system"`
}

type systemPayload struct {
	Hostname        string                   `json:"hostname"`
	Platform        string                   `json:"platform"`
	PlatformVersion string                   `json:"platform_version"`
	KernelVersion   string                   `json:"kernel_version"`
	KernelArch      string                   `json:"kernel_arch"`
	UptimeSeconds   uint64                   `json:"uptime_seconds"`
	Load            loadAverages             `json:"load"`
	SystemInfo      *bridgeSystem.SystemInfo `json:"system_info,omitempty"`
}

type summaryEnvelope struct {
	envelope
	Summary summaryPayload `json:"summary"`
}

type summaryPayload struct {
	Hostname             string       `json:"hostname"`
	UptimeSeconds        uint64       `json:"uptime_seconds"`
	Load                 loadAverages `json:"load"`
	CPUUsagePercent      float64      `json:"cpu_usage_percent"`
	CPUSummary           string       `json:"cpu_summary,omitempty"`
	CPUTemperatureC      *float64     `json:"cpu_temperature_c,omitempty"`
	MemoryUsedPercent    float64      `json:"memory_used_percent"`
	MemoryUsedBytes      uint64       `json:"memory_used_bytes"`
	MemoryTotalBytes     uint64       `json:"memory_total_bytes"`
	NetworkRXBytesPerSec float64      `json:"network_rx_bytes_per_sec"`
	NetworkTXBytesPerSec float64      `json:"network_tx_bytes_per_sec"`
	DiskReadBytesPerSec  float64      `json:"disk_read_bytes_per_sec"`
	DiskWriteBytesPerSec float64      `json:"disk_write_bytes_per_sec"`
}

type healthResponse struct {
	OK            bool   `json:"ok"`
	TSMS          int64  `json:"ts_ms"`
	Version       string `json:"version"`
	Enabled       bool   `json:"enabled"`
	ListenAddress string `json:"listen_address"`
}

type versionResponse struct {
	Component string `json:"component"`
	Version   string `json:"version"`
	CommitSHA string `json:"commit_sha,omitempty"`
	BuildTime string `json:"build_time,omitempty"`
}

type cpuSnapshot struct {
	ts     time.Time
	idle   float64
	perCPU map[string]float64
}

type cpuRateStore struct {
	mu   sync.Mutex
	prev cpuSnapshot
}

type ioSnapshot struct {
	ts     time.Time
	first  map[string]float64
	second map[string]float64
}

type ioRateStore struct {
	mu   sync.Mutex
	prev ioSnapshot
}

type ioRate struct {
	FirstRate   float64
	SecondRate  float64
	FirstTotal  uint64
	SecondTotal uint64
}

func newApp(collector *internalpcp.LiveCollector, cfg commonpcpapi.Config, token string) *app {
	instance := &app{collector: collector}
	instance.runtime.Store(&runtimeState{Config: cfg, Token: token})
	return instance
}

func (a *app) currentRuntime() runtimeState {
	state := a.runtime.Load()
	if state == nil {
		return runtimeState{Config: commonpcpapi.DefaultConfig()}
	}
	return *state
}

func (a *app) reloadRuntime() error {
	current := a.currentRuntime()
	cfg, err := commonpcpapi.ReadConfig(commonpcpapi.DefaultConfigPath)
	if err != nil {
		return err
	}
	token, err := commonpcpapi.ReadToken(cfg.Auth.TokenFile)
	if err != nil {
		return err
	}

	if cfg.ListenAddress != current.Config.ListenAddress {
		logger.Warnf("listen_address changed from %s to %s; restart required to apply", current.Config.ListenAddress, cfg.ListenAddress)
	}

	a.runtime.Store(&runtimeState{Config: cfg, Token: token})
	logger.Infof("reloaded runtime config")
	return nil
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", a.handleHealth)
	mux.HandleFunc("/version", a.handleVersion)
	mux.HandleFunc("/api/v1/summary", a.protect("/api/v1/summary", a.handleSummary))
	mux.HandleFunc("/api/v1/cpu", a.protect("/api/v1/cpu", a.handleCPU))
	mux.HandleFunc("/api/v1/memory", a.protect("/api/v1/memory", a.handleMemory))
	mux.HandleFunc("/api/v1/network", a.protect("/api/v1/network", a.handleNetwork))
	mux.HandleFunc("/api/v1/disk", a.protect("/api/v1/disk", a.handleDisk))
	mux.HandleFunc("/api/v1/filesystems", a.protect("/api/v1/filesystems", a.handleFilesystems))
	mux.HandleFunc("/api/v1/thermal", a.protect("/api/v1/thermal", a.handleThermal))
	mux.HandleFunc("/api/v1/system", a.protect("/api/v1/system", a.handleSystem))
	return mux
}

func (a *app) protect(endpoint string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		runtimeState := a.currentRuntime()
		if !runtimeState.Config.Enabled {
			http.Error(w, "pcp api is disabled", http.StatusServiceUnavailable)
			return
		}

		if commonpcpapi.IsEndpointPublic(runtimeState.Config, endpoint) || !runtimeState.Config.Auth.Enabled {
			next(w, r)
			return
		}

		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if token == "" || token != runtimeState.Token {
			w.Header().Set("WWW-Authenticate", `Bearer realm="linuxio-pcp-api"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

func (a *app) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	state := a.currentRuntime()
	writeJSON(w, healthResponse{
		OK:            true,
		TSMS:          time.Now().UnixMilli(),
		Version:       commonconfig.Version,
		Enabled:       state.Config.Enabled,
		ListenAddress: state.Config.ListenAddress,
	})
}

func (a *app) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, versionResponse{
		Component: "LinuxIO PCP API",
		Version:   commonconfig.Version,
		CommitSHA: commonconfig.CommitSHA,
		BuildTime: commonconfig.BuildTime,
	})
}

func (a *app) handleCPU(w http.ResponseWriter, r *http.Request) {
	result, err := a.collector.Fetch([]internalpcp.MetricRequest{
		{Name: "hinv.ncpu"},
		{Name: "kernel.all.cpu.idle"},
		{Name: "kernel.percpu.cpu.idle", IncludeInstances: true},
		{Name: "kernel.all.load", IncludeInstances: true},
	})
	if err != nil {
		writeJSON(w, cpuEnvelope{
			envelope: envelope{
				TSMS:      time.Now().UnixMilli(),
				Available: false,
				Reason:    err.Error(),
			},
		})
		return
	}

	ts := millisTime(result.Timestamp)
	cores := int(metricValue(result, "hinv.ncpu"))
	idle := metricValue(result, "kernel.all.cpu.idle")
	perCPUIdle := metricInstances(result, "kernel.percpu.cpu.idle")
	windowMS, usage, perCPUUsage := a.cpuRates.compute(ts, float64(cores), idle, perCPUIdle)

	writeJSON(w, cpuEnvelope{
		envelope: envelope{
			TSMS:      result.Timestamp,
			WindowMS:  windowMS,
			Available: cores > 0,
			Reason:    unavailableReason(cores > 0, "cpu metrics returned no values"),
		},
		CPU: cpuPayload{
			Cores:           cores,
			UsagePercent:    usage,
			Load:            loadFromInstances(metricInstances(result, "kernel.all.load")),
			PerCPUUsage:     perCPUUsage,
			RawIdleMillis:   uint64(max(idle, 0)),
			RawPerCPUIDLEMS: floatMapToUint(perCPUIdle),
		},
	})
}

func (a *app) handleMemory(w http.ResponseWriter, r *http.Request) {
	result, err := a.collector.Fetch([]internalpcp.MetricRequest{
		{Name: "mem.physmem"},
		{Name: "mem.util.available"},
		{Name: "mem.util.swapTotal"},
		{Name: "mem.util.swapFree"},
	})
	if err != nil {
		writeJSON(w, memoryEnvelope{
			envelope: envelope{
				TSMS:      time.Now().UnixMilli(),
				Available: false,
				Reason:    err.Error(),
			},
		})
		return
	}

	totalKib := metricValue(result, "mem.physmem")
	availableKib := metricValue(result, "mem.util.available")
	swapTotalKib := metricValue(result, "mem.util.swapTotal")
	swapFreeKib := metricValue(result, "mem.util.swapFree")
	usedKib := max(totalKib-availableKib, 0)
	swapUsedKib := max(swapTotalKib-swapFreeKib, 0)

	writeJSON(w, memoryEnvelope{
		envelope: envelope{
			TSMS:      result.Timestamp,
			Available: totalKib > 0,
			Reason:    unavailableReason(totalKib > 0, "memory metrics returned no values"),
		},
		Memory: memoryPayload{
			TotalBytes:      kibToBytes(totalKib),
			AvailableBytes:  kibToBytes(availableKib),
			UsedBytes:       kibToBytes(usedKib),
			UsedPercent:     percentOf(usedKib, totalKib),
			SwapTotalBytes:  kibToBytes(swapTotalKib),
			SwapFreeBytes:   kibToBytes(swapFreeKib),
			SwapUsedBytes:   kibToBytes(swapUsedKib),
			SwapUsedPercent: percentOf(swapUsedKib, swapTotalKib),
			RawTotalKib:     uint64(max(totalKib, 0)),
			RawAvailableKib: uint64(max(availableKib, 0)),
			RawSwapTotalKib: uint64(max(swapTotalKib, 0)),
			RawSwapFreeKib:  uint64(max(swapFreeKib, 0)),
		},
	})
}

func (a *app) handleNetwork(w http.ResponseWriter, r *http.Request) {
	result, err := a.collector.Fetch([]internalpcp.MetricRequest{
		{Name: "network.interface.in.bytes", IncludeInstances: true},
		{Name: "network.interface.out.bytes", IncludeInstances: true},
	})
	if err != nil {
		writeJSON(w, networkEnvelope{
			envelope: envelope{
				TSMS:      time.Now().UnixMilli(),
				Available: false,
				Reason:    err.Error(),
			},
		})
		return
	}

	inbound := filterInstances(metricInstances(result, "network.interface.in.bytes"), allowNetworkInstance)
	outbound := filterInstances(metricInstances(result, "network.interface.out.bytes"), allowNetworkInstance)
	windowMS, items := a.networkRates.compute(millisTime(result.Timestamp), inbound, outbound)

	interfaces := make([]networkInterfacePayload, 0, len(items))
	for _, name := range sortedKeys(items) {
		item := items[name]
		interfaces = append(interfaces, networkInterfacePayload{
			Name:          name,
			RXBytesPerSec: item.FirstRate,
			TXBytesPerSec: item.SecondRate,
			RXTotalBytes:  item.FirstTotal,
			TXTotalBytes:  item.SecondTotal,
		})
	}

	writeJSON(w, networkEnvelope{
		envelope: envelope{
			TSMS:      result.Timestamp,
			WindowMS:  windowMS,
			Available: len(interfaces) > 0,
			Reason:    unavailableReason(len(interfaces) > 0, "network metrics returned no values"),
		},
		Network: networkPayload{Interfaces: interfaces},
	})
}

func (a *app) handleDisk(w http.ResponseWriter, r *http.Request) {
	result, err := a.collector.Fetch([]internalpcp.MetricRequest{
		{Name: "disk.dev.read_bytes", IncludeInstances: true},
		{Name: "disk.dev.write_bytes", IncludeInstances: true},
	})
	if err != nil {
		writeJSON(w, diskEnvelope{
			envelope: envelope{
				TSMS:      time.Now().UnixMilli(),
				Available: false,
				Reason:    err.Error(),
			},
		})
		return
	}

	reads := filterInstances(metricInstances(result, "disk.dev.read_bytes"), allowDiskInstance)
	writes := filterInstances(metricInstances(result, "disk.dev.write_bytes"), allowDiskInstance)
	windowMS, items := a.diskRates.compute(millisTime(result.Timestamp), reads, writes)

	devices := make([]diskDevicePayload, 0, len(items))
	for _, name := range sortedKeys(items) {
		item := items[name]
		devices = append(devices, diskDevicePayload{
			Name:             name,
			ReadBytesPerSec:  item.FirstRate,
			WriteBytesPerSec: item.SecondRate,
			ReadTotalBytes:   item.FirstTotal,
			WriteTotalBytes:  item.SecondTotal,
		})
	}

	writeJSON(w, diskEnvelope{
		envelope: envelope{
			TSMS:      result.Timestamp,
			WindowMS:  windowMS,
			Available: len(devices) > 0,
			Reason:    unavailableReason(len(devices) > 0, "disk metrics returned no values"),
		},
		Disk: diskPayload{Devices: devices},
	})
}

func (a *app) handleFilesystems(w http.ResponseWriter, r *http.Request) {
	result, err := a.collector.Fetch([]internalpcp.MetricRequest{
		{Name: "filesys.capacity", IncludeInstances: true},
		{Name: "filesys.avail", IncludeInstances: true},
	})
	if err != nil {
		writeJSON(w, filesystemsEnvelope{
			envelope: envelope{
				TSMS:      time.Now().UnixMilli(),
				Available: false,
				Reason:    err.Error(),
			},
		})
		return
	}

	capacityByDevice := metricInstances(result, "filesys.capacity")
	availByDevice := metricInstances(result, "filesys.avail")
	mounts, fsErr := bridgeSystem.FetchFileSystemInfo(true)
	if fsErr != nil {
		writeJSON(w, filesystemsEnvelope{
			envelope: envelope{
				TSMS:      result.Timestamp,
				Available: false,
				Reason:    fsErr.Error(),
			},
		})
		return
	}

	payload := make([]filesystemPayload, 0, len(mounts))
	for _, mount := range mounts {
		source := "sysfs"
		totalBytes := mount.Total
		freeBytes := mount.Free
		usedBytes := mount.Used
		usedPercent := mount.UsedPercent

		if totalKib, ok := capacityByDevice[mount.Device]; ok {
			source = "pcp"
			totalBytes = kibToBytes(totalKib)
			freeBytes = kibToBytes(availByDevice[mount.Device])
			usedBytes = saturatingSub(totalBytes, freeBytes)
			usedPercent = percentOf(float64(usedBytes), float64(totalBytes))
		}

		payload = append(payload, filesystemPayload{
			Device:      mount.Device,
			Mountpoint:  mount.Mountpoint,
			FSType:      mount.FSType,
			TotalBytes:  totalBytes,
			FreeBytes:   freeBytes,
			UsedBytes:   usedBytes,
			UsedPercent: usedPercent,
			ReadOnly:    mount.ReadOnly,
			Source:      source,
		})
	}

	sort.Slice(payload, func(i, j int) bool {
		return payload[i].Mountpoint < payload[j].Mountpoint
	})

	writeJSON(w, filesystemsEnvelope{
		envelope: envelope{
			TSMS:      result.Timestamp,
			Available: len(payload) > 0,
			Reason:    unavailableReason(len(payload) > 0, "filesystem metrics returned no values"),
		},
		Filesystems: filesystemsPayload{Mounts: payload},
	})
}

func (a *app) handleThermal(w http.ResponseWriter, r *http.Request) {
	groups := bridgeSystem.FetchSensorsInfo()
	writeJSON(w, thermalEnvelope{
		envelope: envelope{
			TSMS:      time.Now().UnixMilli(),
			Available: len(groups) > 0,
			Reason:    unavailableReason(len(groups) > 0, "no thermal sensors available"),
		},
		Thermal: thermalPayload{Groups: groups},
	})
}

func (a *app) handleSystem(w http.ResponseWriter, r *http.Request) {
	result, err := a.collector.Fetch([]internalpcp.MetricRequest{
		{Name: "kernel.all.uptime"},
		{Name: "kernel.all.load", IncludeInstances: true},
	})
	if err != nil {
		writeJSON(w, systemEnvelope{
			envelope: envelope{
				TSMS:      time.Now().UnixMilli(),
				Available: false,
				Reason:    err.Error(),
			},
		})
		return
	}

	hostInfo, hostErr := bridgeSystem.FetchHostInfo()
	systemInfo, sysErr := bridgeSystem.FetchSystemInfo()
	if hostErr != nil {
		writeJSON(w, systemEnvelope{
			envelope: envelope{
				TSMS:      result.Timestamp,
				Available: false,
				Reason:    hostErr.Error(),
			},
		})
		return
	}

	var info *bridgeSystem.SystemInfo
	if sysErr == nil {
		info = systemInfo
	}

	writeJSON(w, systemEnvelope{
		envelope: envelope{
			TSMS:      result.Timestamp,
			Available: true,
		},
		System: systemPayload{
			Hostname:        hostInfo.Hostname,
			Platform:        hostInfo.Platform,
			PlatformVersion: hostInfo.PlatformVersion,
			KernelVersion:   hostInfo.KernelVersion,
			KernelArch:      hostInfo.KernelArch,
			UptimeSeconds:   uint64(max(metricValue(result, "kernel.all.uptime"), 0)),
			Load:            loadFromInstances(metricInstances(result, "kernel.all.load")),
			SystemInfo:      info,
		},
	})
}

func (a *app) handleSummary(w http.ResponseWriter, r *http.Request) {
	result, err := a.collector.Fetch([]internalpcp.MetricRequest{
		{Name: "hinv.ncpu"},
		{Name: "kernel.all.cpu.idle"},
		{Name: "kernel.all.load", IncludeInstances: true},
		{Name: "mem.physmem"},
		{Name: "mem.util.available"},
		{Name: "kernel.all.uptime"},
		{Name: "network.interface.in.bytes", IncludeInstances: true},
		{Name: "network.interface.out.bytes", IncludeInstances: true},
		{Name: "disk.dev.read_bytes", IncludeInstances: true},
		{Name: "disk.dev.write_bytes", IncludeInstances: true},
	})
	if err != nil {
		writeJSON(w, summaryEnvelope{
			envelope: envelope{
				TSMS:      time.Now().UnixMilli(),
				Available: false,
				Reason:    err.Error(),
			},
		})
		return
	}

	hostInfo, hostErr := bridgeSystem.FetchHostInfo()
	if hostErr != nil {
		writeJSON(w, summaryEnvelope{
			envelope: envelope{
				TSMS:      result.Timestamp,
				Available: false,
				Reason:    hostErr.Error(),
			},
		})
		return
	}

	cpuSummary := bridgeSystem.FetchCPUSummary()
	cpuTemp, cpuTempOK := bridgeSystem.FetchPreferredCPUTemperature()
	var cpuTempPtr *float64
	if cpuTempOK {
		cpuTempPtr = &cpuTemp
	}

	ts := millisTime(result.Timestamp)
	cores := metricValue(result, "hinv.ncpu")
	idle := metricValue(result, "kernel.all.cpu.idle")
	cpuWindowMS, cpuUsage, _ := a.summaryCPURates.compute(ts, cores, idle, nil)

	inbound := filterInstances(metricInstances(result, "network.interface.in.bytes"), allowNetworkInstance)
	outbound := filterInstances(metricInstances(result, "network.interface.out.bytes"), allowNetworkInstance)
	networkWindowMS, networkItems := a.summaryNetworkRate.compute(ts, inbound, outbound)

	reads := filterInstances(metricInstances(result, "disk.dev.read_bytes"), allowDiskInstance)
	writes := filterInstances(metricInstances(result, "disk.dev.write_bytes"), allowDiskInstance)
	diskWindowMS, diskItems := a.summaryDiskRate.compute(ts, reads, writes)

	totalMemoryKib := metricValue(result, "mem.physmem")
	availableKib := metricValue(result, "mem.util.available")
	usedMemoryKib := max(totalMemoryKib-availableKib, 0)

	var totalRX, totalTX, totalRead, totalWrite float64
	for _, item := range networkItems {
		totalRX += item.FirstRate
		totalTX += item.SecondRate
	}
	for _, item := range diskItems {
		totalRead += item.FirstRate
		totalWrite += item.SecondRate
	}

	writeJSON(w, summaryEnvelope{
		envelope: envelope{
			TSMS:      result.Timestamp,
			WindowMS:  commonWindow(cpuWindowMS, networkWindowMS, diskWindowMS),
			Available: cores > 0 && totalMemoryKib > 0,
			Reason:    unavailableReason(cores > 0 && totalMemoryKib > 0, "summary metrics returned no values"),
		},
		Summary: summaryPayload{
			Hostname:             hostInfo.Hostname,
			UptimeSeconds:        uint64(max(metricValue(result, "kernel.all.uptime"), 0)),
			Load:                 loadFromInstances(metricInstances(result, "kernel.all.load")),
			CPUUsagePercent:      cpuUsage,
			CPUSummary:           cpuSummary,
			CPUTemperatureC:      cpuTempPtr,
			MemoryUsedPercent:    percentOf(usedMemoryKib, totalMemoryKib),
			MemoryUsedBytes:      kibToBytes(usedMemoryKib),
			MemoryTotalBytes:     kibToBytes(totalMemoryKib),
			NetworkRXBytesPerSec: totalRX,
			NetworkTXBytesPerSec: totalTX,
			DiskReadBytesPerSec:  totalRead,
			DiskWriteBytesPerSec: totalWrite,
		},
	})
}

func (s *cpuRateStore) compute(ts time.Time, cores, idle float64, perCPU map[string]float64) (int64, float64, map[string]float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	windowMS := ts.Sub(s.prev.ts).Milliseconds()
	if s.prev.ts.IsZero() || windowMS <= 0 || cores <= 0 {
		s.prev = cpuSnapshot{ts: ts, idle: idle, perCPU: cloneFloatMap(perCPU)}
		return 0, 0, zeroCPUUsage(perCPU)
	}

	totalIdleFraction := clamp01((idle - s.prev.idle) / float64(windowMS) / cores)
	usage := clampPercent(100 * (1 - totalIdleFraction))
	perCPUUsage := make(map[string]float64, len(perCPU))
	for name, current := range perCPU {
		previous, ok := s.prev.perCPU[name]
		if !ok {
			perCPUUsage[name] = 0
			continue
		}
		idleFraction := clamp01((current - previous) / float64(windowMS))
		perCPUUsage[name] = clampPercent(100 * (1 - idleFraction))
	}

	s.prev = cpuSnapshot{ts: ts, idle: idle, perCPU: cloneFloatMap(perCPU)}
	return windowMS, usage, perCPUUsage
}

func (s *ioRateStore) compute(ts time.Time, first, second map[string]float64) (int64, map[string]ioRate) {
	s.mu.Lock()
	defer s.mu.Unlock()

	windowMS := ts.Sub(s.prev.ts).Milliseconds()
	current := mergeKeySet(first, second)
	result := make(map[string]ioRate, len(current))

	if s.prev.ts.IsZero() || windowMS <= 0 {
		for _, name := range current {
			result[name] = ioRate{
				FirstTotal:  uint64(max(first[name], 0)),
				SecondTotal: uint64(max(second[name], 0)),
			}
		}
		s.prev = ioSnapshot{ts: ts, first: cloneFloatMap(first), second: cloneFloatMap(second)}
		return 0, result
	}

	seconds := float64(windowMS) / 1000
	for _, name := range current {
		currentFirst := first[name]
		currentSecond := second[name]
		previousFirst := s.prev.first[name]
		previousSecond := s.prev.second[name]

		firstRate := 0.0
		secondRate := 0.0
		if currentFirst >= previousFirst {
			firstRate = (currentFirst - previousFirst) / seconds
		}
		if currentSecond >= previousSecond {
			secondRate = (currentSecond - previousSecond) / seconds
		}

		result[name] = ioRate{
			FirstRate:   firstRate,
			SecondRate:  secondRate,
			FirstTotal:  uint64(max(currentFirst, 0)),
			SecondTotal: uint64(max(currentSecond, 0)),
		}
	}

	s.prev = ioSnapshot{ts: ts, first: cloneFloatMap(first), second: cloneFloatMap(second)}
	return windowMS, result
}

func metricValue(result internalpcp.FetchResult, name string) float64 {
	item, ok := result.Metrics[name]
	if !ok || !item.Found {
		return 0
	}
	return item.Value
}

func metricInstances(result internalpcp.FetchResult, name string) map[string]float64 {
	item, ok := result.Metrics[name]
	if !ok || len(item.Instances) == 0 {
		return map[string]float64{}
	}
	return cloneFloatMap(item.Instances)
}

func loadFromInstances(values map[string]float64) loadAverages {
	load := loadAverages{}
	for name, value := range values {
		switch {
		case strings.Contains(name, "1 minute") || name == "1":
			load.Load1 = value
		case strings.Contains(name, "5 minute") || name == "5":
			load.Load5 = value
		case strings.Contains(name, "15 minute") || name == "15":
			load.Load15 = value
		}
	}
	return load
}

func kibToBytes(value float64) uint64 {
	return uint64(max(value, 0) * 1024)
}

func percentOf(part, total float64) float64 {
	if total <= 0 {
		return 0
	}
	return clampPercent((part / total) * 100)
}

func saturatingSub(total, free uint64) uint64 {
	if free >= total {
		return 0
	}
	return total - free
}

func cloneFloatMap(src map[string]float64) map[string]float64 {
	if len(src) == 0 {
		return map[string]float64{}
	}
	clone := make(map[string]float64, len(src))
	maps.Copy(clone, src)
	return clone
}

func mergeKeySet(first, second map[string]float64) []string {
	keys := make(map[string]struct{}, len(first)+len(second))
	for key := range first {
		keys[key] = struct{}{}
	}
	for key := range second {
		keys[key] = struct{}{}
	}
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func zeroCPUUsage(perCPU map[string]float64) map[string]float64 {
	if len(perCPU) == 0 {
		return nil
	}
	zeroes := make(map[string]float64, len(perCPU))
	for key := range perCPU {
		zeroes[key] = 0
	}
	return zeroes
}

func filterInstances(values map[string]float64, allow func(string) bool) map[string]float64 {
	filtered := make(map[string]float64)
	for name, value := range values {
		if allow == nil || allow(name) {
			filtered[name] = value
		}
	}
	return filtered
}

func allowNetworkInstance(name string) bool {
	if networkExcludeRE.MatchString(name) {
		return false
	}
	return networkIncludeRE.MatchString(name) || strings.HasPrefix(name, "en") || strings.HasPrefix(name, "eth")
}

func allowDiskInstance(name string) bool {
	return diskDeviceRE.MatchString(name)
}

func millisTime(ts int64) time.Time {
	return time.UnixMilli(ts)
}

func commonWindow(values ...int64) int64 {
	var best int64
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if best == 0 || value < best {
			best = value
		}
	}
	return best
}

func clamp01(value float64) float64 {
	return math.Max(0, math.Min(1, value))
}

func clampPercent(value float64) float64 {
	return math.Max(0, math.Min(100, value))
}

func floatMapToUint(values map[string]float64) map[string]uint64 {
	if len(values) == 0 {
		return nil
	}
	converted := make(map[string]uint64, len(values))
	for key, value := range values {
		converted[key] = uint64(max(value, 0))
	}
	return converted
}

func unavailableReason(ok bool, reason string) string {
	if ok {
		return ""
	}
	return reason
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
