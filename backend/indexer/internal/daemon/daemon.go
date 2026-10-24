package daemon

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/storage"
)

// daemon is the long-running server. Its roles live in sibling files: server.go
// (HTTP wiring), idle.go (idle shutdown), operations.go and reindex.go (index
// lifecycle), supervisor.go (worker subprocess), settings.go and sse.go.
type daemon struct {
	settings       settings
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

// New opens the cache, recovering from corruption, and returns a daemon that
// uses cfg for scans and persists changes to configPath. An empty configPath
// means the installed default.
func New(cfg configfile.Config, configPath string) (*daemon, error) {
	cfg, err := configfile.Normalize(cfg)
	if err != nil {
		return nil, err
	}
	if configPath == "" {
		configPath = configfile.DefaultPath()
	}

	store, err := storage.OpenStoreRecovering(context.Background(), storage.DefaultDBPath, indexing.SameFile)
	if err != nil {
		return nil, err
	}
	slog.Debug("database connection pool opened")
	logStoreStatus(store)

	return &daemon{settings: settings{cfg: cfg, path: configPath}, store: store}, nil
}

func logStoreStatus(store *storage.Store) {
	journalCtx, cancel := context.WithTimeout(context.Background(), startupProbeTimeout)
	defer cancel()
	if journalMode, err := store.JournalMode(journalCtx); err != nil {
		slog.Warn("failed to determine database journal_mode", "error", err)
	} else {
		slog.Debug("database journal_mode", "mode", strings.ToUpper(journalMode))
	}

	latest, err := store.LatestGeneration(context.Background())
	switch {
	case err == nil:
		slog.Debug("latest index loaded", "last_indexed", latest.LastIndexed.Format(time.RFC3339), "dirs", latest.NumDirs, "files", latest.NumFiles)
	case errors.Is(err, storage.ErrNoGeneration):
		slog.Debug("no prior index metadata found in database")
	default:
		slog.Warn("could not load latest index metadata", "error", err)
	}
}

// goBackground runs fn on a tracked goroutine so Close can wait for it before
// tearing down the database. Detached index work must go through this, or
// shutdown races it against the store closing.
func (d *daemon) goBackground(fn func()) {
	d.bgWG.Go(func() {
		fn()
	})
}

// waitBackground waits for every daemon-owned operation before closing SQLite.
func (d *daemon) waitBackground() {
	d.bgWG.Wait()
}

func (d *daemon) Close() {
	slog.Debug("shutting down daemon")

	// Signal in-flight background work to cancel before tearing down servers/db.
	if d.bgCancel != nil {
		d.bgCancel()
	}

	d.shutdownHTTPServers()

	// Wait for cancelled maintenance work to unwind before closing the DB it
	// writes to; closing under a running reindex turns cancellation into
	// spurious write errors.
	d.waitBackground()

	if d.store != nil {
		if err := d.store.Close(); err != nil {
			slog.Warn("database close error", "error", err)
		}
	}

	slog.Info("daemon shutdown complete")
}
