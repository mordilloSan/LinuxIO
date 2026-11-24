// Package store persists collected metric history to a local SQLite database.
package store

import (
	"context"
	"database/sql"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	sqlite3 "github.com/mattn/go-sqlite3"

	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
	smartmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
	systemmodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/plugins"
)

// storeSchemaVersion is the layout of metrics.db. A database of any other
// version is deleted at start and the history starts again; see OpenStore.
const storeSchemaVersion = 8

const resolution1m = "1m"

// tier is one stored history resolution. The collector writes 1m rows; every
// coarser tier is rolled up from the tier before it when one of its buckets
// closes.
type tier struct {
	resolution string
	step       time.Duration
	keep       time.Duration // how long its rows are kept; 0 means history.retention
}

// tiers lists the stored resolutions, finest first. Each step is a whole
// multiple of the one before, so every bucket holds whole buckets of the tier
// below. Their names are the resolutions of the wire contract.
var tiers = []tier{
	{resolution: resolution1m, step: time.Minute, keep: 24 * time.Hour},
	{resolution: "10m", step: 10 * time.Minute, keep: 12 * time.Hour},
	{resolution: "20m", step: 20 * time.Minute, keep: 24 * time.Hour},
	{resolution: "60m", step: time.Hour},
}

// keepFor is how long the tier keeps its rows: its own length, never longer
// than history.retention, which is the whole length of the hourly tier.
func (t tier) keepFor(retention time.Duration) time.Duration {
	if t.keep == 0 || t.keep > retention {
		return retention
	}
	return t.keep
}

type Store struct {
	db               *sql.DB
	path             string
	historyMu        sync.RWMutex
	historyPlugins   map[string]struct{}
	historyEvery     map[string]uint64 // write cadence in collector ticks; absent means every tick
	tickCount        uint64            // BeginTick calls so far, one per collector tick
	historyRetention time.Duration

	rollupMu    sync.Mutex
	rolledUntil []int64 // per tier, the end of the last bucket Rollup closed, in Unix ms
}

// Tick is the history plan of one collector tick: the plugins whose 1m row it
// writes. SMART is never in it; its rows follow the SMART refresh.
type Tick struct {
	due map[string]struct{}
}

// Due reports whether the plugin's row is written on this tick.
func (t Tick) Due(plugin string) bool {
	_, ok := t.due[plugin]
	return ok
}

// Skip takes the plugin out of this tick, and every copy of it, so its row is
// not written; for a collection that failed.
func (t Tick) Skip(plugin string) {
	delete(t.due, plugin)
}

type HistoryRecord[T any] struct {
	CapturedAt int64
	Stats      T
}

type SmartDeviceRecord struct {
	ID   string
	Key  string
	Data smartmodel.SmartData
}

type Options struct {
	HistoryPlugins   []string
	HistoryEvery     map[string]uint64 // from plugins.HistoryEvery; nil writes every plugin on every tick
	HistoryRetention time.Duration
}

// OpenStore opens metrics.db in dataDir, creating it when absent. A database
// of another schema version, older or newer, is deleted with its sidecars and
// created again empty: its history is gone, and a store this daemon cannot read
// no longer stops it from starting. A corrupt database is moved aside, and only
// the newest such copy is kept.
func OpenStore(ctx context.Context, dataDir string, options ...Options) (*Store, error) {
	dbPath := filepath.Join(dataDir, "metrics.db")
	historyPlugins := plugins.DefaultHistory()
	retention := config.DefaultHistoryRetention
	if len(options) > 0 {
		historyPlugins = options[0].HistoryPlugins
		if options[0].HistoryRetention > 0 {
			retention = options[0].HistoryRetention
		}
	}

	store, err := openStoreDB(ctx, dbPath, historyPlugins, retention)
	if versionErr, ok := errors.AsType[*schemaVersionError](err); ok {
		if removeErr := removeStoreFiles(dbPath); removeErr != nil {
			return nil, fmt.Errorf("%w; delete failed: %w", err, removeErr)
		}
		slog.Info("deleted history store of another version", "path", dbPath, "version", versionErr.version, "expected", storeSchemaVersion)
		store, err = openStoreDB(ctx, dbPath, historyPlugins, retention)
	} else if err != nil && recoverableStoreOpenError(err) {
		slog.Warn("moving aside corrupt metrics.db", "path", dbPath, "error", err)
		if _, moveErr := moveAsideStoreFiles(dbPath, "corrupt"); moveErr != nil {
			return nil, fmt.Errorf("open metrics.db failed: %w; move aside failed: %w", err, moveErr)
		}
		if pruneErr := keepNewestCorruptCopy(dbPath); pruneErr != nil {
			slog.Warn("could not delete older copies of a corrupt metrics.db", "path", dbPath, "error", pruneErr)
		}
		store, err = openStoreDB(ctx, dbPath, historyPlugins, retention)
	}
	if err != nil {
		return nil, err
	}
	if len(options) > 0 {
		store.SetHistoryEvery(options[0].HistoryEvery)
	}
	return store, nil
}

