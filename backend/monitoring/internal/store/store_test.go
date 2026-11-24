package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	smartmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/plugins"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store/storetest"
)

func TestStoreSnapshotAndHistoryQueries(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	store, err := OpenStore(t.Context(), tmpDir)
	require.NoError(t, err)
	defer store.Close()

	capturedAt := time.Now().UTC().UnixMilli()
	require.NoError(t, store.WriteSnapshot(t.Context(), capturedAt, storetest.SampleCombinedData(42), store.BeginTick()))
	var tableCount int
	require.NoError(t, store.db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND (name = 'meta' OR name LIKE '%_current')
	`).Scan(&tableCount))
	assert.Zero(t, tableCount, "snapshot/current tables should not exist")

	records, err := store.PluginHistory(ctx, plugins.CPU, resolution1m, 0, capturedAt, 10)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, capturedAt, records[0].CapturedAt)
	var cpu plugins.CPUData
	require.NoError(t, json.Unmarshal(records[0].Stats, &cpu))
	assert.InDelta(t, 42.0, cpu.Cpu, 0.0001)

	telemetry, err := store.PluginHistory(ctx, plugins.ContainerTelemetry, resolution1m, 0, capturedAt, 10)
	require.NoError(t, err)
	require.Len(t, telemetry, 1)
	assert.Contains(t, string(telemetry[0].Stats), `"disk_read_bytes_per_second":400`)

	allPlugins := plugins.Names()
	require.Contains(t, allPlugins, plugins.CPU)
	require.Contains(t, allPlugins, plugins.Smart)

	_, err = os.Stat(filepath.Join(tmpDir, "metrics.db"))
	require.NoError(t, err)
}

func TestWriteSmartDevicesPersistsCompactData(t *testing.T) {
	store, err := OpenStore(t.Context(), t.TempDir(), Options{HistoryPlugins: []string{plugins.Smart}})
	require.NoError(t, err)
	defer store.Close()

	namespaces := uint64(2)
	powerCycles := uint64(430)
	item := smartmodel.SmartData{
		ModelName:              "P3-2TB",
		SerialNumber:           "serial",
		FirmwareVersion:        "X0104A0",
		Capacity:               2_048,
		SmartStatus:            "PASSED",
		DiskName:               "/dev/sda",
		DiskType:               "sat",
		Temperature:            31,
		Attributes:             []*smartmodel.SmartAttribute{{ID: 5, Name: "Power", RawValue: 42}},
		Device:                 &smartmodel.DeviceInfo{Name: "/dev/sda", Type: "sat"},
		AtaSmartSelfTestLog:    &smartmodel.SmartSelfTestLog{Standard: &smartmodel.SmartSelfTestStandardLog{Count: 1}},
		NVMeVersion:            &smartmodel.NVMeVersionInfo{String: "1.4"},
		NVMeNumberOfNamespaces: &namespaces,
		PowerOnTime:            &smartmodel.SmartPowerOnTime{Hours: 4_399},
		PowerCycleCount:        &powerCycles,
	}

	capturedAt := time.Now().UTC().UnixMilli()
	require.NoError(t, store.WriteSmartDevices(t.Context(), capturedAt, map[string]smartmodel.SmartData{"serial": item}))
	records, err := store.PluginHistory(context.Background(), plugins.Smart, resolution1m, 0, capturedAt, 10)
	require.NoError(t, err)
	require.Len(t, records, 1)

	var envelopes []map[string]jsontext.Value
	require.NoError(t, json.Unmarshal(records[0].Stats, &envelopes))
	require.Len(t, envelopes, 1)
	var persisted map[string]jsontext.Value
	require.NoError(t, json.Unmarshal(envelopes[0]["Data"], &persisted))
	require.Len(t, persisted, 9)
	for _, field := range []string{"model_name", "serial_number", "firmware_version", "capacity_bytes", "smart_status", "disk_name", "disk_type", "temperature_celsius", "attributes"} {
		assert.Contains(t, persisted, field)
	}
	for _, field := range []string{"device", "ata_smart_self_test_log", "nvme_version", "nvme_number_of_namespaces", "power_on_time", "power_cycle_count"} {
		assert.NotContains(t, persisted, field)
	}
	assert.JSONEq(t, `{"model_name":"P3-2TB","serial_number":"serial","firmware_version":"X0104A0","capacity_bytes":2048,"smart_status":"PASSED","disk_name":"/dev/sda","disk_type":"sat","temperature_celsius":31,"attributes":[{"id":5,"name":"Power","raw_value":42}]}`, string(envelopes[0]["Data"]))
	assert.NotNil(t, item.Device)
	assert.NotNil(t, item.AtaSmartSelfTestLog)
	assert.NotNil(t, item.NVMeVersion)
	assert.NotNil(t, item.PowerOnTime)
}

func TestStoreRejectsNilSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := OpenStore(t.Context(), tmpDir)
	require.NoError(t, err)
	defer store.Close()

	err = store.WriteSnapshot(t.Context(), time.Now().UTC().UnixMilli(), nil, store.BeginTick())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot data is nil")
}

// A store of another version, the previous release's or one the daemon does
// not know, is deleted with its sidecars and the history starts again.
func TestStoreDeletesAStoreOfAnotherVersion(t *testing.T) {
	for _, version := range []int{7, storeSchemaVersion + 1} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "metrics.db")
			db, err := sql.Open("sqlite3", dbPath)
			require.NoError(t, err)
			_, err = db.Exec(fmt.Sprintf(`
				CREATE TABLE cpu_history (resolution TEXT NOT NULL, captured_at INTEGER NOT NULL, stats_json TEXT NOT NULL, PRIMARY KEY (resolution, captured_at));
				CREATE INDEX idx_cpu_history_captured_at ON cpu_history (captured_at);
				INSERT INTO cpu_history VALUES ('1m', 1, '{}');
				PRAGMA user_version = %d;
			`, version))
			require.NoError(t, err)
			require.NoError(t, db.Close())
			require.NoError(t, os.WriteFile(dbPath+"-shm", []byte("stale shm"), 0o600))

			store, err := OpenStore(t.Context(), tmpDir)
			require.NoError(t, err)
			defer store.Close()

			var got, rows, indexes int
			require.NoError(t, store.db.QueryRow("PRAGMA user_version").Scan(&got))
			assert.Equal(t, storeSchemaVersion, got)
			require.NoError(t, store.db.QueryRow("SELECT COUNT(1) FROM cpu_history").Scan(&rows))
			assert.Zero(t, rows, "the old history is gone")
			require.NoError(t, store.db.QueryRow("SELECT COUNT(1) FROM sqlite_master WHERE type = 'index' AND name LIKE 'idx_%'").Scan(&indexes))
			assert.Zero(t, indexes, "the primary key is the only index")
			moved, err := filepath.Glob(dbPath + ".*")
			require.NoError(t, err)
			assert.Empty(t, moved, "nothing is moved aside")
		})
	}
}

func TestStoreMovesAsideCorruptDatabaseAndRecreatesSchema(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "metrics.db")
	// An older corrupt copy and its sidecar, moved aside by an earlier start.
	const oldStamp = ".corrupt-20200101T000000.000000000Z"
	require.NoError(t, os.WriteFile(dbPath+oldStamp, []byte("older"), 0o600))
	require.NoError(t, os.WriteFile(dbPath+"-wal"+oldStamp, []byte("older wal"), 0o600))
	// A copy an administrator named is neither counted nor deleted.
	const kept = ".corrupt-before-upgrade"
	require.NoError(t, os.WriteFile(dbPath+kept, []byte("kept"), 0o600))
	require.NoError(t, os.WriteFile(dbPath, []byte("this is not sqlite"), 0o600))
	require.NoError(t, os.WriteFile(dbPath+"-wal", []byte("stale wal"), 0o600))
	require.NoError(t, os.WriteFile(dbPath+"-shm", []byte("stale shm"), 0o600))

	store, err := OpenStore(t.Context(), tmpDir)
	require.NoError(t, err)
	defer store.Close()

	assert.FileExists(t, dbPath+kept)
	movedDBs, err := filepath.Glob(dbPath + ".corrupt-2*")
	require.NoError(t, err)
	require.Len(t, movedDBs, 1, "only the newest copy is kept")
	raw, err := os.ReadFile(movedDBs[0])
	require.NoError(t, err)
	assert.Equal(t, "this is not sqlite", string(raw))
	assert.NoFileExists(t, dbPath+"-wal"+oldStamp, "an older copy's sidecars go with it")

	var version int
	require.NoError(t, store.db.QueryRow("PRAGMA user_version").Scan(&version))
	assert.Equal(t, storeSchemaVersion, version)
	require.NoError(t, store.WriteSnapshot(t.Context(), time.Now().UTC().UnixMilli(), storetest.SampleCombinedData(42), store.BeginTick()))
}

func TestStoreDatabaseIntegrity(t *testing.T) {
	s, err := OpenStore(t.Context(), t.TempDir())
	require.NoError(t, err)
	defer s.Close()

	require.NoError(t, s.WriteSnapshot(t.Context(), time.Now().UTC().UnixMilli(), storetest.SampleCombinedData(42), s.BeginTick()))
	require.NoError(t, s.IntegrityCheck(t.Context()))
}

func TestStoreUnknownPluginHistory(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := OpenStore(t.Context(), tmpDir)
	require.NoError(t, err)
	defer store.Close()

	_, err = store.PluginHistory(context.Background(), "nope", resolution1m, 0, 1, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown plugin "nope"`)
}

