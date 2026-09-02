package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/socketactivation"
	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/storage"
)

// DaemonConfig controls the long-running server.
type DaemonConfig struct {
	ExcludePaths         []string
	IncludeNetworkMounts bool
	ConfigPath           string
}

type daemon struct {
	cfg            DaemonConfig
	cfgMu          sync.RWMutex
	savedConfig    configfile.Config
	db             *sql.DB
	store          *storage.Store
	servers        []*http.Server
	running        atomic.Bool
	workStreamMu   sync.RWMutex
	workStream     *workStreamBroadcaster
	activeRequests atomic.Int64
	activityMu     sync.RWMutex
	lastActivity   time.Time
	bgCancel       context.CancelFunc
	bgWG           sync.WaitGroup
}

// goBackground runs fn on a tracked goroutine so Close can wait for it before
// tearing down the database. Detached index work must go through this, or
// shutdown races it against db.Close.
func (d *daemon) goBackground(fn func()) {
	d.bgWG.Go(func() {
		fn()
	})
}

// waitBackground waits for every daemon-owned operation before closing SQLite.
func (d *daemon) waitBackground() {
	d.bgWG.Wait()
}

const idleGrace = 90 * time.Second

const runtimeDirectoryMarker = "/run/linuxio/webserver"

const idleCheckInterval = 15 * time.Second

func runtimeDirectoryExists() bool {
	info, err := os.Stat(runtimeDirectoryMarker)
	return err == nil && info.IsDir()
}

func DaemonConfigFromConfig(cfg configfile.Config, configPath string) (DaemonConfig, error) {
	cfg, err := configfile.Normalize(cfg)
	if err != nil {
		return DaemonConfig{}, err
	}
	var daemonCfg DaemonConfig
	if err := applyFileConfigFields(&daemonCfg, cfg); err != nil {
		return DaemonConfig{}, err
	}
	daemonCfg.ConfigPath = configPath
	return daemonCfg, nil
}

func applyFileConfigFields(dst *DaemonConfig, cfg configfile.Config) error {
	dst.ExcludePaths = append([]string(nil), cfg.ExcludePaths...)
	dst.IncludeNetworkMounts = cfg.IncludeNetworkMounts
	return nil
}

func NewDaemon(cfg DaemonConfig) (*daemon, error) {
	if cfg.ConfigPath == "" {
		cfg.ConfigPath = configfile.DefaultPath()
	}

	dbPath := configfile.DefaultDBPath
	db, err := openDaemonDatabase(dbPath, storage.DefaultOpenOptions())
	if err != nil {
		return nil, err
	}

	slog.Info("DB connection pool opened")
	journalCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	journalMode, err := storage.GetJournalMode(journalCtx, db)
	if err != nil {
		slog.Warn("failed to determine database journal_mode", "err", err)
	} else {
		slog.Info("database journal_mode", "mode", strings.ToUpper(journalMode))
	}
	logLatestIndexStatus(db)

	savedConfig, err := daemonConfigToFileConfig(cfg)
	if err != nil {
		if closeErr := db.Close(); closeErr != nil {
			slog.Warn("database close error after config normalization failure", "err", closeErr)
		}
		return nil, err
	}

	return &daemon{
		cfg:         cfg,
		savedConfig: savedConfig,
		db:          db,
		store:       storage.NewStoreWithDB(db, dbPath),
	}, nil
}

// openDaemonDatabase replaces the rebuildable cache only when SQLite confirms
// corruption or storage reports an incompatible schema. Operational failures
// (busy, timeout, and I/O errors) preserve the last good cache.
func openDaemonDatabase(path string, opts storage.OpenOptions) (*sql.DB, error) {
	db, err := storage.Open(path, opts)
	if err == nil {
		return db, err
	}
	var reason string
	switch {
	case storage.IsCorruptionError(err):
		reason = "corrupt"
	case errors.Is(err, storage.ErrIncompatibleSchema):
		reason = "incompatible"
	default:
		return db, err
	}
	if db != nil {
		_ = db.Close()
	}
	if err := quarantineDatabaseFiles(path, reason); err != nil {
		return nil, fmt.Errorf("quarantine %s database: %w", reason, err)
	}
	return storage.Open(path, opts)
}