func openStoreDB(ctx context.Context, dbPath string, historyPlugins []string, retention time.Duration) (*Store, error) {
	// The pragmas live in the DSN so every connection the pool opens gets them.
	// The path is URI-escaped (keeping '/' literal) so '?', '#' or '%' in the
	// directory name are not misparsed as DSN syntax.
	escapedPath := strings.ReplaceAll(url.PathEscape(dbPath), "%2F", "/")
	dsn := "file:" + escapedPath + "?_journal_mode=WAL&_synchronous=NORMAL&_foreign_keys=on&_busy_timeout=5000"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	store := &Store{
		db:               db,
		path:             dbPath,
		historyPlugins:   historyPluginSet(historyPlugins),
		historyRetention: retention,
		rolledUntil:      make([]int64, len(tiers)),
	}
	if err := store.init(ctx); err != nil {
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

func moveAsideStoreFiles(dbPath, reason string) ([]string, error) {
	suffix := "." + reason + "-" + time.Now().UTC().Format(movedAsideStamp)
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

// schemaVersionError is a database whose layout this daemon does not write.
type schemaVersionError struct {
	version int
}

func (e *schemaVersionError) Error() string {
	return fmt.Sprintf("store schema version %d, expected %d", e.version, storeSchemaVersion)
}

// removeStoreFiles deletes the database and its sidecars; a missing file is not
// an error.
func removeStoreFiles(dbPath string) error {
	var errs error
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = errors.Join(errs, err)
		}
	}
	return errs
}

// movedAsideStamp is the UTC time a moved-aside database's name ends with.
const movedAsideStamp = "20060102T150405.000000000Z"

// keepNewestCorruptCopy deletes every copy of a corrupt database, sidecars
// included, that was moved aside before the newest one. Only the names
// moveAsideStoreFiles gives count, so a copy an administrator named is kept.
func keepNewestCorruptCopy(dbPath string) error {
	dir, base := filepath.Split(dbPath)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	stamps := make(map[string][]string)
	newest := ""
	for _, entry := range entries {
		name := entry.Name()
		file, stamp, ok := strings.Cut(name, ".corrupt-")
		if !ok || (file != base && file != base+"-wal" && file != base+"-shm") {
			continue
		}
		if _, err := time.Parse(movedAsideStamp, stamp); err != nil {
			continue
		}
		stamps[stamp] = append(stamps[stamp], filepath.Join(dir, name))
		newest = max(newest, stamp) // the stamps are UTC times that sort as text
	}
	var errs error
	for stamp, paths := range stamps {
		if stamp == newest {
			continue
		}
		for _, path := range paths {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = errors.Join(errs, err)
			}
		}
	}
	return errs
}

func (s *Store) init(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	switch version {
	case 0, storeSchemaVersion:
		return createSchema(ctx, s.db)
	default:
		return &schemaVersionError{version: version}
	}
}

// rollbackTx is deferred right after Begin; it is a no-op once Commit succeeded.
func rollbackTx(tx *sql.Tx, operation string) {
	if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
		slog.Warn("rollback failed", "operation", operation, "error", err)
	}
}

type schemaExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// createSchema creates one table per history-capable plugin. The primary key
// serves every query: reads, rollups and retention all name a resolution and
// a range of times.
func createSchema(ctx context.Context, db schemaExecer) error {
	statements := make([]string, 0, len(plugins.HistoryCapable())+1)
	for _, plugin := range plugins.HistoryCapable() {
		statements = append(statements,
			fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
				resolution TEXT NOT NULL,
				captured_at INTEGER NOT NULL,
				stats_json TEXT NOT NULL,
				PRIMARY KEY (resolution, captured_at)
			)`, pluginHistoryTable(plugin)),
		)
	}
	statements = append(statements, fmt.Sprintf("PRAGMA user_version = %d", storeSchemaVersion))

	for _, stmt := range statements {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
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

func (s *Store) IntegrityCheck(ctx context.Context) error {
	return checkDBIntegrity(ctx, s.db)
}

func checkDBIntegrity(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, "PRAGMA quick_check")
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

// BeginTick counts one collector tick and returns the plugins whose 1m row is
// due on it. A plugin with cadence N is due on every Nth tick, starting with
// the first. The collector asks before it collects, so a tick collects only
// what it stores.
// ponytail: the stored value is still the average over one collector tick, not
// over the plugin's own interval; a per-interval delta key would fix that.
func (s *Store) BeginTick() Tick {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	tick := s.tickCount
	s.tickCount++
	due := make(map[string]struct{}, len(s.historyPlugins))
	for plugin := range s.historyPlugins {
		if plugin == plugins.Smart {
			continue
		}
		if n := s.historyEvery[plugin]; n > 1 && tick%n != 0 {
			continue
		}
		due[plugin] = struct{}{}
	}
	return Tick{due: due}
}

// WriteSnapshot writes the 1m row of every plugin due on the tick.
func (s *Store) WriteSnapshot(ctx context.Context, capturedAt int64, data *systemmodel.CombinedData, tick Tick) error {
	if data == nil {
		return errors.New("snapshot data is nil")
	}
	if len(tick.due) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackTx(tx, "write snapshot")

	payloads := plugins.Snapshot(data)
	for _, plugin := range plugins.HistoryCapable() {
		if !tick.Due(plugin) {
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
		if err = insertPluginHistory(ctx, tx, plugin, resolution1m, capturedAt, raw); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func insertPluginHistory(ctx context.Context, tx *sql.Tx, plugin, resolution string, capturedAt int64, raw string) error {
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT OR REPLACE INTO %s (resolution, captured_at, stats_json)
		VALUES (?, ?, ?)
	`, pluginHistoryTable(plugin)), resolution, capturedAt, raw)
	return err
}