func TestStoreWritesHistoryOnlyForAllowlistedPlugins(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := OpenStore(t.Context(), tmpDir, Options{HistoryPlugins: []string{plugins.CPU}})
	require.NoError(t, err)
	defer store.Close()

	capturedAt := time.Now().UTC().UnixMilli()
	require.NoError(t, store.WriteSnapshot(t.Context(), capturedAt, storetest.SampleCombinedData(42), store.BeginTick()))

	var cpuRows int
	require.NoError(t, store.db.QueryRow("SELECT COUNT(1) FROM cpu_history").Scan(&cpuRows))
	assert.Equal(t, 1, cpuRows)
	cpuHistory, err := store.PluginHistory(context.Background(), plugins.CPU, resolution1m, 0, capturedAt, 10)
	require.NoError(t, err)
	require.Len(t, cpuHistory, 1)
	var cpu plugins.CPUData
	require.NoError(t, json.Unmarshal(cpuHistory[0].Stats, &cpu))
	assert.InDelta(t, 42.0, cpu.Cpu, 0.0001)

	var memRows int
	require.NoError(t, store.db.QueryRow("SELECT COUNT(1) FROM mem_history").Scan(&memRows))
	assert.Zero(t, memRows)

	_, err = store.PluginHistory(context.Background(), plugins.Mem, resolution1m, 0, capturedAt, 10)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestStoreConcurrentReadsDuringWriteSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := OpenStore(t.Context(), tmpDir, Options{HistoryPlugins: []string{plugins.CPU}})
	require.NoError(t, err)
	defer store.Close()

	start := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, store.WriteSnapshot(t.Context(), start.UnixMilli(), storetest.SampleCombinedData(1), store.BeginTick()))

	errs := make(chan error, 128)
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 50 {
			capturedAt := start.Add(time.Duration(i+1) * time.Millisecond).UnixMilli()
			if writeErr := store.WriteSnapshot(t.Context(), capturedAt, storetest.SampleCombinedData(float64(i)), store.BeginTick()); writeErr != nil {
				errs <- writeErr
			}
		}
	})

	for range 4 {
		wg.Go(func() {
			for range 50 {
				if _, readErr := store.PluginHistory(context.Background(), plugins.CPU, resolution1m, 0, time.Now().Add(time.Hour).UnixMilli(), 10); readErr != nil {
					errs <- readErr
				}
			}
		})
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

func TestStoreMaintenanceAppliesEachTiersRetentionToAllHistoryTables(t *testing.T) {
	tmpDir := t.TempDir()
	retention := 14 * 24 * time.Hour
	store, err := OpenStore(t.Context(), tmpDir, Options{HistoryPlugins: plugins.HistoryCapable(), HistoryRetention: retention})
	require.NoError(t, err)
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Minute)
	keep := map[string]time.Duration{"1m": 24 * time.Hour, "10m": 12 * time.Hour, "20m": 24 * time.Hour, "60m": retention}
	for _, plugin := range plugins.HistoryCapable() {
		for resolution, kept := range keep {
			_, err = store.db.Exec(fmt.Sprintf(`
				INSERT INTO %s (resolution, captured_at, stats_json)
				VALUES (?, ?, '{}'), (?, ?, '{}')
			`, pluginHistoryTable(plugin)), resolution, now.Add(-kept-time.Millisecond).UnixMilli(), resolution, now.Add(-kept).UnixMilli())
			require.NoError(t, err)
		}
	}

	require.NoError(t, store.RunMaintenance(t.Context(), now))

	for _, plugin := range plugins.HistoryCapable() {
		for resolution, kept := range keep {
			var count, boundary int
			require.NoError(t, store.db.QueryRow(fmt.Sprintf(`SELECT COUNT(1), COUNT(CASE WHEN captured_at = ? THEN 1 END) FROM %s WHERE resolution = ?`, pluginHistoryTable(plugin)), now.Add(-kept).UnixMilli(), resolution).Scan(&count, &boundary))
			assert.Equal(t, [2]int{1, 1}, [2]int{count, boundary}, "%s %s keeps its boundary row only", plugin, resolution)
		}
	}
}

