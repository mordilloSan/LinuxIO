// Package store persists collected metric history to a local SQLite database.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/utils"
)

const storeSchemaVersion = 7

const resolution1m = "1m"
const defaultHistoryRetention = 30 * 24 * time.Hour

var queryResolutions = map[string]time.Duration{
	resolution1m: 1 * time.Minute,
	"10m":        10 * time.Minute,
	"20m":        20 * time.Minute,
	"120m":       120 * time.Minute,
	"480m":       480 * time.Minute,
}

type Store struct {
	db               *sql.DB
	path             string
	historyMu        sync.RWMutex
	historyPlugins   map[string]struct{}
	historyEvery     map[string]uint64 // write cadence in collector ticks; absent means every tick
	snapshotCount    uint64            // WriteSnapshot calls so far, one per collector tick
	historyRetention time.Duration
}
type HistoryRecord[T any] struct {
	CapturedAt int64
	Stats      T
}

type SmartDeviceRecord struct {
	ID   string
	Key  string
	Data smart.SmartData
}

type Options struct {
	HistoryPlugins   []string
	HistoryEvery     map[string]uint64 // from HistoryEvery; nil writes every plugin on every tick
	HistoryRetention time.Duration
}

func OpenStore(dataDir string, options ...Options) (*Store, error) {
	dbPath := filepath.Join(dataDir, "metrics.db")
	historyPlugins := DefaultHistoryPluginNames()
	retention := defaultHistoryRetention
	if len(options) > 0 {
		historyPlugins = options[0].HistoryPlugins
		if options[0].HistoryRetention > 0 {
			retention = options[0].HistoryRetention
		}
	}

	store, err := openStoreDB(dbPath, historyPlugins, retention)
	if err == nil {
		if len(options) > 0 {
			store.SetHistoryEvery(options[0].HistoryEvery)
		}
		return store, nil
	}
	if !recoverableStoreOpenError(err) {
		return nil, err
	}

	slog.Warn("Moving aside corrupt metrics.db", "path", dbPath, "err", err)
	if _, moveErr := moveAsideStoreFiles(dbPath, "corrupt"); moveErr != nil {
		return nil, fmt.Errorf("open metrics.db failed: %w; move aside failed: %w", err, moveErr)
	}
	store, err = openStoreDB(dbPath, historyPlugins, retention)
	if err != nil {
		return nil, err
	}
	if len(options) > 0 {
		store.SetHistoryEvery(options[0].HistoryEvery)
	}
	return store, nil
}

func openStoreDB(dbPath string, historyPlugins []string, retention time.Duration) (*Store, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	store := &Store{
		db:               db,
		path:             dbPath,
		historyPlugins:   historyPluginSet(historyPlugins),
		historyRetention: retention,
	}
	if err := store.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func recoverableStoreOpenError(err error) bool {
	serr, ok := errors.AsType[sqlite3.Error](err)
	if !ok {
		return false
	}
	return serr.Code == sqlite3.ErrCorrupt || serr.Code == sqlite3.ErrNotADB
}

func DatabasePath(dataDir string) string {
	return filepath.Join(dataDir, "metrics.db")
}

// DatabaseFile is one on-disk database file and its size in bytes.
type DatabaseFile struct {
	Path string
	Size int64
}

// DatabaseFiles returns the metrics database and its SQLite sidecars that exist
// in dataDir, together with their sizes. Missing files are skipped, so the
// result is empty when the database has not been created yet.
func DatabaseFiles(dataDir string) ([]DatabaseFile, error) {
	dbPath := DatabasePath(dataDir)
	files := make([]DatabaseFile, 0, 3)
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		info, err := os.Stat(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		files = append(files, DatabaseFile{Path: path, Size: info.Size()})
	}
	return files, nil
}

func CheckDatabase(dataDir string) error {
	dbPath := DatabasePath(dataDir)
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("stat metrics.db: %w", err)
	}
	// A WAL database may need recovery just to be read, so the integrity check
	// needs write access even though it changes no rows. Say so plainly instead
	// of surfacing SQLITE_CANTOPEN from the first query.
	if err := checkStoreWritable(dbPath); err != nil {
		return err
	}

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if err := checkDBIntegrity(db); err != nil {
		return err
	}

	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	switch version {
	case storeSchemaVersion:
		return nil
	case 4, 5, 6:
		return fmt.Errorf("obsolete metrics.db schema version %d; start the agent once to migrate current schema", version)
	case 1, 2, 3:
		return fmt.Errorf("obsolete metrics.db schema version %d; run db repair to recreate current schema", version)
	default:
		return fmt.Errorf("unsupported metrics.db schema version %d", version)
	}
}

