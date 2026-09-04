package store

import (
	"context"
	"database/sql"
	"encoding/json"
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

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store/storetest"
)

func createStoreDBWithVersion(t *testing.T, dataDir string, version int) {
	t.Helper()

	db, err := sql.Open("sqlite3", filepath.Join(dataDir, "metrics.db"))
	require.NoError(t, err)
	_, err = db.Exec(fmt.Sprintf("PRAGMA user_version = %d", version))
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

func TestStoreSnapshotAndHistoryQueries(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	store, err := OpenStore(tmpDir)
	require.NoError(t, err)
	defer store.Close()

	capturedAt := time.Now().UTC().UnixMilli()
	require.NoError(t, store.WriteSnapshot(capturedAt, storetest.SampleCombinedData(42)))
	var tableCount int
	require.NoError(t, store.db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND (name = 'meta' OR name LIKE '%_current')
	`).Scan(&tableCount))
	assert.Zero(t, tableCount, "snapshot/current tables should not exist")

	records, err := store.PluginHistory(ctx, PluginCPU, resolution1m, 0, capturedAt, 10)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, capturedAt, records[0].CapturedAt)
	var cpu CPUData
	require.NoError(t, json.Unmarshal(records[0].Stats, &cpu))
	assert.InDelta(t, 42.0, cpu.Cpu, 0.0001)

	telemetry, err := store.PluginHistory(ctx, PluginContainerTelemetry, resolution1m, 0, capturedAt, 10)
	require.NoError(t, err)
	require.Len(t, telemetry, 1)
	assert.Contains(t, string(telemetry[0].Stats), `"disk_read_bytes_per_second":400`)

	allPlugins := PluginNames()
	require.Contains(t, allPlugins, PluginCPU)
	require.Contains(t, allPlugins, PluginSmart)

	_, err = os.Stat(filepath.Join(tmpDir, "metrics.db"))
	require.NoError(t, err)
}

func TestStoreMigratesV4AndV5ToV7HistoryOnly(t *testing.T) {
	for _, oldVersion := range []int{4, 5} {
		t.Run(fmt.Sprintf("v%d", oldVersion), func(t *testing.T) {
			tmpDir := t.TempDir()
			dbPath := filepath.Join(tmpDir, "metrics.db")
			db, err := sql.Open("sqlite3", dbPath)
			require.NoError(t, err)
			_, err = db.Exec("CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL)")
			require.NoError(t, err)
			for _, plugin := range pluginNames {
				_, err = db.Exec(fmt.Sprintf("CREATE TABLE %s_current (singleton INTEGER PRIMARY KEY, captured_at INTEGER NOT NULL, data_json TEXT NOT NULL)", plugin))
				require.NoError(t, err)
			}
			for _, plugin := range []string{PluginCPU, PluginProcesses, PluginPrograms} {
				_, err = db.Exec(fmt.Sprintf("CREATE TABLE %s_history (resolution TEXT NOT NULL, captured_at INTEGER NOT NULL, stats_json TEXT NOT NULL, PRIMARY KEY (resolution, captured_at))", plugin))
				require.NoError(t, err)
			}
			_, err = db.Exec(`
				INSERT INTO meta VALUES ('secret', 'sensitive-meta');
				INSERT INTO processes_current VALUES (1, 1, '{"pid":99}');
				INSERT INTO processes_history VALUES ('1m', 1, '{"secret":true}');
				INSERT INTO programs_current VALUES (1, 1, '[{"name":"secret"}]');
				INSERT INTO programs_history VALUES ('1m', 1, '{"secret":true}');
				INSERT INTO cpu_history VALUES ('1m', 2, '{"cpu_percent":7}');
				INSERT INTO cpu_history VALUES ('10m', 3, '{"cpu_percent":8}');
			`)
			require.NoError(t, err)
			_, err = db.Exec(fmt.Sprintf("PRAGMA user_version = %d", oldVersion))
			require.NoError(t, err)
			require.NoError(t, db.Close())

			store, err := OpenStore(tmpDir)
			require.NoError(t, err)
			var version int
			require.NoError(t, store.db.QueryRow("PRAGMA user_version").Scan(&version))
			assert.Equal(t, storeSchemaVersion, version)
			var tableCount int
			require.NoError(t, store.db.QueryRow(`
				SELECT COUNT(*) FROM sqlite_master
				WHERE type = 'table' AND (name = 'meta' OR name LIKE '%_current' OR name IN ('processes_history', 'programs_history'))
			`).Scan(&tableCount))
			assert.Zero(t, tableCount)
			var capturedAt int64
			var raw string
			require.NoError(t, store.db.QueryRow("SELECT captured_at, stats_json FROM cpu_history WHERE resolution = '1m'").Scan(&capturedAt, &raw))
			assert.Equal(t, int64(2), capturedAt)
			assert.JSONEq(t, `{"cpu_percent":7}`, raw)
			var legacyRows int
			require.NoError(t, store.db.QueryRow("SELECT COUNT(*) FROM cpu_history WHERE resolution <> '1m'").Scan(&legacyRows))
			assert.Zero(t, legacyRows)
			require.NoError(t, store.Close())

			databaseBytes, err := os.ReadFile(dbPath)
			require.NoError(t, err)
			assert.NotContains(t, string(databaseBytes), "sensitive-meta")
			assert.NotContains(t, string(databaseBytes), `"pid":99`)
			assert.NotContains(t, string(databaseBytes), `"secret"`)
			legacyCopies, err := filepath.Glob(dbPath + ".*")
			require.NoError(t, err)
			assert.Empty(t, legacyCopies, "migration must not retain a legacy database copy")
		})
	}
}

func TestStoreMigratesV6ToV7WithoutDroppingFullResolutionHistory(t *testing.T) {
	tmpDir := t.TempDir()
	db, err := sql.Open("sqlite3", filepath.Join(tmpDir, "metrics.db"))
	require.NoError(t, err)
	_, err = db.Exec(`
		CREATE TABLE cpu_history (
			resolution TEXT NOT NULL,
			captured_at INTEGER NOT NULL,
			stats_json TEXT NOT NULL,
			PRIMARY KEY (resolution, captured_at)
		);
		INSERT INTO cpu_history VALUES ('1m', 1, '{"cpu_percent":7}');
		INSERT INTO cpu_history VALUES ('10m', 2, '{"cpu_percent":8}');
		PRAGMA user_version = 6;
	`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := OpenStore(tmpDir)
	require.NoError(t, err)
	defer store.Close()

	var version int
	require.NoError(t, store.db.QueryRow("PRAGMA user_version").Scan(&version))
	assert.Equal(t, storeSchemaVersion, version)
	var capturedAt int64
	var raw string
	require.NoError(t, store.db.QueryRow("SELECT captured_at, stats_json FROM cpu_history").Scan(&capturedAt, &raw))
	assert.Equal(t, int64(1), capturedAt)
	assert.JSONEq(t, `{"cpu_percent":7}`, raw)
}

func TestStoreRejectsNilSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := OpenStore(tmpDir)
	require.NoError(t, err)
	defer store.Close()

	err = store.WriteSnapshot(time.Now().UTC().UnixMilli(), nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "snapshot data is nil")
}

func TestStoreRejectsUnsupportedSchemaVersion(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "metrics.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	_, err = db.Exec(`
		PRAGMA user_version = 3;
	`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store, err := OpenStore(tmpDir)
	require.Error(t, err)
	assert.Nil(t, store)
	assert.Contains(t, err.Error(), "unsupported store schema version 3")
}

func TestStoreMovesAsideCorruptDatabaseAndRecreatesSchema(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "metrics.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("this is not sqlite"), 0600))
	require.NoError(t, os.WriteFile(dbPath+"-wal", []byte("stale wal"), 0600))
	require.NoError(t, os.WriteFile(dbPath+"-shm", []byte("stale shm"), 0600))

	store, err := OpenStore(tmpDir)
	require.NoError(t, err)
	defer store.Close()

	movedDBs, err := filepath.Glob(dbPath + ".corrupt-*")
	require.NoError(t, err)
	require.Len(t, movedDBs, 1)
	raw, err := os.ReadFile(movedDBs[0])
	require.NoError(t, err)
	assert.Equal(t, "this is not sqlite", string(raw))

	var version int
	require.NoError(t, store.db.QueryRow("PRAGMA user_version").Scan(&version))
	assert.Equal(t, storeSchemaVersion, version)
	require.NoError(t, store.WriteSnapshot(time.Now().UTC().UnixMilli(), storetest.SampleCombinedData(42)))
}

func TestStoreDatabaseIntegrityMaintenanceAndReset(t *testing.T) {
	tmpDir := t.TempDir()
	s, err := OpenStore(tmpDir)
	require.NoError(t, err)

	require.NoError(t, s.WriteSnapshot(time.Now().UTC().UnixMilli(), storetest.SampleCombinedData(42)))
	require.NoError(t, s.IntegrityCheck())
	require.NoError(t, s.Vacuum())
	require.NoError(t, CheckDatabase(tmpDir))
	require.NoError(t, s.Close())

	dbPath := filepath.Join(tmpDir, "metrics.db")
	resetStore, moved, err := ResetDatabase(tmpDir)
	require.NoError(t, err)
	defer resetStore.Close()
	require.NotEmpty(t, moved)
	assert.FileExists(t, dbPath)

	movedDBs, err := filepath.Glob(dbPath + ".reset-*")
	require.NoError(t, err)
	require.Len(t, movedDBs, 1)
	require.NoError(t, resetStore.IntegrityCheck())
	require.NoError(t, CheckDatabase(tmpDir))
}

func TestCheckDatabaseErrors(t *testing.T) {
	t.Run("missing database", func(t *testing.T) {
		err := CheckDatabase(t.TempDir())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "stat metrics.db")
	})

	t.Run("obsolete schema", func(t *testing.T) {
		tmpDir := t.TempDir()
		createStoreDBWithVersion(t, tmpDir, 3)

		err := CheckDatabase(tmpDir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "obsolete metrics.db schema version 3")
	})

	t.Run("unsupported schema", func(t *testing.T) {
		tmpDir := t.TempDir()
		createStoreDBWithVersion(t, tmpDir, 99)

		err := CheckDatabase(tmpDir)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported metrics.db schema version 99")
	})
}

func TestStoreRepairValidDatabaseDoesNotMoveFiles(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := OpenStore(tmpDir)
	require.NoError(t, err)
	require.NoError(t, store.Close())

	repaired, moved, err := RepairDatabase(tmpDir)

	require.NoError(t, err)
	defer repaired.Close()
	assert.Empty(t, moved)
	require.NoError(t, repaired.IntegrityCheck())
}

func TestStoreResetDatabaseWithoutExistingDB(t *testing.T) {
	tmpDir := t.TempDir()

	resetStore, moved, err := ResetDatabase(tmpDir)

	require.NoError(t, err)
	defer resetStore.Close()
	assert.Empty(t, moved)
	assert.FileExists(t, filepath.Join(tmpDir, "metrics.db"))
	require.NoError(t, resetStore.IntegrityCheck())
}

func TestStoreRepairMovesAsideCorruptDatabase(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "metrics.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("this is not sqlite"), 0600))

	repaired, moved, err := RepairDatabase(tmpDir)
	require.NoError(t, err)
	defer repaired.Close()
	require.NotEmpty(t, moved)

	movedDBs, err := filepath.Glob(dbPath + ".repair-*")
	require.NoError(t, err)
	require.Len(t, movedDBs, 1)
	require.NoError(t, repaired.IntegrityCheck())
	require.NoError(t, CheckDatabase(tmpDir))
}

func TestStoreUnknownPluginHistory(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := OpenStore(tmpDir)
	require.NoError(t, err)
	defer store.Close()

	_, err = store.PluginHistory(context.Background(), "nope", resolution1m, 0, 1, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown plugin "nope"`)
}

func TestStoreWritesHistoryOnlyForAllowlistedPlugins(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := OpenStore(tmpDir, Options{HistoryPlugins: []string{PluginCPU}})
	require.NoError(t, err)
	defer store.Close()

	capturedAt := time.Now().UTC().UnixMilli()
	require.NoError(t, store.WriteSnapshot(capturedAt, storetest.SampleCombinedData(42)))

	var cpuRows int
	require.NoError(t, store.db.QueryRow("SELECT COUNT(1) FROM cpu_history").Scan(&cpuRows))
	assert.Equal(t, 1, cpuRows)
	cpuHistory, err := store.PluginHistory(context.Background(), PluginCPU, resolution1m, 0, capturedAt, 10)
	require.NoError(t, err)
	require.Len(t, cpuHistory, 1)
	var cpu CPUData
	require.NoError(t, json.Unmarshal(cpuHistory[0].Stats, &cpu))
	assert.InDelta(t, 42.0, cpu.Cpu, 0.0001)

	var memRows int
	require.NoError(t, store.db.QueryRow("SELECT COUNT(1) FROM mem_history").Scan(&memRows))
	assert.Zero(t, memRows)

	_, err = store.PluginHistory(context.Background(), PluginMem, resolution1m, 0, capturedAt, 10)
	assert.ErrorIs(t, err, sql.ErrNoRows)
}

func TestParseHistoryPlugins(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		plugins, err := ParseHistoryPlugins("", false)
		require.NoError(t, err)
		assert.Equal(t, DefaultHistoryPluginNames(), plugins)
	})

	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("HISTORY", "cpu,swap")
		plugins, err := ParseHistoryPlugins("", false)
		require.NoError(t, err)
		assert.Equal(t, []string{PluginCPU, PluginSwap}, plugins)
	})

	t.Run("explicit all", func(t *testing.T) {
		plugins, err := ParseHistoryPlugins("all", true)
		require.NoError(t, err)
		assert.Equal(t, historyCapablePluginNames(), plugins)
		assert.NotContains(t, plugins, PluginProcesses)
		assert.NotContains(t, plugins, PluginPrograms)
	})

	t.Run("explicit none", func(t *testing.T) {
		plugins, err := ParseHistoryPlugins("none", true)
		require.NoError(t, err)
		assert.Empty(t, plugins)
	})

	t.Run("invalid plugin", func(t *testing.T) {
		_, err := ParseHistoryPlugins("cpu,nope", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), `unknown history plugin "nope"`)
	})

	t.Run("live-only process plugins", func(t *testing.T) {
		for _, plugin := range []string{PluginProcesses, PluginPrograms} {
			_, err := ParseHistoryPlugins(plugin, true)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "live-only")
		}
	})
}