// history.retention is the hourly tier's length and bounds every tier.
func TestTierRetentionIsBoundedByHistoryRetention(t *testing.T) {
	assert.Equal(t, map[string]string{"1m": "24h0m0s", "10m": "12h0m0s", "20m": "24h0m0s", "60m": "720h0m0s"}, RetentionStrings())
	assert.Equal(t, map[string]string{"1m": "6h0m0s", "10m": "6h0m0s", "20m": "6h0m0s", "60m": "6h0m0s"}, retentionStrings(6*time.Hour))
}

func TestStoreMaintenanceCullsDisabledPluginTables(t *testing.T) {
	tmpDir := t.TempDir()
	retention := time.Hour
	store, err := OpenStore(t.Context(), tmpDir, Options{HistoryPlugins: []string{plugins.CPU}, HistoryRetention: retention})
	require.NoError(t, err)
	defer store.Close()
	now := time.Now().UTC().Truncate(time.Minute)
	_, err = store.db.Exec(`INSERT INTO mem_history (resolution, captured_at, stats_json) VALUES ('1m', ?, '{}')`, now.Add(-retention-time.Millisecond).UnixMilli())
	require.NoError(t, err)

	require.NoError(t, store.RunMaintenance(t.Context(), now))

	var rows int
	require.NoError(t, store.db.QueryRow("SELECT COUNT(1) FROM mem_history").Scan(&rows))
	assert.Zero(t, rows)
}