// checkStoreWritable verifies the current user can open metrics.db and its data
// directory for writing, which SQLite requires for any WAL database.
func checkStoreWritable(dbPath string) error {
	file, err := os.OpenFile(dbPath, os.O_WRONLY, 0)
	if err != nil {
		return fmt.Errorf("metrics.db is not writable by uid %d (integrity check needs write access; try sudo): %w", os.Getuid(), err)
	}
	if err := file.Close(); err != nil {
		return err
	}
	if _, err := directoryIsWritable(filepath.Dir(dbPath)); err != nil {
		return fmt.Errorf("data directory is not writable by uid %d (integrity check needs write access; try sudo): %w", os.Getuid(), err)
	}
	return nil
}

func ResetDatabase(dataDir string, options ...Options) (*Store, []string, error) {
	dbPath := DatabasePath(dataDir)
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, nil, err
	}
	moved, err := moveAsideStoreFiles(dbPath, "reset")
	if err != nil {
		return nil, nil, err
	}
	historyPlugins := DefaultHistoryPluginNames()
	retention := defaultHistoryRetention
	if len(options) > 0 {
		historyPlugins = options[0].HistoryPlugins
		if options[0].HistoryRetention > 0 {
			retention = options[0].HistoryRetention
		}
	}
	store, err := openStoreDB(dbPath, historyPlugins, retention)
	if err != nil {
		return nil, moved, err
	}
	return store, moved, nil
}

func RepairDatabase(dataDir string, options ...Options) (*Store, []string, error) {
	dbPath := DatabasePath(dataDir)
	historyPlugins := DefaultHistoryPluginNames()
	retention := defaultHistoryRetention
	if len(options) > 0 {
		historyPlugins = options[0].HistoryPlugins
		if options[0].HistoryRetention > 0 {
			retention = options[0].HistoryRetention
		}
	}

	store, err := openStoreDB(dbPath, historyPlugins, retention)
	repairErr := err
	if err == nil {
		checkErr := store.IntegrityCheck()
		if checkErr == nil {
			return store, nil, nil
		}
		repairErr = checkErr
		_ = store.Close()
	} else if !recoverableStoreOpenError(err) {
		return nil, nil, err
	}

	moved, moveErr := moveAsideStoreFiles(dbPath, "repair")
	if moveErr != nil {
		return nil, nil, fmt.Errorf("repair metrics.db failed: %w; move aside failed: %w", repairErr, moveErr)
	}
	store, err = openStoreDB(dbPath, historyPlugins, retention)
	if err != nil {
		return nil, moved, err
	}
	return store, moved, nil
}

func moveAsideStoreFiles(dbPath, reason string) ([]string, error) {
	suffix := "." + reason + "-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	moved := make([]string, 0, 3)
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return moved, err
		}
		target := path + suffix
		if err := os.Rename(path, target); err != nil {
			return moved, err
		}
		moved = append(moved, target)
	}
	return moved, nil
}

func ParseHistoryPlugins(raw string, explicit bool) ([]string, error) {
	return parseHistoryPlugins(raw, explicit, utils.GetEnv)
}

func (s *Store) init() error {
	pragmas := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	}
	for _, pragma := range pragmas {
		if _, err := s.db.Exec(pragma); err != nil {
			return err
		}
	}

	var version int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	switch version {
	case 0:
		return createSchema(s.db)
	case 4, 5, 6:
		return s.migrateToV7(version)
	case storeSchemaVersion:
		return createSchema(s.db)
	default:
		return fmt.Errorf("unsupported store schema version %d", version)
	}
}

func (s *Store) migrateToV7(version int) (err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if version < 6 {
		if err = dropLegacySnapshotTables(tx); err != nil {
			return err
		}
	}
	if err = deleteLegacyRollupRows(tx); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}

	// Remove the old payload bytes from both the WAL and the main database
	// before marking the schema upgrade complete.
	for _, statement := range []string{"PRAGMA wal_checkpoint(TRUNCATE)", "VACUUM", "PRAGMA wal_checkpoint(TRUNCATE)"} {
		if _, err = s.db.Exec(statement); err != nil {
			return err
		}
	}
	return createSchema(s.db)
}

func dropLegacySnapshotTables(tx *sql.Tx) error {
	for _, plugin := range pluginNames {
		if _, err := tx.Exec("DROP TABLE IF EXISTS " + plugin + "_current"); err != nil {
			return err
		}
	}
	for _, plugin := range []string{PluginProcesses, PluginPrograms} {
		if _, err := tx.Exec("DROP TABLE IF EXISTS " + pluginHistoryTable(plugin)); err != nil {
			return err
		}
	}
	_, err := tx.Exec("DROP TABLE IF EXISTS meta")
	return err
}

func deleteLegacyRollupRows(tx *sql.Tx) error {
	for _, plugin := range historyCapablePluginNames() {
		table := pluginHistoryTable(plugin)
		var exists int
		if err := tx.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			if _, err := tx.Exec("DELETE FROM "+table+" WHERE resolution <> ?", resolution1m); err != nil {
				return err
			}
		}
	}
	return nil
}

