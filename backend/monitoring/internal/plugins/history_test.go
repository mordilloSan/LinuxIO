package plugins

import (
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
)

func TestParseHistory(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		got, err := ParseHistory("")
		require.NoError(t, err)
		assert.Equal(t, DefaultHistory(), got)
	})

	t.Run("explicit all", func(t *testing.T) {
		got, err := ParseHistory("all")
		require.NoError(t, err)
		assert.Equal(t, HistoryCapable(), got)
		assert.NotContains(t, got, Processes)
		assert.NotContains(t, got, Programs)
	})

	t.Run("explicit none", func(t *testing.T) {
		got, err := ParseHistory("none")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("invalid plugin", func(t *testing.T) {
		_, err := ParseHistory("cpu,nope")
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown history plugin "nope"`)
	})

	t.Run("live-only process plugins", func(t *testing.T) {
		for _, plugin := range []string{Processes, Programs} {
			_, err := ParseHistory(plugin)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "live-only")
		}
	})
}

func TestHistoryEvery(t *testing.T) {
	tick := time.Minute

	t.Run("converts intervals to tick multiples", func(t *testing.T) {
		every, err := HistoryEvery(map[string]time.Duration{
			Containers: 5 * time.Minute,
			CPU:        time.Minute,
		}, tick)
		require.NoError(t, err)
		assert.Equal(t, map[string]uint64{Containers: 5, CPU: 1}, every)
	})

	t.Run("empty map is fine", func(t *testing.T) {
		every, err := HistoryEvery(nil, tick)
		require.NoError(t, err)
		assert.Empty(t, every)
	})

	t.Run("rejects intervals that are not a whole multiple of the tick", func(t *testing.T) {
		_, err := HistoryEvery(map[string]time.Duration{Mem: 90 * time.Second}, tick)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mem")
		assert.Contains(t, err.Error(), "multiple")
	})

	t.Run("rejects intervals below the tick", func(t *testing.T) {
		_, err := HistoryEvery(map[string]time.Duration{Mem: 30 * time.Second}, tick)
		require.Error(t, err)
		_, err = HistoryEvery(map[string]time.Duration{Mem: 0}, tick)
		require.Error(t, err)
	})

	t.Run("rejects plugins without collector history", func(t *testing.T) {
		for _, plugin := range []string{Smart, Processes, Programs, "nope"} {
			_, err := HistoryEvery(map[string]time.Duration{plugin: tick}, tick)
			require.Error(t, err, plugin)
			assert.Contains(t, err.Error(), plugin)
		}
	})
}

func TestLoadDataBatteryUsesNumericArray(t *testing.T) {
	var item LoadData
	require.NoError(t, json.Unmarshal([]byte(`{"battery":[85,1]}`), &item))
	assert.Equal(t, [2]uint16{85, 1}, item.Battery)

	got, err := json.Marshal(item)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"battery":[85,1]`)
}

func TestAverageSystemStatsAveragesKeysOverTheirOwnSamples(t *testing.T) {
	samples := make([]systemmodel.Stats, 2)
	for i, raw := range []string{
		`{"temperatures":{"cpu":40,"nvme":50},"network_interfaces":{"eth0":[100,200,5,6]},"extra_filesystems":{"/data":{"disk_total_gb":10,"disk_used_gb":4}},"gpus":{"g0":{"name":"gpu","usage_percent":60,"engines":{"render":30}}}}`,
		`{"temperatures":{"cpu":60},"network_interfaces":{"eth0":[300,400,7,8],"wlan0":[10,20,1,2]},"extra_filesystems":{"/data":{"disk_total_gb":20,"disk_used_gb":8}}}`,
	} {
		require.NoError(t, json.Unmarshal([]byte(raw), &samples[i]))
	}
	got := averageSystemStats(samples)

	assert.InDelta(t, 50.0, got.Temperatures["cpu"], 0.001)
	assert.InDelta(t, 50.0, got.Temperatures["nvme"], 0.001)
	assert.Equal(t, [4]uint64{200, 300, 7, 8}, got.NetworkInterfaces["eth0"])
	assert.Equal(t, [4]uint64{10, 20, 1, 2}, got.NetworkInterfaces["wlan0"])
	assert.InDelta(t, 15.0, got.ExtraFs["/data"].DiskTotal, 0.001)
	assert.InDelta(t, 60.0, got.GPUData["g0"].Usage, 0.001)
	assert.InDelta(t, 30.0, got.GPUData["g0"].Engines["render"], 0.001)
}

func TestAverageKeepsMemorySplitAndSensorReadings(t *testing.T) {
	mem, err := Average(Mem, []string{
		`{"memory_used_gb":8,"memory_docker_gb":1,"memory_vm_gb":4}`,
		`{"memory_used_gb":10,"memory_docker_gb":2,"memory_vm_gb":5}`,
	})
	require.NoError(t, err)
	assert.Contains(t, mem, `"memory_docker_gb":1.5`)
	assert.Contains(t, mem, `"memory_vm_gb":4.5`)

	sensors, err := Average(Sensors, []string{
		`{"readings":{"k10temp":{"Tctl":40},"ec":{"CPU fan":1000}}}`,
		`{"readings":{"k10temp":{"Tctl":50}}}`,
	})
	require.NoError(t, err)
	var got SensorsData
	require.NoError(t, json.Unmarshal([]byte(sensors), &got))
	// A reading missing from a sample averages over the samples that have it.
	assert.Equal(t, map[string]map[string]float64{
		"k10temp": {"Tctl": 45},
		"ec":      {"CPU fan": 1000},
	}, got.Readings)
}

// A container in a rollup is keyed by name and divided by every row of the
// bucket: one that ran in one row of four at 100% reads 25%, and one recreated
// under a new ID (an image update) stays one series with the newest ID.
func TestAverageContainersKeysByNameAndDividesByEveryRow(t *testing.T) {
	got, err := Average(Containers, []string{
		`[{"id":"aaa","name":"web","cpu_percent":40,"memory_mb":100,"bandwidth_bytes":[400,800]}]`,
		`[{"id":"aaa","name":"web","cpu_percent":40,"memory_mb":100,"bandwidth_bytes":[400,800]},{"id":"ccc","name":"job","cpu_percent":100,"memory_mb":64}]`,
		`[{"id":"bbb","name":"web","cpu_percent":40,"memory_mb":100,"bandwidth_bytes":[400,800]}]`,
		`[{"id":"bbb","name":"web","cpu_percent":40,"memory_mb":100,"bandwidth_bytes":[400,800]}]`,
	})
	require.NoError(t, err)
	var records []containerSnapshotRecord
	require.NoError(t, json.Unmarshal([]byte(got), &records))
	require.Len(t, records, 2)
	byName := map[string]containerSnapshotRecord{}
	for _, record := range records {
		byName[record.Name] = record
	}
	assert.Equal(t, "bbb", byName["web"].ID)
	assert.InDelta(t, 40.0, byName["web"].Cpu, 0.001)
	assert.InDelta(t, 100.0, byName["web"].Mem, 0.001)
	assert.Equal(t, [2]uint64{400, 800}, byName["web"].Bandwidth)
	assert.InDelta(t, 25.0, byName["job"].Cpu, 0.001)
	assert.InDelta(t, 16.0, byName["job"].Mem, 0.001)
}

// Container telemetry follows the same rule for its CPU and disk rates, which
// the container history joins to the containers rollup, and keeps averaging
// its process count over the rows that carry the container.
func TestAverageContainerTelemetryKeysByNameAndDividesRatesByEveryRow(t *testing.T) {
	got, err := Average(ContainerTelemetry, []string{
		`[{"id":"aaa","name":"web","cpu_percent":20,"process_count":4,"disk_read_bytes_per_second":1000,"disk_write_bytes_per_second":2000}]`,
		`[{"id":"bbb","name":"web","cpu_percent":20,"process_count":4,"disk_read_bytes_per_second":1000,"disk_write_bytes_per_second":2000}]`,
		`[]`,
		`[]`,
	})
	require.NoError(t, err)
	assert.Contains(t, got, `"id":"bbb"`)
	assert.NotContains(t, got, `"id":"aaa"`)
	assert.Contains(t, got, `"cpu_percent":10`)
	assert.Contains(t, got, `"process_count":4`)
	assert.Contains(t, got, `"disk_read_bytes_per_second":500`)
	assert.Contains(t, got, `"disk_write_bytes_per_second":1000`)
}