func TestStoreWritesAbortOnCancelledContext(t *testing.T) {
	store, err := OpenStore(t.Context(), t.TempDir(), Options{HistoryPlugins: []string{plugins.CPU, plugins.Smart}})
	require.NoError(t, err)
	defer store.Close()
	_, err = store.db.Exec(`INSERT INTO cpu_history (resolution, captured_at, stats_json) VALUES ('1m', 0, '{}')`)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	now := time.Now().UTC()
	require.ErrorIs(t, store.WriteSnapshot(ctx, now.UnixMilli(), storetest.SampleCombinedData(42), store.BeginTick()), context.Canceled)
	require.ErrorIs(t, store.WriteSmartDevices(ctx, now.UnixMilli(), map[string]smartmodel.SmartData{}), context.Canceled)
	require.ErrorIs(t, store.RunMaintenance(ctx, now), context.Canceled)
	require.ErrorIs(t, store.Rollup(ctx, now), context.Canceled)

	var cpuRows, smartRows int
	require.NoError(t, store.db.QueryRow("SELECT COUNT(1) FROM cpu_history").Scan(&cpuRows))
	require.NoError(t, store.db.QueryRow("SELECT COUNT(1) FROM smart_history").Scan(&smartRows))
	assert.Equal(t, 1, cpuRows, "neither the snapshot insert nor the expired-row delete ran")
	assert.Zero(t, smartRows)
}

func TestStoredTiersAreTheContractResolutions(t *testing.T) {
	resolutions := make([]string, 0, len(tiers))
	for i, tier := range tiers {
		resolutions = append(resolutions, tier.resolution)
		step, ok := monitoringapi.HistoryResolutionStep(tier.resolution)
		require.True(t, ok, tier.resolution)
		assert.Equal(t, step, tier.step, tier.resolution)
		if i > 0 {
			assert.Zero(t, tier.step%tiers[i-1].step, "a %s bucket holds whole %s buckets", tier.resolution, tiers[i-1].resolution)
		}
	}
	assert.Equal(t, monitoringapi.HistoryResolutions(), resolutions)
	for _, gone := range []string{"120m", "480m", "5m", ""} {
		assert.False(t, ValidResolution(gone), gone)
	}
}

// An hour-aligned time, so the 10m, 20m and 60m buckets all start at it.
const rollupBase = int64(472_222) * 3_600_000