func (d *daemon) Close() {
	slog.Info("shutting down daemon")

	// Signal in-flight background work to cancel before tearing down servers/db.
	if d.bgCancel != nil {
		d.bgCancel()
	}

	d.shutdownHTTPServers()

	// Wait for cancelled maintenance work to unwind before closing the DB it
	// writes to; closing under a running reindex turns cancellation into
	// spurious write errors.
	d.waitBackground()

	// Close database connection
	if d.db != nil {
		if err := d.db.Close(); err != nil {
			slog.Warn("database close error", "err", err)
		}
	}

	slog.Info("daemon shutdown complete")
}

func (d *daemon) shutdownHTTPServers() {
	for _, srv := range d.servers {
		if err := srv.Shutdown(context.Background()); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("server shutdown error", "err", err)
		}
	}
}

func loadActivatedListener() (net.Listener, error) {
	listeners, err := socketactivation.Listeners()
	if err != nil {
		return nil, fmt.Errorf("load socket activation listeners: %w", err)
	}
	if len(listeners) != 1 || listeners[0].Addr().Network() != "unix" {
		socketactivation.CloseListeners(listeners)
		return nil, fmt.Errorf("systemd socket activation requires exactly one Unix listener")
	}
	return listeners[0], nil
}

// Run starts the HTTP server and blocks until context is cancelled.
func (d *daemon) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.TODO()
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if d.bgCancel != nil {
		d.bgCancel()
	}
	d.bgCancel = cancel

	d.markActivity()
	d.goBackground(func() { d.stopWhenIdle(runCtx, cancel, runtimeDirectoryExists, idleCheckInterval) })

	return d.startHTTP(runCtx)
}

func (d *daemon) startHTTP(ctx context.Context) error {
	listener, err := loadActivatedListener()
	if err != nil {
		return err
	}
	return d.serveHTTP(ctx, listener)
}

func (d *daemon) serveHTTP(ctx context.Context, listener net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc(api.RouteIndex, func(w http.ResponseWriter, r *http.Request) {
		d.handleIndexContext(ctx, w, r)
	})
	mux.HandleFunc(api.RouteReindex, func(w http.ResponseWriter, r *http.Request) {
		d.handleReindexContext(ctx, w, r)
	})
	mux.HandleFunc(api.RouteStatus, d.handleStatus)
	mux.HandleFunc(api.RouteSearch, d.handleSearch)
	mux.HandleFunc(api.RouteDirSize, d.handleDirSize)
	mux.HandleFunc(api.RouteSubfolders, d.handleSubfolders)
	mux.HandleFunc(api.RouteAdd, d.handleAdd)
	mux.HandleFunc(api.RouteDelete, d.handleDelete)
	mux.HandleFunc(api.RouteConfig, d.handleConfig)

	handler := d.activityMiddleware(loggerMiddleware(recoveryMiddleware(authorizeTransportMiddleware(mux))))

	errCh := make(chan error, 1)
	srv := newHTTPServer(handler)
	srv.ConnContext = unixConnContext
	srv.BaseContext = func(net.Listener) context.Context { return ctx }
	d.servers = append(d.servers, srv)
	slog.Info("API listening", "addr", "unix://"+listener.Addr().String(), "systemd_socket_activation", true)
	go func() { errCh <- srv.Serve(listener) }()

	select {
	case <-ctx.Done():
		d.shutdownHTTPServers()
		return nil
	case err := <-errCh:
		d.shutdownHTTPServers()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func newHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ErrorLog:          slog.NewLogLogger(slog.Default().Handler(), slog.LevelWarn),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// SSE responses can legitimately remain open for an entire index run.
		WriteTimeout:   0,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
}

func (d *daemon) markActivity() {
	d.activityMu.Lock()
	d.lastActivity = time.Now()
	d.activityMu.Unlock()
}

func (d *daemon) activityTime() time.Time {
	d.activityMu.RLock()
	last := d.lastActivity
	d.activityMu.RUnlock()
	return last
}

func (d *daemon) activityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.activeRequests.Add(1)
		d.markActivity()
		defer func() {
			d.markActivity()
			d.activeRequests.Add(-1)
		}()
		next.ServeHTTP(w, r)
	})
}