type schemaExecer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

func createSchema(db schemaExecer) error {
	statements := make([]string, 0, len(historyCapablePluginNames())*2+1)
	for _, plugin := range historyCapablePluginNames() {
		statements = append(statements,
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				resolution TEXT NOT NULL,
				captured_at INTEGER NOT NULL,
				stats_json TEXT NOT NULL,
				PRIMARY KEY (resolution, captured_at)
			)`, pluginHistoryTable(plugin)),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_captured_at
				ON %s (captured_at)`, pluginHistoryTable(plugin), pluginHistoryTable(plugin)),
		)
	}
	statements = append(statements, fmt.Sprintf("PRAGMA user_version = %d", storeSchemaVersion))

	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) IntegrityCheck() error {
	return checkDBIntegrity(s.db)
}

func (s *Store) Vacuum() error {
	_, err := s.db.Exec("VACUUM")
	return err
}

func checkDBIntegrity(db *sql.DB) error {
	rows, err := db.Query("PRAGMA quick_check")
	if err != nil {
		return err
	}
	defer rows.Close()

	ok := false
	issues := make([]string, 0)
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return err
		}
		if strings.EqualFold(result, "ok") {
			ok = true
			continue
		}
		issues = append(issues, result)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(issues) > 0 {
		return fmt.Errorf("database quick_check failed: %s", strings.Join(issues, "; "))
	}
	if !ok {
		return errors.New("database quick_check returned no result")
	}
	return nil
}

