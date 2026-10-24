package live

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	containermodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

func TestBuildMapsSample(t *testing.T) {
	at := time.UnixMilli(1_700_000_000_000)
	data := &systemmodel.CombinedData{
		Stats: systemmodel.Stats{
			Cpu:           12.5,
			CpuBreakdown:  []float64{5, 4, 1, 0, 90},
			CpuCoresUsage: systemmodel.Uint8Slice{10, 15},
			LoadAvg:       [3]float64{1, 2, 3},
			Counters: systemmodel.Counters{
				MemoryBytes: systemmodel.MemoryBytes{Total: 16 << 30, Used: 8 << 30, ZFSArc: 1 << 30},
				DiskDevices: map[string]systemmodel.DiskDeviceRates{
					"sda":     {ReadBytesPerSec: 100, WriteBytesPerSec: 50, ReadOpsPerSec: 1, WriteOpsPerSec: 2},
					"nvme0n1": {ReadBytesPerSec: 300, WriteBytesPerSec: 0},
				},
				NetworkInterfaceCounters: map[string]systemmodel.NetworkInterfaceCounters{"eth0": {
					RxDropped: 1, RxErrors: 2, RxPackets: 3, TxDropped: 4, TxErrors: 5, TxPackets: 6,
				}},
			},
			NetworkInterfaces: map[string][4]uint64{"eth0": {1000, 2000, 30000, 40000}},
		},
		Info: systemmodel.Info{Uptime: 4242},
		Containers: []*containermodel.Stats{
			{Id: "abc123def456", Name: "web", Cpu: 25, Mem: 512, Bandwidth: [2]uint64{7, 9}},
		},
	}
	telemetry := []containermodel.Telemetry{{ID: "abc123def456", DiskReadBytesPerSecond: 11, DiskWriteBytesPerSecond: 13}}

	live := Build(data, at, 4, telemetry, at.Add(-30*time.Second), 3*time.Minute)

	if live.CapturedAtMs != at.UnixMilli() || live.UptimeSeconds != 4242 {
		t.Fatalf("header = %+v", live)
	}
	if live.CPU.Percent != 12.5 || live.CPU.Breakdown.Idle != 90 || len(live.CPU.PerCorePercent) != 2 || live.CPU.PerCorePercent[1] != 15 {
		t.Fatalf("cpu = %+v", live.CPU)
	}
	if live.Memory.TotalBytes != 16<<30 || live.Memory.ZFSArcBytes != 1<<30 || live.Memory.DockerUsedBytes != 512<<20 {
		t.Fatalf("memory = %+v", live.Memory)
	}
	if live.DiskIO.ReadBytesPerSec != 400 || live.Disks["sda"].WriteOpsPerSec != 2 {
		t.Fatalf("disks = %+v %+v", live.DiskIO, live.Disks)
	}
	eth := live.Interfaces["eth0"]
	if eth.TxBytesPerSec != 1000 || eth.RxBytesPerSec != 2000 || eth.TxBytesTotal != 30000 || eth.RxBytesTotal != 40000 {
		t.Fatalf("eth0 = %+v", eth)
	}
	require.Equal(t, []uint64{1, 2, 3, 4, 5, 6}, []uint64{eth.RxDropped, eth.RxErrors, eth.RxPackets, eth.TxDropped, eth.TxErrors, eth.TxPackets})
	if len(live.Containers.Items) != 1 {
		t.Fatalf("containers = %+v", live.Containers)
	}
	ctr := live.Containers.Items[0]
	if ctr.CPUPercent != 100 || ctr.MemoryBytes != 512<<20 || ctr.TxBytesPerSec != 7 || ctr.RxBytesPerSec != 9 {
		t.Fatalf("container = %+v", ctr)
	}
	if ctr.BlockReadBytesPerSec == nil || *ctr.BlockReadBytesPerSec != 11 || *ctr.BlockWriteBytesPerSec != 13 {
		t.Fatalf("telemetry not attached: %+v", ctr)
	}

	stale := Build(data, at, 4, telemetry, at.Add(-10*time.Minute), 3*time.Minute)
	if stale.Containers.Items[0].BlockReadBytesPerSec != nil {
		t.Fatal("stale telemetry must leave block rates nil")
	}
}

func TestExtrasApplyCopiesAndFillsEmptySections(t *testing.T) {
	sensors := []monitoringapi.SensorGroup{{Adapter: "coretemp", Readings: []monitoringapi.SensorReading{{
		Label: "Package id 0", Value: 55.5, Kind: monitoringapi.SensorReadingKindNumber, Unit: "°C", Field: "temp1_input",
	}}}}
	var empty monitoringapi.Live
	Extras{}.Apply(&empty)
	if empty.Filesystems == nil || empty.Sensors == nil || empty.CPU.FrequenciesMHz == nil || empty.GPUs == nil || empty.Smart == nil || empty.CPU.Temperatures == nil {
		t.Fatalf("absent sections must be empty, not nil: %+v", empty)
	}

	var filled monitoringapi.Live
	extras := Extras{Sensors: sensors, Frequencies: []float64{2200}}
	extras.Apply(&filled)
	extras.Frequencies[0] = 1
	if filled.CPU.FrequenciesMHz[0] != 2200 {
		t.Fatalf("Apply must copy the shared frequencies: %v", filled.CPU.FrequenciesMHz)
	}
	if filled.CPU.Temperatures["package"] != 55.5 {
		t.Fatalf("temperatures = %v", filled.CPU.Temperatures)
	}
}