func (d *daemon) stopWhenIdle(ctx context.Context, cancel context.CancelFunc, markerExists func() bool, checkInterval time.Duration) {
	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	markerHeld := false

	for {
		select {
		case <-ticker.C:
			if markerExists() {
				d.markActivity()
				markerHeld = true
				continue
			}
			if markerHeld {
				d.markActivity()
				markerHeld = false
				continue
			}
			if d.activeRequests.Load() > 0 || d.running.Load() {
				d.markActivity()
				continue
			}
			last := d.activityTime()
			if last.IsZero() {
				continue
			}
			idleFor := time.Since(last)
			if idleFor >= idleGrace {
				slog.Info("idle timeout reached; shutting down daemon", "idle_for", idleFor.Truncate(time.Second), "idle_timeout", idleGrace)
				cancel()
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func (d *daemon) tryLockIndex() bool {
	return d.running.CompareAndSwap(false, true)
}

func (d *daemon) unlockIndex() {
	d.markActivity()
	d.running.Store(false)
}

func (d *daemon) setWorkStreamBroadcaster(b *workStreamBroadcaster) {
	d.workStreamMu.Lock()
	d.workStream = b
	d.workStreamMu.Unlock()
}

func (d *daemon) getWorkStreamBroadcaster() *workStreamBroadcaster {
	d.workStreamMu.RLock()
	b := d.workStream
	d.workStreamMu.RUnlock()
	return b
}

func (d *daemon) clearWorkStreamBroadcaster(b *workStreamBroadcaster) {
	d.workStreamMu.Lock()
	if d.workStream == b {
		d.workStream = nil
	}
	d.workStreamMu.Unlock()
}

var indexerSpawnOverride func(*daemon, context.Context, func(indexWireEvent)) (*IndexRunStats, error)

// runIndexSubprocess spawns the current binary with --index-mode flag.
// Wire events decoded from the subprocess stdout stream to onEvent (may be
// nil); the returned stats come from the subprocess's summary event, nil when
// it never emitted one.
func (d *daemon) runIndexSubprocess(ctx context.Context, onEvent func(indexWireEvent)) (*IndexRunStats, error) {
	if indexerSpawnOverride != nil {
		return indexerSpawnOverride(d, ctx, onEvent)
	}
	cfg := d.configSnapshot()
	slog.Info("starting index process")
	stats, err := runIndexProcess(exec.CommandContext(ctx, os.Args[0], "--index-mode", "--config-file", cfg.ConfigPath), onEvent)
	if err != nil {
		return nil, err
	}

	slog.Info("index process completed successfully")
	return stats, nil
}

// IndexRunStats summarizes a completed index run and its API progress events.
type IndexRunStats struct {
	Files          int64
	Dirs           int64
	TotalSize      int64
	DeletedEntries int64
	SkippedDirs    uint64
	Duration       time.Duration
}

// IndexProgress receives user-facing updates during RunIndexMode; nil
// disables reporting (the daemon-spawned subprocess only logs). Step marks
// the start of a phase. ScanProgress fires from a sampling goroutine while
// traversal runs, never concurrently with Step.
type IndexProgress interface {
	Step(message string)
	ScanProgress(dirs, files, size uint64)
}

// startScanProgress samples the traversal counters twice a second and
// forwards them to progress. The returned stop function halts sampling,
// waits for the sampler to exit, and emits one final exact snapshot.
func startScanProgress(progress IndexProgress, index *indexing.Index) (stop func()) {
	if progress == nil {
		return func() {}
	}
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				progress.ScanProgress(index.Counts())
			}
		}
	}()
	return func() {
		close(done)
		<-finished
		progress.ScanProgress(index.Counts())
	}
}

// RunIndexMode performs one full atomic-generation scan.
func RunIndexMode(ctx context.Context, cfg configfile.Config, dbPath string, progress IndexProgress) (IndexRunStats, error) {
	if ctx == nil {
		ctx = context.TODO()
	}
	start := time.Now()
	cfg, err := configfile.Normalize(cfg)
	if err != nil {
		return IndexRunStats{}, err
	}
	slog.Info("running in index mode",
		"include_network_mounts", cfg.IncludeNetworkMounts,
		"exclude_paths", cfg.ExcludePaths,
	)

	if progress != nil {
		progress.Step("Opening database")
	}
	db, err := storage.OpenContext(ctx, dbPath, storage.DefaultOpenOptions())
	if err != nil {
		return IndexRunStats{}, fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			slog.Warn("failed to close database", "err", closeErr)
		}
	}()

	runStats, err := runIndex(ctx, db, cfg, progress)
	if err != nil {
		return IndexRunStats{}, fmt.Errorf("run index: %w", err)
	}

	if progress != nil {
		progress.Step("Pruning old index generation")
	}
	stats, err := storage.PruneOldIndexes(ctx, db, 1)
	if err != nil {
		return IndexRunStats{}, fmt.Errorf("prune old indexes: %w", err)
	}
	runStats.DeletedEntries = stats.DeletedEntries

	slog.Info("index complete, subprocess exiting")
	runStats.Duration = time.Since(start)
	return runStats, nil
}

