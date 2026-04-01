package system

import (
	"testing"

	gopsdisk "github.com/shirou/gopsutil/v4/disk"
	"github.com/stretchr/testify/require"
)

func TestBuildDiskThroughputResponseAggregatesAndSortsDevices(t *testing.T) {
	previous := map[string]gopsdisk.IOCountersStat{
		"nvme0n1": {
			ReadBytes:  100,
			WriteBytes: 200,
			ReadCount:  10,
			WriteCount: 20,
		},
		"sda": {
			ReadBytes:  1000,
			WriteBytes: 1500,
			ReadCount:  100,
			WriteCount: 150,
		},
	}

	current := map[string]gopsdisk.IOCountersStat{
		"sda": {
			ReadBytes:  1600,
			WriteBytes: 2100,
			ReadCount:  130,
			WriteCount: 180,
		},
		"nvme0n1": {
			ReadBytes:  300,
			WriteBytes: 260,
			ReadCount:  18,
			WriteCount: 22,
		},
		"vdb": {
			ReadBytes:  500,
			WriteBytes: 100,
			ReadCount:  5,
			WriteCount: 2,
		},
	}

	response := buildDiskThroughputResponse(previous, current, 2)

	require.Equal(t, 400.0, response.ReadBytesPerSec)
	require.Equal(t, 330.0, response.WriteBytesPerSec)
	require.Equal(t, 19.0, response.ReadOpsPerSec)
	require.Equal(t, 16.0, response.WriteOpsPerSec)
	require.Equal(t, 2.0, response.IntervalSeconds)
	require.Len(t, response.Devices, 3)

	require.Equal(t, "nvme0n1", response.Devices[0].Name)
	require.Equal(t, 100.0, response.Devices[0].ReadBytesPerSec)
	require.Equal(t, 30.0, response.Devices[0].WriteBytesPerSec)
	require.Equal(t, 4.0, response.Devices[0].ReadOpsPerSec)
	require.Equal(t, 1.0, response.Devices[0].WriteOpsPerSec)

	require.Equal(t, "sda", response.Devices[1].Name)
	require.Equal(t, 300.0, response.Devices[1].ReadBytesPerSec)
	require.Equal(t, 300.0, response.Devices[1].WriteBytesPerSec)
	require.Equal(t, 15.0, response.Devices[1].ReadOpsPerSec)
	require.Equal(t, 15.0, response.Devices[1].WriteOpsPerSec)

	require.Equal(t, "vdb", response.Devices[2].Name)
	require.Zero(t, response.Devices[2].ReadBytesPerSec)
	require.Zero(t, response.Devices[2].WriteBytesPerSec)
	require.Zero(t, response.Devices[2].ReadOpsPerSec)
	require.Zero(t, response.Devices[2].WriteOpsPerSec)
}

func TestCounterRateReturnsZeroForInvalidSamples(t *testing.T) {
	require.Zero(t, counterRate(10, 20, 0))
	require.Zero(t, counterRate(20, 10, 1))
	require.Equal(t, 10.0, counterRate(10, 20, 1))
}

func TestIsPhysicalDiskCounterFiltersNonPhysicalDevices(t *testing.T) {
	originalBlockExists := sysBlockExists
	originalDeviceExists := sysBlockDeviceExist
	t.Cleanup(func() {
		sysBlockExists = originalBlockExists
		sysBlockDeviceExist = originalDeviceExists
	})

	sysBlockExists = func(name string) bool {
		switch name {
		case "nvme0n1", "sda", "vdb", "dm-0", "sda1":
			return true
		default:
			return false
		}
	}
	sysBlockDeviceExist = func(name string) bool {
		switch name {
		case "nvme0n1", "sda", "vdb":
			return true
		default:
			return false
		}
	}

	require.True(t, isPhysicalDiskCounter("nvme0n1"))
	require.True(t, isPhysicalDiskCounter("sda"))
	require.True(t, isPhysicalDiskCounter("vdb"))
	require.False(t, isPhysicalDiskCounter(""))
	require.False(t, isPhysicalDiskCounter("loop0"))
	require.False(t, isPhysicalDiskCounter("zram0"))
	require.False(t, isPhysicalDiskCounter("dm-0"))
	require.False(t, isPhysicalDiskCounter("md127"))
	require.False(t, isPhysicalDiskCounter("sr0"))
	require.False(t, isPhysicalDiskCounter("sda/queue"))
	require.False(t, isPhysicalDiskCounter("sda1"))
}