func insertRow(t *testing.T, store *Store, plugin, resolution string, at int64, raw string) {
	t.Helper()
	_, err := store.db.Exec(fmt.Sprintf("INSERT INTO %s (resolution, captured_at, stats_json) VALUES (?, ?, ?)", pluginHistoryTable(plugin)), resolution, at, raw)
	require.NoError(t, err)
}

func tierRows(t *testing.T, store *Store, plugin, resolution string) []HistoryRecord[jsontext.Value] {
	t.Helper()
	rows, err := store.PluginHistory(t.Context(), plugin, resolution, 0, rollupBase+24*3_600_000, 1000)
	require.NoError(t, err)
	return rows
}

func TestRollupAveragesEachClosedBucketIntoTheNextTier(t *testing.T) {
	store, err := OpenStore(t.Context(), t.TempDir(), Options{HistoryPlugins: []string{plugins.CPU}})
	require.NoError(t, err)
	defer store.Close()
	for i, sample := range []string{
		`{"cpu_percent":10,"max_cpu_percent":15,"cpu_breakdown_percent":[10,20],"cpu_cores_percent":[10,20],"cpu_frequencies_mhz":[3000,2000]}`,
		`{"cpu_percent":20,"max_cpu_percent":18,"cpu_breakdown_percent":[20,30],"cpu_cores_percent":[20,30],"cpu_frequencies_mhz":[3300,2200]}`,
		`{"cpu_percent":30,"max_cpu_percent":35,"cpu_breakdown_percent":[30,40],"cpu_cores_percent":[30,40],"cpu_frequencies_mhz":[3600,2400]}`,
	} {
		insertRow(t, store, plugins.CPU, "1m", rollupBase+int64(i)*60_000, sample)
	}
	insertRow(t, store, plugins.CPU, "1m", rollupBase+10*60_000, `{"cpu_percent":40}`)

	// At minute 11 only the first 10m bucket has closed.
	require.NoError(t, store.Rollup(t.Context(), time.UnixMilli(rollupBase+11*60_000)))
	rows := tierRows(t, store, plugins.CPU, "10m")
	require.Len(t, rows, 1)
	assert.Equal(t, rollupBase+120_000, rows[0].CapturedAt, "a row's time is its newest sample")
	var cpu plugins.CPUData
	require.NoError(t, json.Unmarshal(rows[0].Stats, &cpu))
	assert.InDelta(t, 20, cpu.Cpu, 0.001)
	assert.InDelta(t, 35, cpu.MaxCpu, 0.001)
	assert.Equal(t, []float64{20, 30}, cpu.CpuBreakdown)
	assert.Equal(t, []uint8{20, 30}, []uint8(cpu.CpuCoresUsage))
	assert.Equal(t, []float64{3300, 2200}, cpu.CpuFrequencies)
	assert.Empty(t, tierRows(t, store, plugins.CPU, "20m"))
	assert.Empty(t, tierRows(t, store, plugins.CPU, "60m"))

	// At minute 61 the hour has closed: the second 10m bucket, then the 20m
	// bucket from both 10m rows, then the hour from that 20m row.
	at := time.UnixMilli(rollupBase + 61*60_000)
	require.NoError(t, store.Rollup(t.Context(), at))
	require.NoError(t, store.Rollup(t.Context(), at), "a second pass changes nothing")
	assert.Len(t, tierRows(t, store, plugins.CPU, "10m"), 2)
	for _, resolution := range []string{"20m", "60m"} {
		rows := tierRows(t, store, plugins.CPU, resolution)
		require.Len(t, rows, 1, resolution)
		assert.Equal(t, rollupBase+600_000, rows[0].CapturedAt, resolution)
		require.NoError(t, json.Unmarshal(rows[0].Stats, &cpu))
		assert.InDelta(t, 30, cpu.Cpu, 0.001, resolution)
	}
}

// A row that does not decode costs its own bucket; the next bucket and the
// next tier still roll up.
func TestRollupSkipsABucketThatDoesNotDecode(t *testing.T) {
	store, err := OpenStore(t.Context(), t.TempDir(), Options{HistoryPlugins: []string{plugins.CPU}})
	require.NoError(t, err)
	defer store.Close()
	insertRow(t, store, plugins.CPU, "1m", rollupBase, `{"cpu_percent":"not a number"}`)
	insertRow(t, store, plugins.CPU, "1m", rollupBase+10*60_000, `{"cpu_percent":20}`)

	require.NoError(t, store.Rollup(t.Context(), time.UnixMilli(rollupBase+20*60_000)))
	rows := tierRows(t, store, plugins.CPU, "10m")
	require.Len(t, rows, 1)
	assert.Equal(t, rollupBase+10*60_000, rows[0].CapturedAt)
	require.Len(t, tierRows(t, store, plugins.CPU, "20m"), 1)
}