// runIndex performs the actual indexing work
func runIndex(ctx context.Context, db *sql.DB, cfg configfile.Config, progress IndexProgress) (IndexRunStats, error) {
	index := indexing.Initialize("/", indexing.WithNetworkMounts(cfg.IncludeNetworkMounts), indexing.WithExcludePaths(configfile.EffectiveExcludePaths(cfg)))

	start := time.Now()

	// Prepare index record in database
	indexID, err := prepareIndexRecord(ctx, db)
	if err != nil {
		return IndexRunStats{}, fmt.Errorf("prepare index record: %w", err)
	}

	// Create streaming writer with a 1000-entry channel buffer; SQL writes
	// are batched separately by storage (500 rows or 1s, whichever first).
	writer := storage.NewStreamingWriter(ctx, db, indexID, 1000, nil)
	index.EnableStreaming(writer)

	// Start filesystem traversal
	if progress != nil {
		progress.Step("Scanning filesystem")
	}
	slog.Info("starting filesystem traversal")
	stopScanProgress := startScanProgress(progress, index)
	indexErr := index.StartIndexing(ctx)
	stopScanProgress()
	if indexErr != nil {
		if closeErr := writer.Close(); closeErr != nil {
			slog.Warn("failed to close streaming writer after index error", "err", closeErr)
		}
		return IndexRunStats{}, fmt.Errorf("indexing failed: %w", indexErr)
	}

	// Flush remaining batches.
	if closeErr := writer.Close(); closeErr != nil {
		return IndexRunStats{}, fmt.Errorf("streaming writer close: %w", closeErr)
	}

	// Update index metadata
	if _, err := db.ExecContext(ctx, `
		UPDATE indexes SET
			num_dirs = ?,
			num_files = ?,
			total_size = ?,
			last_indexed = ?
		WHERE id = ?;
	`,
		index.NumDirs,
		index.NumFiles,
		int64(index.GetTotalSize()),
		time.Now().UTC().Unix(),
		indexID,
	); err != nil {
		return IndexRunStats{}, fmt.Errorf("update index metadata: %w", err)
	}

	slog.Info("index complete",
		"duration", time.Since(start).Truncate(time.Millisecond),
		"dirs", index.NumDirs,
		"files", index.NumFiles,
		"size", index.GetTotalSize(),
		"skipped_dirs", index.SkippedDirCount(),
	)

	if progress != nil {
		progress.Step("Checkpointing database")
	}
	if stats, err := storage.WALCheckpointTruncate(ctx, db); err != nil {
		slog.Warn("WAL checkpoint failed after index", "err", err)
	} else {
		slog.Info("WAL checkpoint complete after index", "duration", stats.Duration, "busy", stats.Busy, "log", stats.Log, "checkpointed", stats.Checkpointed)
	}
	if err := storage.ReleaseSQLiteMemory(ctx, db); err != nil {
		slog.Warn("failed to release SQLite memory after index", "err", err)
	}

	return IndexRunStats{
		Files:       int64(index.NumFiles),
		Dirs:        int64(index.NumDirs),
		TotalSize:   int64(index.GetTotalSize()),
		SkippedDirs: index.SkippedDirCount(),
		Duration:    time.Since(start),
	}, nil
}

