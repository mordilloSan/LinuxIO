package daemon

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/storage"
)

// DaemonConfig controls the long-running server.
type DaemonConfig struct {
	IndexName            string
	IndexPath            string
	ExcludePaths         []string
	IncludeHidden        bool
	IncludeNetworkMounts bool
	FreshIndex           bool
	KeepIndexes          int
	IntegrityCheck       string
	DBPath               string
	DBOptions            storage.OpenOptions
	SearchDefaultLimit   int
	SearchMaxLimit       int
	EntriesDefaultLimit  int
	EntriesMaxLimit      int
	SocketPath           string
	ListenAddr           string
	Interval             time.Duration
	IdleTimeout          time.Duration
	ConfigPath           string
}

type daemon struct {
	cfg              DaemonConfig
	cfgMu            sync.RWMutex
	savedConfig      configfile.Config
	activeSocketPath string
	db               *sql.DB
	store            *storage.Store
	servers          []*http.Server
	running          atomic.Bool
	workStreamMu     sync.RWMutex
	workStream       *workStreamBroadcaster
	usedSystemdSock  bool
	activeRequests   atomic.Int64
	lastActivityUnix atomic.Int64
	bgCtx            context.Context
	bgCancel         context.CancelFunc
	bgWG             sync.WaitGroup
}

// goBackground runs fn on a tracked goroutine so Close can wait for it before
// tearing down the database. Detached maintenance work (index, reindex,
// vacuum, prune) must go through this, or shutdown races it against db.Close.
func (d *daemon) goBackground(fn func()) {
	d.bgWG.Go(func() {
		fn()
	})
}

// waitBackground waits for tracked background work to finish, giving up after
// timeout so a wedged operation cannot hang shutdown forever.
func (d *daemon) waitBackground(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		d.bgWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		slog.Warn("background operations did not finish before shutdown timeout", "timeout", timeout)
	}
}

const databaseCheckTimeout = 30 * time.Second

var idleCheckInterval = 5 * time.Second

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
	interval, err := configfile.ParseInterval(cfg.Interval)
	if err != nil {
		return err
	}
	idleTimeout, err := configfile.ParseIdleTimeout(cfg.IdleTimeout)
	if err != nil {
		return err
	}
	dbOptions, err := configfile.DBOpenOptions(cfg)
	if err != nil {
		return err
	}
	dst.IndexPath = cfg.IndexPath
	dst.IndexName = cfg.IndexName
	dst.ExcludePaths = append([]string(nil), cfg.ExcludePaths...)
	dst.IncludeHidden = cfg.IncludeHidden
	dst.IncludeNetworkMounts = cfg.IncludeNetworkMounts
	dst.FreshIndex = cfg.FreshIndex
	dst.KeepIndexes = cfg.KeepIndexes
	dst.IntegrityCheck = string(cfg.IntegrityCheck)
	dst.DBPath = cfg.DBPath
	dst.DBOptions = dbOptions
	dst.SearchDefaultLimit = cfg.SearchDefaultLimit
	dst.SearchMaxLimit = cfg.SearchMaxLimit
	dst.EntriesDefaultLimit = cfg.EntriesDefaultLimit
	dst.EntriesMaxLimit = cfg.EntriesMaxLimit
	dst.SocketPath = cfg.SocketPath
	dst.ListenAddr = cfg.ListenAddr
	dst.Interval = interval
	dst.IdleTimeout = idleTimeout
	return nil
}