func (s *Store) WriteSnapshot(capturedAt int64, data *system.CombinedData) (err error) {
	if data == nil {
		return errors.New("snapshot data is nil")
	}
	historyPlugins, dueFilter := s.beginSnapshotTick()
	if len(historyPlugins) == 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	payloads := SnapshotPluginPayloads(data)
	for _, plugin := range historyPlugins {
		if plugin == PluginSmart || !dueFilter(plugin) {
			continue
		}
		payload, ok := payloads[plugin]
		if !ok {
			continue
		}
		raw, marshalErr := marshalJSON(payload)
		if marshalErr != nil {
			return marshalErr
		}
		if err = insertPluginHistory(tx, plugin, resolution1m, capturedAt, raw); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func insertPluginHistory(tx *sql.Tx, plugin, resolution string, capturedAt int64, raw string) error {
	_, err := tx.Exec(fmt.Sprintf(`
		INSERT OR REPLACE INTO %s (resolution, captured_at, stats_json)
		VALUES (?, ?, ?)
	`, pluginHistoryTable(plugin)), resolution, capturedAt, raw)
	return err
}

func (s *Store) WriteSmartDevices(capturedAt int64, items map[string]smart.SmartData) error {
	if !s.HistoryEnabled(PluginSmart) {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	currentItems := make([]SmartDeviceRecord, 0, len(items))
	for key, item := range items {
		id := key
		if item.DiskName != "" {
			id = item.DiskName
		}
		currentItems = append(currentItems, SmartDeviceRecord{
			ID:   id,
			Key:  key,
			Data: item,
		})
	}

	raw, err := marshalJSON(currentItems)
	if err != nil {
		return err
	}
	if err = insertPluginHistory(tx, PluginSmart, resolution1m, capturedAt, raw); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *Store) PluginHistory(ctx context.Context, plugin, resolution string, from, to int64, limit int) ([]HistoryRecord[json.RawMessage], error) {
	if !IsPluginName(plugin) {
		return nil, fmt.Errorf("unknown plugin %q", plugin)
	}
	if !s.HistoryEnabled(plugin) {
		return nil, sql.ErrNoRows
	}
	interval, ok := queryResolutions[resolution]
	if !ok {
		return nil, fmt.Errorf("invalid resolution %q", resolution)
	}
	items := []HistoryRecord[json.RawMessage]{}
	queryResolution := resolution1m
	table := pluginHistoryTable(plugin)
	// Only the newest rows can survive the limit, so bound the JSON scan to
	// them: LIMIT at the stored resolution, or the newest `limit` non-empty
	// buckets when downsampling. A small request never decodes the whole
	// retention window.
	order := "ORDER BY captured_at ASC"
	args := []any{queryResolution, from, to}
	if interval == time.Minute {
		order = "ORDER BY captured_at DESC LIMIT ?"
		args = append(args, limit)
	} else if step := interval.Milliseconds(); step > 0 && limit > 0 {
		var lower sql.NullInt64
		err := s.db.QueryRowContext(ctx, fmt.Sprintf(`
			SELECT MIN(bucket) FROM (
				SELECT (captured_at / ?) * ? AS bucket
				FROM %s
				WHERE resolution = ? AND captured_at >= ? AND captured_at <= ?
				GROUP BY bucket
				ORDER BY bucket DESC
				LIMIT ?
			)
		`, table), step, step, queryResolution, from, to, limit).Scan(&lower)
		if err != nil {
			return nil, err
		}
		if lower.Valid && lower.Int64 > from {
			args[1] = lower.Int64
		}
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT captured_at, stats_json
		FROM %s
		WHERE resolution = ? AND captured_at >= ? AND captured_at <= ?
		%s
	`, table, order), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			capturedAt int64
			raw        string
		)
		if err := rows.Scan(&capturedAt, &raw); err != nil {
			return nil, err
		}
		items = append(items, HistoryRecord[json.RawMessage]{
			CapturedAt: capturedAt,
			Stats:      json.RawMessage(raw),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if interval == time.Minute {
		slices.Reverse(items) // the LIMIT scan ran newest-first
		return items, nil
	}
	return downsampleHistory(ctx, plugin, items, interval, limit)
}

func downsampleHistory(ctx context.Context, plugin string, items []HistoryRecord[json.RawMessage], interval time.Duration, limit int) ([]HistoryRecord[json.RawMessage], error) {
	if len(items) == 0 {
		return items, nil
	}
	step := interval.Milliseconds()
	out := make([]HistoryRecord[json.RawMessage], 0)
	bucketAt := (items[0].CapturedAt / step) * step
	bucketLatest := items[0].CapturedAt
	bucketItems := make([]string, 0)
	flush := func() error {
		raw, err := aggregatePluginHistoryJSON(plugin, bucketItems)
		if err != nil {
			return err
		}
		out = append(out, HistoryRecord[json.RawMessage]{CapturedAt: bucketLatest, Stats: json.RawMessage(raw)})
		return nil
	}
	for _, item := range items {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		at := (item.CapturedAt / step) * step
		if at != bucketAt {
			if err := flush(); err != nil {
				return nil, err
			}
			bucketAt = at
			bucketItems = bucketItems[:0]
		}
		bucketLatest = item.CapturedAt
		bucketItems = append(bucketItems, string(item.Stats))
	}
	if err := flush(); err != nil {
		return nil, err
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

func (s *Store) HistoryEnabled(plugin string) bool {
	s.historyMu.RLock()
	defer s.historyMu.RUnlock()
	_, ok := s.historyPlugins[plugin]
	return ok
}

func (s *Store) historyPluginNames() []string {
	s.historyMu.RLock()
	defer s.historyMu.RUnlock()
	out := make([]string, 0, len(s.historyPlugins))
	for _, plugin := range historyCapablePluginNames() {
		if _, ok := s.historyPlugins[plugin]; ok {
			out = append(out, plugin)
		}
	}
	return out
}

func (s *Store) SetHistoryPlugins(plugins []string) {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	s.historyPlugins = historyPluginSet(plugins)
}

// SetHistoryEvery replaces the per-plugin write cadence, see HistoryEvery.
func (s *Store) SetHistoryEvery(every map[string]uint64) {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	s.historyEvery = maps.Clone(every)
}

// beginSnapshotTick counts one collector tick and reports the enabled history
// plugins together with a filter for the ones due on this tick. A plugin with
// cadence N writes on every Nth tick, starting with the first.
// ponytail: the stored value is still the average over one collector tick, not
// over the plugin's own interval; a per-interval delta key would fix that.
func (s *Store) beginSnapshotTick() ([]string, func(plugin string) bool) {
	s.historyMu.Lock()
	tick := s.snapshotCount
	s.snapshotCount++
	every := s.historyEvery
	s.historyMu.Unlock()
	return s.historyPluginNames(), func(plugin string) bool {
		n := every[plugin]
		return n <= 1 || tick%n == 0
	}
}

func marshalJSON(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func ValidResolution(resolution string) bool {
	_, ok := queryResolutions[resolution]
	return ok
}

// RetentionStrings returns history retention windows keyed by resolution,
// with each duration formatted via time.Duration.String.
func (s *Store) RetentionStrings() map[string]string {
	s.historyMu.RLock()
	retention := s.historyRetention
	s.historyMu.RUnlock()
	return map[string]string{resolution1m: retention.String()}
}

func RetentionStrings() map[string]string {
	return map[string]string{resolution1m: defaultHistoryRetention.String()}
}

func DefaultHistoryRetention() time.Duration { return defaultHistoryRetention }

func (s *Store) SetHistoryRetention(retention time.Duration) {
	if retention > 0 {
		s.historyMu.Lock()
		s.historyRetention = retention
		s.historyMu.Unlock()
	}
}

func (s *Store) historyRetentionDuration() time.Duration {
	s.historyMu.RLock()
	defer s.historyMu.RUnlock()
	return s.historyRetention
}