// prepareIndexRecord inserts the unpublished generation this scan writes into.
// Rows with last_indexed=0 are invisible to readers until publication.
//
// Full scans insert a brand-new generation row with last_indexed=0. Such
// rows are invisible to LatestIndexID until the final metadata update
// publishes them, so readers keep the previous complete generation.
func prepareIndexRecord(ctx context.Context, db *sql.DB) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			slog.Warn("prepareIndexRecord rollback failed", "err", rollbackErr)
		}
	}()

	if _, execErr := tx.ExecContext(ctx, `DELETE FROM indexes WHERE last_indexed = 0;`); execErr != nil {
		return 0, fmt.Errorf("clean up incomplete generations: %w", execErr)
	}
	res, err := tx.ExecContext(ctx, `
		INSERT INTO indexes (last_indexed)
		VALUES (0);
	`)
	if err != nil {
		return 0, fmt.Errorf("insert index generation: %w", err)
	}
	indexID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read generation id: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}

	return indexID, nil
}

// quarantineDatabaseFiles renames a database and its sidecars aside instead of
// deleting them, so the files stay available for recovery or inspection.
func quarantineDatabaseFiles(dbPath, reason string) error {
	suffix := "." + reason + "-" + time.Now().UTC().Format("20060102T150405Z")
	if err := os.Rename(dbPath, dbPath+suffix); err != nil {
		return fmt.Errorf("failed to quarantine database: %w", err)
	}
	for _, sidecar := range []string{"-wal", "-shm"} {
		if err := os.Rename(dbPath+sidecar, dbPath+suffix+sidecar); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("failed to quarantine database sidecar", "path", dbPath+sidecar, "err", err)
		}
	}
	slog.Warn("database quarantined", "path", dbPath+suffix, "reason", reason)
	return nil
}

// LatestIndex represents the most recent index metadata
type LatestIndex struct {
	NumDirs     int64
	NumFiles    int64
	TotalSize   int64
	LastIndexed sql.NullInt64
}

// loadLatestIndex fetches the most recent index metadata from the database
func loadLatestIndex(ctx context.Context, db *sql.DB) (*LatestIndex, error) {
	var li LatestIndex
	var numDirs, numFiles, totalSize sql.NullInt64

	err := db.QueryRowContext(ctx, `
		SELECT num_dirs, num_files, total_size, last_indexed
		FROM indexes
		WHERE last_indexed > 0
		ORDER BY last_indexed DESC, id DESC
		LIMIT 1
	`).Scan(&numDirs, &numFiles, &totalSize, &li.LastIndexed)
	if err != nil {
		return nil, err
	}

	if numDirs.Valid {
		li.NumDirs = numDirs.Int64
	}
	if numFiles.Valid {
		li.NumFiles = numFiles.Int64
	}
	if totalSize.Valid {
		li.TotalSize = totalSize.Int64
	}

	return &li, nil
}

func logLatestIndexStatus(db *sql.DB) {
	ctx := context.Background()
	li, err := loadLatestIndex(ctx, db)

	switch err {
	case nil:
		last := "unknown"
		if li.LastIndexed.Valid && li.LastIndexed.Int64 > 0 {
			last = time.Unix(li.LastIndexed.Int64, 0).UTC().Format(time.RFC3339)
		}
		slog.Info("latest index loaded", "last_indexed", last, "dirs", li.NumDirs, "files", li.NumFiles)
	case sql.ErrNoRows:
		slog.Info("no prior index metadata found in database")
	default:
		slog.Warn("could not load latest index metadata", "err", err)
	}
}