func NewDaemon(cfg DaemonConfig) (*daemon, error) {
	if cfg.IndexPath == "" {
		return nil, fmt.Errorf("index path is required")
	}
	if cfg.IndexName == "" {
		name := strings.ReplaceAll(cfg.IndexPath, "/", "_")
		if name == "" || name == "_" {
			name = "root"
		}
		cfg.IndexName = name
	}
	switch cfg.SocketPath {
	case "-":
		cfg.SocketPath = ""
	case "":
		cfg.SocketPath = "/run/linuxio/indexer.sock"
	}
	if cfg.DBPath == "" {
		cfg.DBPath = configfile.Defaults().DBPath
	}
	integrityCheck, err := configfile.NormalizeIntegrityCheck(cfg.IntegrityCheck)
	if err != nil {
		return nil, err
	}
	cfg.IntegrityCheck = integrityCheck
	if cfg.DBOptions == (storage.OpenOptions{}) {
		cfg.DBOptions = storage.DefaultOpenOptions()
	}
	if cfg.ConfigPath == "" {
		cfg.ConfigPath = configfile.PathFromEnvOrDefault()
	}

	db, err := storage.Open(cfg.DBPath, cfg.DBOptions)
	if err != nil {
		return nil, err
	}

	slog.Info("DB connection pool opened", "db", cfg.DBPath)
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

	bgCtx, bgCancel := context.WithCancel(context.Background())
	return &daemon{
		cfg:              cfg,
		savedConfig:      savedConfig,
		activeSocketPath: cfg.SocketPath,
		db:               db,
		store:            storage.NewStoreWithDB(db, cfg.DBPath),
		bgCtx:            bgCtx,
		bgCancel:         bgCancel,
	}, nil
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
	d.waitBackground(30 * time.Second)

	// Close database connection
	if d.db != nil {
		if err := d.db.Close(); err != nil {
			slog.Warn("database close error", "err", err)
		}
	}

	// Remove Unix socket only if we created it (not systemd-managed)
	activeSocketPath := d.activeSocketPath
	if activeSocketPath == "" {
		activeSocketPath = d.configSnapshot().SocketPath
	}
	if activeSocketPath != "" && !d.usedSystemdSock {
		if err := os.Remove(activeSocketPath); err != nil && !os.IsNotExist(err) {
			slog.Warn("failed to remove socket", "socket", activeSocketPath, "err", err)
		}
	}

	slog.Info("daemon shutdown complete")
}

func (d *daemon) shutdownHTTPServers() {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, srv := range d.servers {
		if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("server shutdown error", "err", err)
		}
	}
}

// getUnixListener creates the standalone Unix listener used without systemd activation.
func (d *daemon) getUnixListener() (net.Listener, error) {
	cfg := d.configSnapshot()
	d.usedSystemdSock = false
	d.activeSocketPath = cfg.SocketPath
	// Only remove a socket that is actually stale. If another daemon is
	// serving on it, dialing succeeds and startup must fail instead of
	// silently unlinking the live socket from under it.
	if conn, dialErr := net.DialTimeout("unix", cfg.SocketPath, time.Second); dialErr == nil {
		if closeErr := conn.Close(); closeErr != nil {
			slog.Warn("failed to close socket probe connection", "err", closeErr)
		}
		return nil, fmt.Errorf("socket %s is in use by another running daemon", cfg.SocketPath)
	}
	if err := os.Remove(cfg.SocketPath); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale socket: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.SocketPath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir socket dir: %w", err)
	}

	l, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("listen on unix socket: %w", err)
	}
	if err := os.Chmod(cfg.SocketPath, 0o600); err != nil {
		if closeErr := l.Close(); closeErr != nil {
			slog.Warn("failed to close listener after chmod error", "err", closeErr)
		}
		return nil, fmt.Errorf("chmod socket: %w", err)
	}

	return l, nil
}

const listenFDsStart = 3

func systemdListeners() ([]net.Listener, error) {
	defer func() {
		_ = os.Unsetenv("LISTEN_PID")
		_ = os.Unsetenv("LISTEN_FDS")
		_ = os.Unsetenv("LISTEN_FDNAMES")
	}()

	pidString := os.Getenv("LISTEN_PID")
	if pidString == "" {
		return nil, nil
	}
	pid, err := strconv.Atoi(pidString)
	if err != nil {
		return nil, fmt.Errorf("invalid LISTEN_PID %q: %w", pidString, err)
	}
	if pid != os.Getpid() {
		return nil, nil
	}

	fdsString := os.Getenv("LISTEN_FDS")
	if fdsString == "" {
		return nil, nil
	}
	nfds, err := strconv.Atoi(fdsString)
	if err != nil {
		return nil, fmt.Errorf("invalid LISTEN_FDS %q: %w", fdsString, err)
	}
	if nfds <= 0 {
		return nil, nil
	}

	files := make([]*os.File, 0, nfds)
	defer func() {
		for _, file := range files {
			_ = file.Close()
		}
	}()
	for i := range nfds {
		fd := listenFDsStart + i
		syscall.CloseOnExec(fd)
		file := os.NewFile(uintptr(fd), fmt.Sprintf("LISTEN_FD_%d", fd))
		if file == nil {
			return nil, fmt.Errorf("invalid fd %d from systemd", fd)
		}
		files = append(files, file)
	}
	return listenersFromFiles(files)
}