func TestRollupAfterARestartNeitherRepeatsNorSkipsABucket(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(t.Context(), dir, Options{HistoryPlugins: []string{plugins.CPU}})
	require.NoError(t, err)
	insertRow(t, store, plugins.CPU, "1m", rollupBase, `{"cpu_percent":10}`)
	require.NoError(t, store.Rollup(t.Context(), time.UnixMilli(rollupBase+10*60_000)))
	insertRow(t, store, plugins.CPU, "1m", rollupBase+10*60_000, `{"cpu_percent":20}`)
	require.NoError(t, store.Close())

	store, err = OpenStore(t.Context(), dir, Options{HistoryPlugins: []string{plugins.CPU}})
	require.NoError(t, err)
	defer store.Close()
	// The first bucket is rolled up already; the second closed while stopped.
	require.NoError(t, store.Rollup(t.Context(), time.UnixMilli(rollupBase+25*60_000)))
	rows := tierRows(t, store, plugins.CPU, "10m")
	require.Len(t, rows, 2)
	assert.Equal(t, []int64{rollupBase, rollupBase + 10*60_000}, []int64{rows[0].CapturedAt, rows[1].CapturedAt})
}

// A container in a rollup is keyed by name, so one recreated under a new ID
// stays one series with the newest record's ID and details, and is divided by
// every row of the bucket, so one absent from a row counts as zero there.
func TestRollupAveragesContainersByNameOverEveryRow(t *testing.T) {
	store, err := OpenStore(t.Context(), t.TempDir(), Options{HistoryPlugins: []string{plugins.Containers}})
	require.NoError(t, err)
	defer store.Close()
	insertRow(t, store, plugins.Containers, "1m", rollupBase, `[{"id":"c1","name":"web","image":"v1","cpu_percent":10,"memory_mb":100},{"id":"c2","name":"job","cpu_percent":50,"memory_mb":200}]`)
	insertRow(t, store, plugins.Containers, "1m", rollupBase+60_000, `[{"id":"c3","name":"web","image":"v2","cpu_percent":30,"memory_mb":300}]`)

	require.NoError(t, store.Rollup(t.Context(), time.UnixMilli(rollupBase+10*60_000)))
	rows := tierRows(t, store, plugins.Containers, "10m")
	require.Len(t, rows, 1)
	var records []struct {
		ID    string  `json:"id"`
		Name  string  `json:"name"`
		Image string  `json:"image"`
		Cpu   float64 `json:"cpu_percent"`
	}
	require.NoError(t, json.Unmarshal(rows[0].Stats, &records))
	require.Len(t, records, 2)
	assert.Equal(t, "c2", records[0].ID)
	assert.InDelta(t, 25, records[0].Cpu, 0.001, "a container absent from a row counts as zero there")
	assert.Equal(t, "c3", records[1].ID, "a recreated container keeps its newest ID")
	assert.Equal(t, "web", records[1].Name)
	assert.Equal(t, "v2", records[1].Image, "latest metadata should be retained")
	assert.InDelta(t, 20, records[1].Cpu, 0.001)
}

func TestRecoverableStoreOpenErrorMatchesMattnCodes(t *testing.T) {
	if !recoverableStoreOpenError(sqlite3.Error{Code: sqlite3.ErrCorrupt}) {
		t.Fatal("ErrCorrupt should be recoverable")
	}
	if !recoverableStoreOpenError(sqlite3.Error{Code: sqlite3.ErrNotADB}) {
		t.Fatal("ErrNotADB should be recoverable")
	}
	if recoverableStoreOpenError(sqlite3.Error{Code: sqlite3.ErrBusy}) {
		t.Fatal("ErrBusy must not be recoverable")
	}
	if recoverableStoreOpenError(errors.New("plain")) {
		t.Fatal("non-sqlite errors must not be recoverable")
	}
}

