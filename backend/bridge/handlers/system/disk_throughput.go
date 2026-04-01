package system

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	gopsdisk "github.com/shirou/gopsutil/v4/disk"
)

type DiskThroughputDevice struct {
	Name             string  `json:"name"`
	ReadBytesPerSec  float64 `json:"readBytesPerSec"`
	WriteBytesPerSec float64 `json:"writeBytesPerSec"`
	ReadOpsPerSec    float64 `json:"readOpsPerSec"`
	WriteOpsPerSec   float64 `json:"writeOpsPerSec"`
}

type DiskThroughputResponse struct {
	ReadBytesPerSec  float64                `json:"readBytesPerSec"`
	WriteBytesPerSec float64                `json:"writeBytesPerSec"`
	ReadOpsPerSec    float64                `json:"readOpsPerSec"`
	WriteOpsPerSec   float64                `json:"writeOpsPerSec"`
	IntervalSeconds  float64                `json:"intervalSeconds"`
	Devices          []DiskThroughputDevice `json:"devices"`
}

var (
	diskThroughputSnapshot     = DiskThroughputResponse{Devices: []DiskThroughputDevice{}}
	diskThroughputSnapshotLock sync.RWMutex
	onceDiskSampler            sync.Once

	diskCounterSampler  = sampleDiskCounters
	sysBlockExists      = defaultSysBlockExists
	sysBlockDeviceExist = defaultSysBlockDeviceExists
)

func runDiskThroughputSampler() {
	previous := diskCounterSampler()
	previousAt := time.Now()

	diskThroughputSnapshotLock.Lock()
	diskThroughputSnapshot = buildDiskThroughputResponse(previous, previous, 0)
	diskThroughputSnapshotLock.Unlock()

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		current := diskCounterSampler()
		currentAt := time.Now()
		intervalSeconds := currentAt.Sub(previousAt).Seconds()
		snapshot := buildDiskThroughputResponse(previous, current, intervalSeconds)

		diskThroughputSnapshotLock.Lock()
		diskThroughputSnapshot = snapshot
		diskThroughputSnapshotLock.Unlock()

		previous = current
		previousAt = currentAt
	}
}

func sampleDiskCounters() map[string]gopsdisk.IOCountersStat {
	stats, err := gopsdisk.IOCounters()
	if err != nil {
		return map[string]gopsdisk.IOCountersStat{}
	}

	result := make(map[string]gopsdisk.IOCountersStat, len(stats))
	for name, stat := range stats {
		if !isPhysicalDiskCounter(name) {
			continue
		}
		result[name] = stat
	}

	return result
}

func FetchDiskThroughput() (DiskThroughputResponse, error) {
	diskThroughputSnapshotLock.RLock()
	defer diskThroughputSnapshotLock.RUnlock()

	response := diskThroughputSnapshot
	response.Devices = append([]DiskThroughputDevice(nil), diskThroughputSnapshot.Devices...)
	return response, nil
}

func buildDiskThroughputResponse(previous, current map[string]gopsdisk.IOCountersStat, intervalSeconds float64) DiskThroughputResponse {
	response := DiskThroughputResponse{
		IntervalSeconds: intervalSeconds,
		Devices:         make([]DiskThroughputDevice, 0, len(current)),
	}

	for name, currentStat := range current {
		previousStat, ok := previous[name]
		if !ok {
			previousStat = currentStat
		}

		device := DiskThroughputDevice{
			Name:             name,
			ReadBytesPerSec:  counterRate(previousStat.ReadBytes, currentStat.ReadBytes, intervalSeconds),
			WriteBytesPerSec: counterRate(previousStat.WriteBytes, currentStat.WriteBytes, intervalSeconds),
			ReadOpsPerSec:    counterRate(previousStat.ReadCount, currentStat.ReadCount, intervalSeconds),
			WriteOpsPerSec:   counterRate(previousStat.WriteCount, currentStat.WriteCount, intervalSeconds),
		}

		response.ReadBytesPerSec += device.ReadBytesPerSec
		response.WriteBytesPerSec += device.WriteBytesPerSec
		response.ReadOpsPerSec += device.ReadOpsPerSec
		response.WriteOpsPerSec += device.WriteOpsPerSec
		response.Devices = append(response.Devices, device)
	}

	sort.Slice(response.Devices, func(i, j int) bool {
		return response.Devices[i].Name < response.Devices[j].Name
	})

	return response
}

func counterRate(previous, current uint64, intervalSeconds float64) float64 {
	if intervalSeconds <= 0 || current < previous {
		return 0
	}
	return float64(current-previous) / intervalSeconds
}

func isPhysicalDiskCounter(name string) bool {
	if name == "" || strings.Contains(name, "/") {
		return false
	}

	switch {
	case strings.HasPrefix(name, "loop"),
		strings.HasPrefix(name, "ram"),
		strings.HasPrefix(name, "zram"),
		strings.HasPrefix(name, "dm-"),
		strings.HasPrefix(name, "md"),
		strings.HasPrefix(name, "sr"),
		strings.HasPrefix(name, "fd"):
		return false
	}

	if !sysBlockExists(name) {
		return false
	}

	return sysBlockDeviceExist(name)
}

func defaultSysBlockExists(name string) bool {
	_, err := os.Stat(filepath.Join("/sys/block", name))
	return err == nil
}

func defaultSysBlockDeviceExists(name string) bool {
	_, err := os.Stat(filepath.Join("/sys/block", name, "device"))
	return err == nil
}