func TestStoreConcurrentReadsDuringWriteSnapshot(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := OpenStore(tmpDir, Options{HistoryPlugins: []string{PluginCPU}})
	require.NoError(t, err)
	defer store.Close()

	start := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, store.WriteSnapshot(start.UnixMilli(), storetest.SampleCombinedData(1)))

	errs := make(chan error, 128)
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 50 {
			capturedAt := start.Add(time.Duration(i+1) * time.Millisecond).UnixMilli()
			if writeErr := store.WriteSnapshot(capturedAt, storetest.SampleCombinedData(float64(i))); writeErr != nil {
				errs <- writeErr
			}
		}
	})

	for range 4 {
		wg.Go(func() {
			for range 50 {
				if _, readErr := store.PluginHistory(context.Background(), PluginCPU, resolution1m, 0, time.Now().Add(time.Hour).UnixMilli(), 10); readErr != nil {
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

func TestStoreMaintenanceAppliesRetentionToAllHistoryTables(t *testing.T) {
	tmpDir := t.TempDir()
	retention := 14 * 24 * time.Hour
	store, err := OpenStore(tmpDir, Options{HistoryPlugins: historyCapablePluginNames(), HistoryRetention: retention})
	require.NoError(t, err)
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Minute)
	historyJSON := `{"retention":"test"}`

	expiredAt := now.Add(-retention - time.Millisecond).UnixMilli()
	boundaryAt := now.Add(-retention).UnixMilli()
	for _, plugin := range historyCapablePluginNames() {
		_, err = store.db.Exec(fmt.Sprintf(`
			INSERT INTO %s (resolution, captured_at, stats_json)
			VALUES (?, ?, ?), (?, ?, ?)
		`, pluginHistoryTable(plugin)), resolution1m, expiredAt, historyJSON, resolution1m, boundaryAt, historyJSON)
		require.NoError(t, err)
	}

	require.NoError(t, store.RunMaintenance(now))

	cutoff := now.Add(-retention).UnixMilli()
	for _, plugin := range historyCapablePluginNames() {
		var expiredRows int
		err = store.db.QueryRow(fmt.Sprintf(`
			SELECT COUNT(1)
			FROM %s
			WHERE resolution = ? AND captured_at < ?
		`, pluginHistoryTable(plugin)), resolution1m, cutoff).Scan(&expiredRows)
		require.NoError(t, err)
		assert.Zero(t, expiredRows, "expired rows should be deleted for %s", plugin)

		var keptRows int
		err = store.db.QueryRow(fmt.Sprintf(`
			SELECT COUNT(1)
			FROM %s
			WHERE resolution = ? AND captured_at = ?
		`, pluginHistoryTable(plugin)), resolution1m, cutoff).Scan(&keptRows)
		require.NoError(t, err)
		assert.Equal(t, 1, keptRows, "boundary row should be kept for %s", plugin)
	}
}

func TestStoreMaintenanceCullsDisabledPluginTables(t *testing.T) {
	tmpDir := t.TempDir()
	retention := time.Hour
	store, err := OpenStore(tmpDir, Options{HistoryPlugins: []string{PluginCPU}, HistoryRetention: retention})
	require.NoError(t, err)
	defer store.Close()
	now := time.Now().UTC().Truncate(time.Minute)
	_, err = store.db.Exec(`INSERT INTO mem_history (resolution, captured_at, stats_json) VALUES ('1m', ?, '{}')`, now.Add(-retention-time.Millisecond).UnixMilli())
	require.NoError(t, err)

	require.NoError(t, store.RunMaintenance(now))

	var rows int
	require.NoError(t, store.db.QueryRow("SELECT COUNT(1) FROM mem_history").Scan(&rows))
	assert.Zero(t, rows)
}

func TestHistorySupportsQueryResolutions(t *testing.T) {
	assert.True(t, ValidResolution(resolution1m))
	for _, resolution := range []string{"10m", "20m", "120m", "480m"} {
		assert.True(t, ValidResolution(resolution))
	}
	assert.Equal(t, map[string]string{resolution1m: (30 * 24 * time.Hour).String()}, RetentionStrings())
}

func TestPluginHistoryDownsamplesAtQueryTimeAndAppliesLimitToNewest(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := OpenStore(tmpDir, Options{HistoryPlugins: []string{PluginCPU}})
	require.NoError(t, err)
	defer store.Close()
	base := int64(1_700_000_000_000)
	samples := []string{
		`{"cpu_percent":10,"max_cpu_percent":15,"cpu_breakdown_percent":[10,20],"cpu_cores_percent":[10,20]}`,
		`{"cpu_percent":20,"max_cpu_percent":18,"cpu_breakdown_percent":[20,30],"cpu_cores_percent":[20,30]}`,
		`{"cpu_percent":30,"max_cpu_percent":35,"cpu_breakdown_percent":[30,40],"cpu_cores_percent":[30,40]}`,
	}
	for i, sample := range samples {
		_, err = store.db.Exec("INSERT INTO cpu_history (resolution, captured_at, stats_json) VALUES ('1m', ?, ?)", base+int64(i)*60_000, sample)
		require.NoError(t, err)
	}
	rows, err := store.PluginHistory(context.Background(), PluginCPU, "10m", base, base+180_000, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, base+120_000, rows[0].CapturedAt)
	var cpu CPUData
	require.NoError(t, json.Unmarshal(rows[0].Stats, &cpu))
	assert.InDelta(t, 20, cpu.Cpu, 0.001)
	assert.InDelta(t, 35, cpu.MaxCpu, 0.001)
	assert.Equal(t, []float64{20, 30}, cpu.CpuBreakdown)
	assert.Equal(t, []uint8{20, 30}, []uint8(cpu.CpuCoresUsage))
}

func TestPluginHistoryDownsamplesContainersByStableIdentityAndPresence(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := OpenStore(tmpDir, Options{HistoryPlugins: []string{PluginContainers}})
	require.NoError(t, err)
	defer store.Close()
	base := int64(1_700_000_000_000)
	samples := []string{
		`[{"id":"c1","name":"web","image":"v1","cpu_percent":10,"memory_mb":100},{"id":"c2","name":"job","cpu_percent":50,"memory_mb":200}]`,
		`[{"id":"c1","name":"web","image":"v2","cpu_percent":30,"memory_mb":300}]`,
	}
	for i, sample := range samples {
		_, err = store.db.Exec("INSERT INTO containers_history (resolution, captured_at, stats_json) VALUES ('1m', ?, ?)", base+int64(i)*60_000, sample)
		require.NoError(t, err)
	}

	rows, err := store.PluginHistory(context.Background(), PluginContainers, "10m", base, base+120_000, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	var records []containerSnapshotRecord
	require.NoError(t, json.Unmarshal(rows[0].Stats, &records))
	require.Len(t, records, 2)
	assert.Equal(t, "c1", records[0].ID)
	assert.Equal(t, "v2", records[0].Image, "latest metadata should be retained")
	assert.InDelta(t, 20, records[0].Cpu, 0.001)
	assert.Equal(t, "c2", records[1].ID)
	assert.InDelta(t, 50, records[1].Cpu, 0.001, "an absent entity is not a zero sample")
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

func TestPluginHistoryBoundsTheScanToTheNewestRowsAndBuckets(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := OpenStore(tmpDir, Options{HistoryPlugins: []string{PluginCPU}})
	require.NoError(t, err)
	defer store.Close()
	base := int64(1_700_000_000_000)
	insert := func(at int64, cpu int) {
		_, execErr := store.db.Exec("INSERT INTO cpu_history (resolution, captured_at, stats_json) VALUES ('1m', ?, ?)", at,
			fmt.Sprintf(`{"cpu_percent":%d,"max_cpu_percent":%d,"cpu_breakdown_percent":[%d,0],"cpu_cores_percent":[%d,0]}`, cpu, cpu, cpu, cpu))
		require.NoError(t, execErr)
	}
	// One sample two buckets back, then five contiguous minutes; the bucket
	// between them stays empty.
	insert(base-1_200_000, 1)
	for i := range 5 {
		insert(base+int64(i)*60_000, 10+i)
	}
	to := base + 600_000

	rows, err := store.PluginHistory(context.Background(), PluginCPU, resolution1m, 0, to, 2)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, []int64{base + 180_000, base + 240_000}, []int64{rows[0].CapturedAt, rows[1].CapturedAt}, "newest rows, ascending")

	rows, err = store.PluginHistory(context.Background(), PluginCPU, "10m", 0, to, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, base+240_000, rows[0].CapturedAt, "newest bucket only")

	rows, err = store.PluginHistory(context.Background(), PluginCPU, "10m", 0, to, 2)
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, []int64{base - 1_200_000, base + 240_000}, []int64{rows[0].CapturedAt, rows[1].CapturedAt}, "the two newest non-empty buckets, skipping the empty one")
}

func TestHistoryEvery(t *testing.T) {
	tick := time.Minute

	t.Run("converts intervals to tick multiples", func(t *testing.T) {
		every, err := HistoryEvery(map[string]time.Duration{
			PluginContainers: 5 * time.Minute,
			PluginCPU:        time.Minute,
		}, tick)
		require.NoError(t, err)
		assert.Equal(t, map[string]uint64{PluginContainers: 5, PluginCPU: 1}, every)
	})

	t.Run("empty map is fine", func(t *testing.T) {
		every, err := HistoryEvery(nil, tick)
		require.NoError(t, err)
		assert.Empty(t, every)
	})

	t.Run("rejects intervals that are not a whole multiple of the tick", func(t *testing.T) {
		_, err := HistoryEvery(map[string]time.Duration{PluginMem: 90 * time.Second}, tick)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "mem")
		assert.Contains(t, err.Error(), "multiple")
	})

	t.Run("rejects intervals below the tick", func(t *testing.T) {
		_, err := HistoryEvery(map[string]time.Duration{PluginMem: 30 * time.Second}, tick)
		require.Error(t, err)
		_, err = HistoryEvery(map[string]time.Duration{PluginMem: 0}, tick)
		require.Error(t, err)
	})

	t.Run("rejects plugins without collector history", func(t *testing.T) {
		for _, plugin := range []string{PluginSmart, PluginProcesses, PluginPrograms, "nope"} {
			_, err := HistoryEvery(map[string]time.Duration{plugin: tick}, tick)
			require.Error(t, err, plugin)
			assert.Contains(t, err.Error(), plugin)
		}
	})
}

func TestWriteSnapshotHonoursHistoryEvery(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(t.TempDir(), Options{
		HistoryPlugins: []string{PluginCPU, PluginMem},
		HistoryEvery:   map[string]uint64{PluginMem: 3},
	})
	require.NoError(t, err)
	defer store.Close()

	start := time.Now().UTC().Truncate(time.Minute).Add(-time.Hour)
	for i := range 4 {
		capturedAt := start.Add(time.Duration(i) * time.Minute).UnixMilli()
		require.NoError(t, store.WriteSnapshot(capturedAt, storetest.SampleCombinedData(float64(i))))
	}
	end := start.Add(time.Hour).UnixMilli()

	cpu, err := store.PluginHistory(ctx, PluginCPU, resolution1m, 0, end, 10)
	require.NoError(t, err)
	assert.Len(t, cpu, 4, "plugins without an interval write every tick")

	mem, err := store.PluginHistory(ctx, PluginMem, resolution1m, 0, end, 10)
	require.NoError(t, err)
	require.Len(t, mem, 2, "every third tick, starting with the first")
	assert.Equal(t, start.UnixMilli(), mem[0].CapturedAt)
	assert.Equal(t, start.Add(3*time.Minute).UnixMilli(), mem[1].CapturedAt)

	// Reload to every tick: the next snapshot writes mem again.
	store.SetHistoryEvery(nil)
	require.NoError(t, store.WriteSnapshot(start.Add(4*time.Minute).UnixMilli(), storetest.SampleCombinedData(4)))
	mem, err = store.PluginHistory(ctx, PluginMem, resolution1m, 0, end, 10)
	require.NoError(t, err)
	assert.Len(t, mem, 3)
}