func TestPluginHistoryReturnsTheNewestStoredRowsOfOneTier(t *testing.T) {
	store, err := OpenStore(t.Context(), t.TempDir(), Options{HistoryPlugins: []string{plugins.CPU}})
	require.NoError(t, err)
	defer store.Close()
	for i := range 5 {
		insertRow(t, store, plugins.CPU, "1m", rollupBase+int64(i)*60_000, fmt.Sprintf(`{"cpu_percent":%d}`, i))
		insertRow(t, store, plugins.CPU, "10m", rollupBase+int64(i)*600_000, fmt.Sprintf(`{"cpu_percent":%d}`, 10+i))
	}

	rows, err := store.PluginHistory(t.Context(), plugins.CPU, resolution1m, 0, rollupBase+180_000, 2)
	require.NoError(t, err)
	assert.Equal(t, []int64{rollupBase + 120_000, rollupBase + 180_000}, []int64{rows[0].CapturedAt, rows[1].CapturedAt}, "newest rows within the range, ascending")

	rows, err = store.PluginHistory(t.Context(), plugins.CPU, "10m", rollupBase+600_000, rollupBase+24*3_600_000, 10)
	require.NoError(t, err)
	require.Len(t, rows, 4, "the stored 10m rows from the start of the range, nothing averaged at read")
	assert.JSONEq(t, `{"cpu_percent":11}`, string(rows[0].Stats))

	rows, err = store.PluginHistory(t.Context(), plugins.CPU, "60m", 0, rollupBase+24*3_600_000, 10)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func TestWriteSnapshotHonoursHistoryEvery(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(t.Context(), t.TempDir(), Options{
		HistoryPlugins: []string{plugins.CPU, plugins.Mem},
		HistoryEvery:   map[string]uint64{plugins.Mem: 3},
	})
	require.NoError(t, err)
	defer store.Close()

	start := time.Now().UTC().Truncate(time.Minute).Add(-time.Hour)
	for i := range 4 {
		capturedAt := start.Add(time.Duration(i) * time.Minute).UnixMilli()
		require.NoError(t, store.WriteSnapshot(t.Context(), capturedAt, storetest.SampleCombinedData(float64(i)), store.BeginTick()))
	}
	end := start.Add(time.Hour).UnixMilli()

	cpu, err := store.PluginHistory(ctx, plugins.CPU, resolution1m, 0, end, 10)
	require.NoError(t, err)
	assert.Len(t, cpu, 4, "plugins without an interval write every tick")

	mem, err := store.PluginHistory(ctx, plugins.Mem, resolution1m, 0, end, 10)
	require.NoError(t, err)
	require.Len(t, mem, 2, "every third tick, starting with the first")
	assert.Equal(t, start.UnixMilli(), mem[0].CapturedAt)
	assert.Equal(t, start.Add(3*time.Minute).UnixMilli(), mem[1].CapturedAt)

	// Reload to every tick: the next snapshot writes mem again.
	store.SetHistoryEvery(nil)
	require.NoError(t, store.WriteSnapshot(t.Context(), start.Add(4*time.Minute).UnixMilli(), storetest.SampleCombinedData(4), store.BeginTick()))
	mem, err = store.PluginHistory(ctx, plugins.Mem, resolution1m, 0, end, 10)
	require.NoError(t, err)
	assert.Len(t, mem, 3)
}

func TestOpenStoreAppliesPragmasToAnyConnectionAndEscapesPath(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "a b#c?d%e")
	require.NoError(t, os.Mkdir(dataDir, 0o700))

	store, err := OpenStore(t.Context(), dataDir)
	require.NoError(t, err)
	defer store.Close()

	_, err = os.Stat(filepath.Join(dataDir, "metrics.db"))
	require.NoError(t, err)
	entries, err := os.ReadDir(filepath.Dir(dataDir))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no stray database at a truncated path")

	// A discarded connection (released as bad by Raw) is replaced by one that must carry the settings too.
	conn, err := store.db.Conn(context.Background())
	require.NoError(t, err)
	require.ErrorIs(t, conn.Raw(func(any) error { return driver.ErrBadConn }), driver.ErrBadConn)

	want := map[string]string{"busy_timeout": "5000", "synchronous": "1", "foreign_keys": "1", "journal_mode": "wal"}
	for pragma, expected := range want {
		var got string
		require.NoError(t, store.db.QueryRow("PRAGMA "+pragma).Scan(&got))
		assert.Equal(t, expected, got, pragma)
	}
}