func listenersFromFiles(files []*os.File) ([]net.Listener, error) {
	listeners := make([]net.Listener, 0, len(files))
	for _, file := range files {
		listener, err := net.FileListener(file)
		if err != nil {
			closeListeners(listeners)
			return nil, fmt.Errorf("wrap fd %d: %w", file.Fd(), err)
		}
		listeners = append(listeners, listener)
	}
	return listeners, nil
}

func closeListeners(listeners []net.Listener) {
	for _, listener := range listeners {
		_ = listener.Close()
	}
}

type activatedListenerSet struct {
	unix net.Listener
	tcp  net.Listener
}

func loadActivatedListeners() (activatedListenerSet, error) {
	listeners, err := systemdListeners()
	if err != nil {
		return activatedListenerSet{}, fmt.Errorf("load socket activation listeners: %w", err)
	}
	return loadActivatedListenersFrom(listeners)
}

func loadActivatedListenersFrom(listeners []net.Listener) (activatedListenerSet, error) {
	var activated activatedListenerSet
	for _, listener := range listeners {
		switch listener.Addr().Network() {
		case "unix":
			if activated.unix != nil {
				closeListeners(listeners)
				return activatedListenerSet{}, fmt.Errorf("multiple activated Unix listeners")
			}
			activated.unix = listener
		case "tcp", "tcp4", "tcp6":
			if activated.tcp != nil {
				closeListeners(listeners)
				return activatedListenerSet{}, fmt.Errorf("multiple activated TCP listeners")
			}
			activated.tcp = listener
		default:
			closeListeners(listeners)
			return activatedListenerSet{}, fmt.Errorf("unsupported activated listener network %q", listener.Addr().Network())
		}
	}
	return activated, nil
}

func (d *daemon) configuredUnixListener(socketPath string, activated net.Listener) (net.Listener, error) {
	if socketPath == "" {
		if activated != nil {
			_ = activated.Close()
		}
		return nil, nil
	}
	if activated != nil {
		d.usedSystemdSock = true
		return activated, nil
	}
	return d.getUnixListener()
}

// Run starts the HTTP server and blocks until context is cancelled.
// backgroundContext returns the daemon-lifetime context used by async handler
// goroutines. Falls back to context.Background() for tests that construct a
// daemon directly without going through NewDaemon/Run.
func (d *daemon) backgroundContext() context.Context {
	if d.bgCtx != nil {
		return d.bgCtx
	}
	return context.Background()
}

func (d *daemon) Run(ctx context.Context) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	if d.bgCancel != nil {
		d.bgCancel()
	}
	d.bgCtx = runCtx
	d.bgCancel = cancel

	if shouldStopWhenIdle(d.configSnapshot()) {
		d.markActivity()
		go d.stopWhenIdle(runCtx, cancel)
	}

	return d.startHTTP(runCtx)
}

func shouldStopWhenIdle(cfg DaemonConfig) bool {
	return cfg.IdleTimeout > 0
}

