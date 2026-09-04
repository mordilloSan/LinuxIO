package app

import (
	"context"
	"testing"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/container"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

func TestBuildLiveMapsSample(t *testing.T) {
	at := time.UnixMilli(1_700_000_000_000)
	data := &system.CombinedData{
		Stats: system.Stats{
			Cpu:           12.5,
			CpuBreakdown:  []float64{5, 4, 1, 0, 90},
			CpuCoresUsage: system.Uint8Slice{10, 15},
			LoadAvg:       [3]float64{1, 2, 3},
			MemoryBytes:   system.MemoryBytes{Total: 16 << 30, Used: 8 << 30, ZFSArc: 1 << 30},
			DiskDevices: map[string]system.DiskDeviceRates{
				"sda":     {ReadBytesPerSec: 100, WriteBytesPerSec: 50, ReadOpsPerSec: 1, WriteOpsPerSec: 2},
				"nvme0n1": {ReadBytesPerSec: 300, WriteBytesPerSec: 0},
			},
			NetworkInterfaces: map[string][4]uint64{"eth0": {1000, 2000, 30000, 40000}},
		},
		Info: system.Info{Uptime: 4242},
		Containers: []*container.Stats{
			{Id: "abc123def456", Name: "web", Cpu: 25, Mem: 512, Bandwidth: [2]uint64{7, 9}},
		},
	}
	telemetry := []container.Telemetry{{ID: "abc123def456", DiskReadBytesPerSecond: 11, DiskWriteBytesPerSecond: 13}}

	live := buildLive(data, at, 4, telemetry, at.Add(-30*time.Second), 3*time.Minute)

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

	stale := buildLive(data, at, 4, telemetry, at.Add(-10*time.Minute), 3*time.Minute)
	if stale.Containers.Items[0].BlockReadBytesPerSec != nil {
		t.Fatal("stale telemetry must leave block rates nil")
	}
}

func TestLiveUsesOwnSampleKeyAndTelemetryMemo(t *testing.T) {
	a := &App{liveRuns: map[uint16]*liveRun{}}
	var gotKey uint16
	var gotDetails, gotContainers bool
	a.collectLive = func(_ context.Context, key uint16, includeDetails, includeContainers bool) (*system.CombinedData, error) {
		gotKey, gotDetails, gotContainers = key, includeDetails, includeContainers
		return &system.CombinedData{
			Details:    &system.Details{Threads: 2},
			Containers: []*container.Stats{{Id: "abc", Cpu: 10}},
		}, nil
	}
	a.rememberTelemetry([]container.Telemetry{{ID: "abc", DiskReadBytesPerSecond: 5}}, time.Now())

	live, err := a.Live(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotKey != liveLiveSampleKey || !gotDetails || !gotContainers {
		t.Fatalf("key = %d details = %v containers = %v", gotKey, gotDetails, gotContainers)
	}
	ctr := live.Containers.Items[0]
	if ctr.CPUPercent != 20 {
		t.Fatalf("threads multiplier not applied: %+v", ctr)
	}
	if ctr.BlockReadBytesPerSec == nil || *ctr.BlockReadBytesPerSec != 5 {
		t.Fatalf("memoed telemetry not attached: %+v", ctr)
	}
}