// Rollup writes a row of each coarser tier for every bucket of it that has
// closed by now, averaging the rows the tier below holds for that bucket. A
// row's time is the time of the newest row it averages. A bucket is rolled up
// once: the next one considered starts after the newest row its tier holds,
// so a restart neither repeats a bucket nor skips one whose rows are still
// kept. Only a tick that crosses a bucket boundary reads the database.
func (s *Store) Rollup(ctx context.Context, now time.Time) error {
	s.rollupMu.Lock()
	defer s.rollupMu.Unlock()

	nowMs := now.UnixMilli()
	closedEnd := make([]int64, len(tiers))
	pending := false
	for i := 1; i < len(tiers); i++ {
		step := tiers[i].step.Milliseconds()
		closedEnd[i] = nowMs / step * step
		if s.rolledUntil[i] < closedEnd[i] {
			pending = true
		}
	}
	if !pending {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackTx(tx, "roll up history")
	for _, plugin := range s.historyPluginNames() {
		// Finest first, so a coarser bucket sees the row just written below it.
		for i := 1; i < len(tiers); i++ {
			if s.rolledUntil[i] >= closedEnd[i] {
				continue
			}
			if err := rollupTier(ctx, tx, plugin, tiers[i-1], tiers[i], closedEnd[i]); err != nil {
				return fmt.Errorf("roll up %s %s: %w", plugin, tiers[i].resolution, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for i := 1; i < len(tiers); i++ {
		s.rolledUntil[i] = max(s.rolledUntil[i], closedEnd[i])
	}
	return nil
}

// rollupTier writes the dst rows of the closed buckets before closedEnd that
// it does not hold yet, from the src rows in those buckets.
func rollupTier(ctx context.Context, tx *sql.Tx, plugin string, src, dst tier, closedEnd int64) error {
	table := pluginHistoryTable(plugin)
	step := dst.step.Milliseconds()

	var newest sql.NullInt64
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT MAX(captured_at) FROM %s WHERE resolution = ?`, table), dst.resolution).Scan(&newest); err != nil {
		return err
	}
	from := int64(0)
	if newest.Valid {
		from = (newest.Int64/step + 1) * step
	}
	if from >= closedEnd {
		return nil
	}

	// Read the rows before writing: the source tier keeps a bounded number.
	// Once the tier has a row this is the bucket that just closed; the first
	// rollup into an empty tier reads all the source keeps, for 1m a day of
	// rows (1,440 at the default interval, 5,760 at 15 s), once.
	source, err := readTierRows(ctx, tx, table, src.resolution, from, closedEnd)
	if err != nil {
		return err
	}
	for start := 0; start < len(source); {
		bucket := source[start].at / step
		end := start
		bucketRows := make([]string, 0, 16)
		for end < len(source) && source[end].at/step == bucket {
			bucketRows = append(bucketRows, source[end].raw)
			end++
		}
		averaged, err := plugins.Average(plugin, bucketRows)
		if err != nil {
			// A row that does not decode costs its own bucket, not every
			// rollup after it.
			slog.Warn("history bucket left out of its rollup", "plugin", plugin, "resolution", dst.resolution, "captured_at", source[end-1].at, "error", err)
			start = end
			continue
		}
		if err := insertPluginHistory(ctx, tx, plugin, dst.resolution, source[end-1].at, averaged); err != nil {
			return err
		}
		start = end
	}
	return nil
}

func (s *Store) WriteSmartDevices(ctx context.Context, capturedAt int64, items map[string]smartmodel.SmartData) error {
	if !s.HistoryEnabled(plugins.Smart) {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackTx(tx, "write smart devices")

	currentItems := make([]SmartDeviceRecord, 0, len(items))
	for key, item := range items {
		id := key
		if item.DiskName != "" {
			id = item.DiskName
		}
		currentItems = append(currentItems, SmartDeviceRecord{
			ID:   id,
			Key:  key,
			Data: compactSmartData(item),
		})
	}

	raw, err := marshalJSON(currentItems)
	if err != nil {
		return err
	}
	if err = insertPluginHistory(ctx, tx, plugins.Smart, resolution1m, capturedAt, raw); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	return nil
}

func compactSmartData(item smartmodel.SmartData) smartmodel.SmartData {
	return smartmodel.SmartData{
		ModelName:       item.ModelName,
		SerialNumber:    item.SerialNumber,
		FirmwareVersion: item.FirmwareVersion,
		Capacity:        item.Capacity,
		SmartStatus:     item.SmartStatus,
		DiskName:        item.DiskName,
		DiskType:        item.DiskType,
		Temperature:     item.Temperature,
		Attributes:      item.Attributes,
	}
}

type storedRow struct {
	at  int64
	raw string
}

// readTierRows reads the rows of one tier in [from, to), oldest first.
func readTierRows(ctx context.Context, tx *sql.Tx, table, resolution string, from, to int64) ([]storedRow, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT captured_at, stats_json FROM %s
		WHERE resolution = ? AND captured_at >= ? AND captured_at < ?
		ORDER BY captured_at
	`, table), resolution, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []storedRow
	for rows.Next() {
		var r storedRow
		if err := rows.Scan(&r.at, &r.raw); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PluginHistory returns the newest limit rows of one tier between from and to,
// oldest first. A coarse tier holds rows only for closed buckets, so its
// newest row can be up to one step old.
func (s *Store) PluginHistory(ctx context.Context, plugin, resolution string, from, to int64, limit int) ([]HistoryRecord[jsontext.Value], error) {
	if !plugins.IsName(plugin) {
		return nil, fmt.Errorf("unknown plugin %q", plugin)
	}
	if !s.HistoryEnabled(plugin) {
		return nil, sql.ErrNoRows
	}
	if !ValidResolution(resolution) {
		return nil, fmt.Errorf("invalid resolution %q", resolution)
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
		SELECT captured_at, stats_json
		FROM %s
		WHERE resolution = ? AND captured_at >= ? AND captured_at <= ?
		ORDER BY captured_at DESC
		LIMIT ?
	`, pluginHistoryTable(plugin)), resolution, from, to, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := []HistoryRecord[jsontext.Value]{}
	for rows.Next() {
		var (
			capturedAt int64
			raw        string
		)
		if err := rows.Scan(&capturedAt, &raw); err != nil {
			return nil, err
		}
		items = append(items, HistoryRecord[jsontext.Value]{
			CapturedAt: capturedAt,
			Stats:      jsontext.Value(raw),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.Reverse(items)
	return items, nil
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
	for _, plugin := range plugins.HistoryCapable() {
		if _, ok := s.historyPlugins[plugin]; ok {
			out = append(out, plugin)
		}
	}
	return out
}

func (s *Store) SetHistoryPlugins(names []string) {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	s.historyPlugins = historyPluginSet(names)
}

// SetHistoryEvery replaces the per-plugin write cadence, see plugins.HistoryEvery.
func (s *Store) SetHistoryEvery(every map[string]uint64) {
	s.historyMu.Lock()
	defer s.historyMu.Unlock()
	s.historyEvery = maps.Clone(every)
}

func marshalJSON(v any) (string, error) {
	raw, err := json.Marshal(v, json.Deterministic(true), jsontext.AllowInvalidUTF8(true))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// ValidResolution reports whether resolution names a stored tier.
func ValidResolution(resolution string) bool {
	return slices.ContainsFunc(tiers, func(t tier) bool { return t.resolution == resolution })
}

// RetentionStrings returns how long each tier keeps its rows, keyed by
// resolution, with each duration formatted via time.Duration.String.
func (s *Store) RetentionStrings() map[string]string {
	return retentionStrings(s.historyRetentionDuration())
}

// RetentionStrings is the default retention of each tier.
func RetentionStrings() map[string]string {
	return retentionStrings(config.DefaultHistoryRetention)
}

func retentionStrings(retention time.Duration) map[string]string {
	out := make(map[string]string, len(tiers))
	for _, t := range tiers {
		out[t.resolution] = t.keepFor(retention).String()
	}
	return out
}

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

func pluginHistoryTable(plugin string) string {
	return plugin + "_history"
}

func historyPluginSet(names []string) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for _, plugin := range names {
		if plugins.IsLiveOnly(plugin) {
			continue
		}
		out[plugin] = struct{}{}
	}
	return out
}