func (d *daemon) startHTTP(ctx context.Context) error {
	cfg := d.configSnapshot()
	mux := http.NewServeMux()
	mux.HandleFunc(api.RouteOpenAPI, serveOpenapi)
	mux.HandleFunc(api.RouteIndex, d.handleIndex)
	mux.HandleFunc(api.RouteReindex, d.handleReindex)
	mux.HandleFunc(api.RouteVacuum, d.handleVacuum)
	mux.HandleFunc(api.RoutePrune, d.handlePrune)
	mux.HandleFunc(api.RouteStatus, d.handleStatus)
	mux.HandleFunc(api.RouteSearch, d.handleSearch)
	mux.HandleFunc(api.RouteDirSize, d.handleDirSize)
	mux.HandleFunc(api.RouteEntryCount, d.handleEntryCount)
	mux.HandleFunc(api.RouteSubfolders, d.handleSubfolders)
	mux.HandleFunc(api.RouteAdd, d.handleAdd)
	mux.HandleFunc(api.RouteDelete, d.handleDelete)
	mux.HandleFunc(api.RouteEntries, d.handleEntries)
	mux.HandleFunc(api.RouteConfig, d.handleConfig)

	handler := d.activityMiddleware(loggerMiddleware(recoveryMiddleware(authorizeTransportMiddleware(mux))))
	errorLog := log.New(httpErrorLogAdapter{}, "", 0)
	activated, err := loadActivatedListeners()
	if err != nil {
		return err
	}

	errCh := make(chan error, 2)
	serverCount := 0

	unixListener, err := d.configuredUnixListener(cfg.SocketPath, activated.unix)
	if err != nil {
		if activated.tcp != nil {
			_ = activated.tcp.Close()
		}
		return err
	}
	if unixListener != nil {
		srv := newHTTPServer(handler, errorLog)
		srv.ConnContext = unixConnContext
		d.servers = append(d.servers, srv)
		serverCount++
		if d.usedSystemdSock {
			slog.Info("API listening", "addr", "unix://"+cfg.SocketPath, "systemd_socket_activation", true)
		} else {
			slog.Info("API listening", "addr", "unix://"+cfg.SocketPath)
		}
		go func() {
			errCh <- srv.Serve(unixListener)
		}()
	}

	// TCP is only available through the privileged systemd socket unit.
	switch {
	case cfg.ListenAddr != "" && activated.tcp != nil:
		tcpSrv := newHTTPServer(handler, errorLog)
		tcpSrv.ConnContext = tcpConnContext
		d.servers = append(d.servers, tcpSrv)
		serverCount++
		slog.Info("API listening", "addr", "http://"+activated.tcp.Addr().String(), "systemd_socket_activation", true)
		go func() {
			errCh <- tcpSrv.Serve(activated.tcp)
		}()
	case cfg.ListenAddr != "":
		slog.Warn("configured TCP listener is not active; systemd socket activation is required", "listen_addr", cfg.ListenAddr)
	case activated.tcp != nil:
		_ = activated.tcp.Close()
	}

	if serverCount == 0 {
		return fmt.Errorf("no listeners configured")
	}

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

func newHTTPServer(handler http.Handler, errorLog *log.Logger) *http.Server {
	return &http.Server{
		Handler:           handler,
		ErrorLog:          errorLog,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// SSE responses can legitimately remain open for an entire index run.
		WriteTimeout:   0,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 64 << 10,
	}
}

func (d *daemon) markActivity() {
	d.lastActivityUnix.Store(time.Now().UnixNano())
}

func (d *daemon) activityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.activeRequests.Add(1)
		d.markActivity()
		defer func() {
			d.activeRequests.Add(-1)
			d.markActivity()
		}()
		next.ServeHTTP(w, r)
	})
}

func (d *daemon) stopWhenIdle(ctx context.Context, cancel context.CancelFunc) {
	ticker := time.NewTicker(idleCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cfg := d.configSnapshot()
			if cfg.IdleTimeout <= 0 {
				continue
			}
			last := d.lastActivityUnix.Load()
			if last == 0 || d.activeRequests.Load() > 0 || d.running.Load() {
				continue
			}
			idleFor := time.Since(time.Unix(0, last))
			if idleFor >= cfg.IdleTimeout {
				slog.Info("idle timeout reached; shutting down daemon", "idle_for", idleFor.Truncate(time.Second), "idle_timeout", cfg.IdleTimeout)
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

// runIndexSubprocess spawns the current binary with --index-mode flag
// Uses systemd-run --scope to isolate memory accounting from the daemon's cgroup.
// Wire events decoded from the subprocess stdout stream to onEvent (may be
// nil); the returned stats come from the subprocess's summary event, nil when
// it never emitted one.
func (d *daemon) runIndexSubprocess(ctx context.Context, onEvent func(indexWireEvent)) (*IndexRunStats, error) {
	if indexerSpawnOverride != nil {
		return indexerSpawnOverride(d, ctx, onEvent)
	}
	cfg := d.configSnapshot()
	// Build args for the index binary
	args := []string{
		"--index-mode",
		"--config-file", cfg.ConfigPath,
		"--path", cfg.IndexPath,
		"--name", cfg.IndexName,
		"--db-path", cfg.DBPath,
		"--include-hidden=" + strconv.FormatBool(cfg.IncludeHidden),
		"--include-network-mounts=" + strconv.FormatBool(cfg.IncludeNetworkMounts),
		"--fresh=" + strconv.FormatBool(cfg.FreshIndex),
		"--fts-search=" + strconv.FormatBool(!cfg.DBOptions.DisableFTS),
		"--keep-indexes", strconv.Itoa(cfg.KeepIndexes),
		"--integrity-check", cfg.IntegrityCheck,
	}
	args = appendDBOptionArgs(args, cfg.DBOptions)
	if len(cfg.ExcludePaths) == 0 {
		args = append(args, "--exclude-path=")
	} else {
		for _, path := range cfg.ExcludePaths {
			args = append(args, "--exclude-path", path)
		}
	}

	// Use systemd-run to spawn in a separate cgroup/scope when available.
	// This ensures proper memory accounting - the daemon's cgroup won't include subprocess memory.
	// systemd-run --scope propagates the child's exit code, so a failure of the
	// real run is indistinguishable from systemd-run itself being unusable;
	// probe scope creation with a no-op first instead of falling back after the
	// fact, which would rerun a genuinely failed index from scratch.
	slog.Info("starting index process")

	if !systemdScopeAvailable(ctx) {
		slog.Info("systemd-run unavailable; running index directly")
		stats, err := runIndexProcess(exec.CommandContext(ctx, os.Args[0], args...), onEvent)
		if err != nil {
			return nil, err
		}
		slog.Info("index process completed successfully", "mode", "direct")
		return stats, nil
	}

	unitName := fmt.Sprintf("linuxio-indexer-index-%d", time.Now().Unix())
	systemdArgs := []string{
		"--scope",            // Run as a transient scope (not a full service)
		"--unit=" + unitName, // Give it a unique name
		os.Args[0],           // The binary to run (still secure - uses os.Args[0])
	}
	systemdArgs = append(systemdArgs, args...) // Add index-mode args

	stats, err := runIndexProcess(exec.CommandContext(ctx, "systemd-run", systemdArgs...), onEvent)
	if err != nil {
		return nil, err
	}

	slog.Info("index process completed successfully", "mode", "systemd-run")
	return stats, nil
}

// systemdScopeAvailable reports whether systemd-run can create transient
// scopes right now, verified by running a no-op command in one.
func systemdScopeAvailable(ctx context.Context) bool {
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return false
	}
	probe := exec.CommandContext(ctx, "systemd-run", "--scope", "--quiet", "true")
	if err := probe.Run(); err != nil {
		slog.Debug("systemd-run scope probe failed", "err", err)
		return false
	}
	return true
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

// RunIndexMode performs the index with the selected existing-database
// integrity check and returns run statistics when complete.
func RunIndexMode(indexName, indexPath string, excludePaths []string, includeHidden, includeNetworkMounts, fresh bool, dbPath string, keepIndexes int, integrityCheck string, dbOptions storage.OpenOptions, progress IndexProgress) (IndexRunStats, error) {
	start := time.Now()
	integrityCheck, err := configfile.NormalizeIntegrityCheck(integrityCheck)
	if err != nil {
		return IndexRunStats{}, err
	}
	slog.Info("running in index mode",
		"path", indexPath,
		"name", indexName,
		"db", dbPath,
		"fresh", fresh,
		"include_hidden", includeHidden,
		"include_network_mounts", includeNetworkMounts,
		"exclude_paths", excludePaths,
		"keep_indexes", keepIndexes,
		"integrity_check", integrityCheck,
	)

	lock, err := tryAcquireOperationLock(dbPath)
	if err != nil {
		return IndexRunStats{}, err
	}
	defer func() {
		if closeErr := lock.Close(); closeErr != nil {
			slog.Warn("failed to release operation lock", "err", closeErr)
		}
	}()

	if progress != nil {
		switch {
		case !fileExists(dbPath):
			progress.Step("Creating new database")
		case integrityCheck == configfile.IntegrityCheckOff:
			progress.Step("Opening existing database")
		default:
			progress.Step("Checking database integrity")
		}
	}
	db, _, err := openDatabaseWithIntegrityCheck(dbPath, dbOptions, integrityCheck)
	if err != nil {
		return IndexRunStats{}, fmt.Errorf("open database: %w", err)
	}
	defer func() {
		if closeErr := db.Close(); closeErr != nil {
			slog.Warn("failed to close database", "err", closeErr)
		}
	}()

	ctx := context.Background()

	// Fresh mode no longer wipes the database up front: the scan writes into
	// a new generation row that only becomes visible once it completes, so
	// readers keep the previous index during the scan and a failed scan
	// loses nothing. The superseded generation is pruned below instead.
	runStats, err := runIndex(ctx, db, indexName, indexPath, excludePaths, includeHidden, includeNetworkMounts, fresh, progress)
	if err != nil {
		return IndexRunStats{}, fmt.Errorf("run index: %w", err)
	}

	// Retention runs only when configured. A zero value deliberately keeps
	// every published generation so an operator can prune them manually.
	if keepIndexes > 0 {
		if progress != nil {
			progress.Step("Pruning old index generations")
		}
		stats, err := storage.PruneOldIndexes(ctx, db, keepIndexes, 0)
		if err != nil {
			return IndexRunStats{}, fmt.Errorf("prune old indexes: %w", err)
		}
		slog.Info("automatic index retention applied",
			"keep_indexes", keepIndexes,
			"deleted_indexes", stats.DeletedIndexes,
			"deleted_entries", stats.DeletedEntries,
			"duration", stats.Duration,
		)
	}

	slog.Info("index complete, subprocess exiting")
	runStats.Duration = time.Since(start)
	return runStats, nil
}

// runIndex performs the actual indexing work
func runIndex(ctx context.Context, db *sql.DB, indexName, indexPath string, excludePaths []string, includeHidden, includeNetworkMounts, fresh bool, progress IndexProgress) (IndexRunStats, error) {
	index := indexing.Initialize(indexName, indexPath, indexPath, includeHidden, indexing.WithNetworkMounts(includeNetworkMounts), indexing.WithExcludePaths(excludePaths))

	start := time.Now()

	// Prepare index record in database
	indexID, err := prepareIndexRecord(ctx, db, indexName, indexPath, includeHidden, fresh)
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
	indexErr := index.StartIndexing()
	stopScanProgress()
	if indexErr != nil {
		if closeErr := writer.Close(); closeErr != nil {
			slog.Warn("failed to close streaming writer after index error", "err", closeErr)
		}
		return IndexRunStats{}, fmt.Errorf("indexing failed: %w", indexErr)
	}

	// Flush remaining batches
	if progress != nil {
		progress.Step("Removing stale entries")
	}
	if closeErr := writer.Close(); closeErr != nil {
		return IndexRunStats{}, fmt.Errorf("streaming writer close: %w", closeErr)
	}

	// Cleanup deleted entries (files that were not seen during this scan)
	scanTime := writer.ScanTime()
	deleted, err := storage.CleanupDeletedEntries(ctx, db, indexID, scanTime)
	if err != nil {
		return IndexRunStats{}, fmt.Errorf("cleanup deleted entries: %w", err)
	}
	if deleted > 0 {
		slog.Info("cleaned up deleted entries", "deleted", deleted)
	}

	// Update index metadata
	if _, err := db.ExecContext(ctx, `
		UPDATE indexes SET
			num_dirs = ?,
			num_files = ?,
			total_size = ?,
			disk_used = ?,
			disk_total = ?,
			last_indexed = ?
		WHERE id = ?;
	`,
		index.NumDirs,
		index.NumFiles,
		int64(index.GetTotalSize()),
		int64(index.DiskUsed),
		int64(index.DiskTotal),
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
		Files:          int64(index.NumFiles),
		Dirs:           int64(index.NumDirs),
		TotalSize:      int64(index.GetTotalSize()),
		DeletedEntries: deleted,
		SkippedDirs:    index.SkippedDirCount(),
		Duration:       time.Since(start),
	}, nil
}

// prepareIndexRecord returns the index row this scan writes into.
//
// Fresh scans insert a brand-new generation row with last_indexed=0. Such
// rows are invisible to LatestIndexID until the final metadata update
// publishes them, so readers keep the previous complete generation for the
// whole scan and a failed scan changes nothing they can see. Non-fresh scans
// update the newest existing generation in place (incremental mode).
func prepareIndexRecord(ctx context.Context, db *sql.DB, indexName, indexPath string, includeHidden, fresh bool) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			slog.Warn("prepareIndexRecord rollback failed", "err", rollbackErr)
		}
	}()

	insertGeneration := func() (int64, error) {
		res, execErr := tx.ExecContext(ctx, `
			INSERT INTO indexes (name, root_path, source, include_hidden, last_indexed)
			VALUES (?, ?, ?, ?, 0);
		`, indexName, indexPath, indexPath, indexing.BoolToInt(includeHidden))
		if execErr != nil {
			return 0, fmt.Errorf("insert index generation: %w", execErr)
		}
		return res.LastInsertId()
	}

	var indexID int64
	if fresh {
		// Leftover incomplete generations are debris from crashed scans;
		// their entries cascade-delete with them.
		if _, execErr := tx.ExecContext(ctx, `DELETE FROM indexes WHERE name = ? AND last_indexed = 0;`, indexName); execErr != nil {
			return 0, fmt.Errorf("clean up incomplete generations: %w", execErr)
		}
		if indexID, err = insertGeneration(); err != nil {
			return 0, err
		}
	} else {
		scanErr := tx.QueryRowContext(ctx, `
			SELECT id FROM indexes WHERE name = ?
			ORDER BY last_indexed DESC, id DESC LIMIT 1;
		`, indexName).Scan(&indexID)
		switch {
		case errors.Is(scanErr, sql.ErrNoRows):
			if indexID, err = insertGeneration(); err != nil {
				return 0, err
			}
		case scanErr != nil:
			return 0, fmt.Errorf("query existing index: %w", scanErr)
		default:
			if _, err := tx.ExecContext(ctx, `
				UPDATE indexes SET root_path = ?, source = ?, include_hidden = ?
				WHERE id = ?;
			`, indexPath, indexPath, indexing.BoolToInt(includeHidden), indexID); err != nil {
				return 0, fmt.Errorf("update index record: %w", err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit transaction: %w", err)
	}

	return indexID, nil
}

// openDatabaseWithIntegrityCheck opens a database and performs the configured
// corruption check. If corruption is confirmed, it quarantines and recreates
// the database.
// Returns the opened database connection and whether it existed before.
func openDatabaseWithIntegrityCheck(dbPath string, dbOptions storage.OpenOptions, integrityCheck string) (*sql.DB, bool, error) {
	integrityCheck, err := configfile.NormalizeIntegrityCheck(integrityCheck)
	if err != nil {
		return nil, false, err
	}
	dbExisted := fileExists(dbPath)
	switch {
	case !dbExisted:
		slog.Info("database not found; creating new", "db", dbPath)
	case integrityCheck == configfile.IntegrityCheckOff:
		slog.Info("database exists; integrity check disabled", "db", dbPath)
	default:
		slog.Info("database exists; checking integrity", "db", dbPath, "mode", integrityCheck)
	}

	db, err := storage.Open(dbPath, dbOptions)
	if err != nil {
		return nil, false, err
	}

	if !dbExisted || integrityCheck == configfile.IntegrityCheckOff {
		return db, dbExisted, nil
	}

	if err := checkDatabaseIntegrityWithTimeout(db, integrityCheck); err != nil {
		return recreateDatabaseAfterIntegrityFailure(dbPath, dbOptions, db, err)
	}

	slog.Info("database integrity check passed", "mode", integrityCheck)
	return db, true, nil
}

func checkDatabaseIntegrityWithTimeout(db *sql.DB, integrityCheck string) error {
	ctx, cancel := context.WithTimeout(context.Background(), databaseCheckTimeout)
	defer cancel()
	return checkDatabaseIntegrity(ctx, db, integrityCheck)
}

func recreateDatabaseAfterIntegrityFailure(dbPath string, dbOptions storage.OpenOptions, db *sql.DB, integrityErr error) (*sql.DB, bool, error) {
	if !errors.Is(integrityErr, errDatabaseCorrupt) {
		// Timeouts, I/O errors, busy databases: operational failures, not
		// proof of corruption. Destroying the last known-good index over a
		// transient error is worse than failing this run and retrying.
		if closeErr := db.Close(); closeErr != nil {
			slog.Warn("failed to close database after integrity check error", "err", closeErr)
		}
		return nil, false, fmt.Errorf("database integrity check did not complete: %w", integrityErr)
	}

	slog.Warn("database corruption detected", "err", integrityErr)
	slog.Warn("quarantining corrupted database and recreating")
	if closeErr := db.Close(); closeErr != nil {
		slog.Warn("failed to close corrupted database", "err", closeErr)
	}
	if err := quarantineDatabaseFiles(dbPath); err != nil {
		return nil, false, err
	}

	db, err := storage.Open(dbPath, dbOptions)
	if err != nil {
		return nil, false, err
	}
	slog.Info("new database created", "db", dbPath)
	return db, false, nil
}

// quarantineDatabaseFiles renames a corrupted database and its sidecars aside
// instead of deleting them, so the damaged files stay available for recovery
// or inspection.
func quarantineDatabaseFiles(dbPath string) error {
	suffix := ".corrupt-" + time.Now().UTC().Format("20060102T150405Z")
	if err := os.Rename(dbPath, dbPath+suffix); err != nil {
		return fmt.Errorf("failed to quarantine corrupted database: %w", err)
	}
	for _, sidecar := range []string{"-wal", "-shm"} {
		if err := os.Rename(dbPath+sidecar, dbPath+suffix+sidecar); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("failed to quarantine database sidecar", "path", dbPath+sidecar, "err", err)
		}
	}
	slog.Warn("corrupted database quarantined", "path", dbPath+suffix)
	return nil
}

// errDatabaseCorrupt marks confirmed corruption: either the configured check
// reported damage, or SQLite itself failed with a corruption-class error.
var errDatabaseCorrupt = errors.New("database corrupt")

// checkDatabaseIntegrity runs SQLite's configured integrity check. The off
// mode deliberately performs no query.
func checkDatabaseIntegrity(ctx context.Context, db *sql.DB, integrityCheck string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	query, checkName, err := integrityCheckQuery(integrityCheck)
	if err != nil {
		return err
	}
	if query == "" {
		return nil
	}

	var result string
	err = db.QueryRowContext(ctx, query).Scan(&result)
	if err != nil {
		if storage.IsCorruptionError(err) {
			return fmt.Errorf("%w: %s query: %v", errDatabaseCorrupt, checkName, err)
		}
		return fmt.Errorf("%s query failed: %w", checkName, err)
	}
	if result != "ok" {
		return fmt.Errorf("%w: %s reported: %s", errDatabaseCorrupt, checkName, result)
	}
	return nil
}

func integrityCheckQuery(integrityCheck string) (query, checkName string, err error) {
	integrityCheck, err = configfile.NormalizeIntegrityCheck(integrityCheck)
	if err != nil {
		return "", "", err
	}
	switch integrityCheck {
	case configfile.IntegrityCheckFull:
		return "PRAGMA integrity_check;", "integrity_check", nil
	case configfile.IntegrityCheckQuick:
		return "PRAGMA quick_check;", "quick_check", nil
	case configfile.IntegrityCheckOff:
		return "", "", nil
	default:
		return "", "", fmt.Errorf("unsupported integrity check mode %q", integrityCheck)
	}
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// LatestIndex represents the most recent index metadata
type LatestIndex struct {
	Name        string
	NumDirs     int64
	NumFiles    int64
	TotalSize   int64
	LastIndexed sql.NullInt64
}

// loadLatestIndex fetches the most recent index metadata from the database
func loadLatestIndex(ctx context.Context, db *sql.DB) (*LatestIndex, error) {
	var li LatestIndex
	var name sql.NullString
	var numDirs, numFiles, totalSize sql.NullInt64

	err := db.QueryRowContext(ctx, `
		SELECT name, num_dirs, num_files, total_size, last_indexed
		FROM indexes
		WHERE last_indexed > 0
		ORDER BY last_indexed DESC, id DESC
		LIMIT 1
	`).Scan(&name, &numDirs, &numFiles, &totalSize, &li.LastIndexed)
	if err != nil {
		return nil, err
	}

	if name.Valid {
		li.Name = name.String
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
		slog.Info(
			fmt.Sprintf("latest index %q: %d dirs, %d files", li.Name, li.NumDirs, li.NumFiles),
			"name", li.Name, "last_indexed", last, "dirs", li.NumDirs, "files", li.NumFiles,
		)
	case sql.ErrNoRows:
		slog.Info("no prior index metadata found in database")
	default:
		slog.Warn("could not load latest index metadata", "err", err)
	}
}
