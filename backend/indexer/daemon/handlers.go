package daemon

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/indexer/api"
	"github.com/mordilloSan/LinuxIO/backend/indexer/indexing"
	"github.com/mordilloSan/LinuxIO/backend/indexer/internal/configfile"
	"github.com/mordilloSan/LinuxIO/backend/indexer/storage"
)

const (
	defaultSearchLimit  = configfile.DefaultSearchLimit
	maxSearchLimit      = configfile.DefaultSearchMaxLimit
	defaultEntriesLimit = configfile.DefaultEntriesLimit
	maxEntriesLimit     = configfile.DefaultEntriesMaxLimit

	// maxSearchQueryBytes bounds the raw q parameter. Combined with
	// iteminfo.MaxSearchTerms it caps the per-request scan cost of the
	// unauthenticated read-only TCP search endpoint.
	maxSearchQueryBytes = 256
)

type apiLimitSettings struct {
	SearchDefault  int
	SearchMax      int
	EntriesDefault int
	EntriesMax     int
}

func (d *daemon) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	if !d.tryLockIndex() {
		http.Error(w, "indexer already running", http.StatusConflict)
		return
	}

	stream := d.beginWorkStream("index", "")
	d.goBackground(func() {
		defer d.unlockIndex()
		defer d.endWorkStream(stream)

		start := time.Now()
		sendSSEEvent(stream, "started", WorkStartedEvent{
			Status:    "running",
			Operation: "index",
		})

		stats, err := d.runIndexSubprocess(d.backgroundContext(), func(evt indexWireEvent) {
			switch evt.Type {
			case "step":
				// CurrentPath mirrors reindex's "Deleting old entries..."
				// convention so path-displaying clients show phases too.
				sendSSEEvent(stream, "progress", WorkProgressEvent{
					Operation:   "index",
					Message:     evt.Message,
					CurrentPath: evt.Message + "...",
				})
			case "scan":
				sendSSEEvent(stream, "progress", WorkProgressEvent{
					Operation:    "index",
					Phase:        "scan",
					FilesIndexed: int64(evt.Files),
					DirsIndexed:  int64(evt.Dirs),
					BytesIndexed: int64(evt.Size),
				})
			}
		})
		if err != nil {
			slog.Error("manual index failed", "err", err)
			sendSSEError(stream, fmt.Sprintf("index failed: %v", err))
			return
		}

		complete := WorkCompleteEvent{
			Status:     "complete",
			Operation:  "index",
			DurationMs: time.Since(start).Milliseconds(),
		}
		if stats != nil {
			complete.FilesIndexed = stats.Files
			complete.DirsIndexed = stats.Dirs
			complete.TotalSize = stats.TotalSize
			complete.DeletedEntries = stats.DeletedEntries
		}
		sendSSEEvent(stream, "complete", complete)
	})
	writeJSONStatus(w, http.StatusAccepted, api.OperationResponse{Status: "running"})
}

func (d *daemon) handleReindex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		http.Error(w, "path parameter is required", http.StatusBadRequest)
		return
	}

	// Reject path traversal attempts
	if !indexing.ValidateRelativePath(path) {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}

	// Normalize the path
	normalizedPath := indexing.NormalizeIndexPath(path)

	if !d.tryLockIndex() {
		http.Error(w, "indexer already running", http.StatusConflict)
		return
	}

	lock, err := tryAcquireOperationLock(d.configSnapshot().DBPath)
	if err != nil {
		d.unlockIndex()
		if errors.Is(err, errOperationAlreadyRunning) {
			http.Error(w, "indexer already running", http.StatusConflict)
			return
		}
		http.Error(w, fmt.Sprintf("operation lock failed: %v", err), http.StatusInternalServerError)
		return
	}

	stream := d.beginWorkStream("reindex", normalizedPath)
	d.goBackground(func() {
		defer func() {
			if err := lock.Close(); err != nil {
				slog.Warn("failed to release operation lock", "err", err)
			}
		}()
		defer d.unlockIndex()
		defer d.endWorkStream(stream)
		sendSSEEvent(stream, "started", WorkStartedEvent{
			Status:    "running",
			Operation: "reindex",
			Path:      normalizedPath,
		})
		d.reindexPathWithProgress(d.backgroundContext(), normalizedPath, stream)
	})

	writeJSONStatus(w, http.StatusAccepted, api.OperationResponse{Status: "running", Path: normalizedPath})
}

func (d *daemon) handleVacuum(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}

	if !d.tryLockIndex() {
		http.Error(w, "indexer already running", http.StatusConflict)
		return
	}

	lock, err := tryAcquireOperationLock(d.configSnapshot().DBPath)
	if err != nil {
		d.unlockIndex()
		if errors.Is(err, errOperationAlreadyRunning) {
			http.Error(w, "indexer already running", http.StatusConflict)
			return
		}
		http.Error(w, fmt.Sprintf("operation lock failed: %v", err), http.StatusInternalServerError)
		return
	}

	stream := d.beginWorkStream("vacuum", "")
	d.goBackground(func() {
		defer func() {
			if err := lock.Close(); err != nil {
				slog.Warn("failed to release operation lock", "err", err)
			}
		}()
		defer d.unlockIndex()
		defer d.endWorkStream(stream)
		sendSSEEvent(stream, "started", WorkStartedEvent{
			Status:    "running",
			Operation: "vacuum",
		})
		d.vacuumWithProgress(d.backgroundContext(), stream)
	})

	writeJSONStatus(w, http.StatusAccepted, api.OperationResponse{Status: "running"})
}

func (d *daemon) handlePrune(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	keepLatestStr := r.URL.Query().Get("keep_latest")
	maxAgeDaysStr := r.URL.Query().Get("max_age_days")

	// Defaults: keep 1 latest index, delete indexes older than 30 days
	keepLatest := 1
	maxAgeDays := 30

	if keepLatestStr != "" {
		val, err := strconv.Atoi(keepLatestStr)
		if err != nil || val < 1 {
			http.Error(w, "keep_latest must be a positive integer", http.StatusBadRequest)
			return
		}
		keepLatest = val
	}
	if maxAgeDaysStr != "" {
		val, err := strconv.Atoi(maxAgeDaysStr)
		if err != nil || val < 1 {
			http.Error(w, "max_age_days must be a positive integer", http.StatusBadRequest)
			return
		}
		maxAgeDays = val
	}

	if !d.tryLockIndex() {
		http.Error(w, "indexer already running", http.StatusConflict)
		return
	}

	lock, err := tryAcquireOperationLock(d.configSnapshot().DBPath)
	if err != nil {
		d.unlockIndex()
		if errors.Is(err, errOperationAlreadyRunning) {
			http.Error(w, "indexer already running", http.StatusConflict)
			return
		}
		http.Error(w, fmt.Sprintf("operation lock failed: %v", err), http.StatusInternalServerError)
		return
	}

	stream := d.beginWorkStream("prune", "")
	d.goBackground(func() {
		defer func() {
			if err := lock.Close(); err != nil {
				slog.Warn("failed to release operation lock", "err", err)
			}
		}()
		defer d.unlockIndex()
		defer d.endWorkStream(stream)
		sendSSEEvent(stream, "started", WorkStartedEvent{
			Status:    "running",
			Operation: "prune",
		})
		d.pruneWithProgress(d.backgroundContext(), keepLatest, maxAgeDays, stream)
	})

	writeJSONStatus(w, http.StatusAccepted, api.OperationResponse{
		Status: "running", KeepLatest: fmt.Sprintf("%d", keepLatest), MaxAgeDays: fmt.Sprintf("%d", maxAgeDays),
	})
}

type statusResponse = api.StatusResponse

func (d *daemon) handleStatus(w http.ResponseWriter, r *http.Request) {
	if wantsStatusSSE(r) && d.attachStatusSSEIfActive(w, r) {
		return
	}

	running := d.running.Load()
	resp := d.newStatusResponse(running)
	if err := d.populateLatestIndexStatus(r.Context(), &resp); err != nil {
		if !running {
			http.Error(w, fmt.Sprintf("error loading status: %v", err), http.StatusInternalServerError)
			return
		}
		resp.AddWarning(fmt.Sprintf("latest index unavailable: %v", err))
		slog.Warn("status: latest index unavailable while indexing", "err", err)
	}
	if err := d.populateDatabaseStatus(r.Context(), &resp); err != nil {
		if !running {
			http.Error(w, fmt.Sprintf("error loading stats: %v", err), http.StatusInternalServerError)
			return
		}
		resp.AddWarning(fmt.Sprintf("stats unavailable: %v", err))
		slog.Warn("status: global stats unavailable while indexing", "err", err)
	}
	// Actual FTS state, probed fresh: the desired state lives in /config
	// (fts_search) and the two differ until the next scan reconciles the DB.
	resp.FTSActive = d.store.SearchIndexActive(r.Context())
	populateMemoryStatus(&resp, running)
	writeJSON(w, resp)
}

func (d *daemon) newStatusResponse(running bool) statusResponse {
	resp := statusResponse{ProtocolVersion: api.ProtocolVersion, Status: "idle"}
	if !running {
		return resp
	}
	resp.Status = "running"
	if active := d.getWorkStreamBroadcaster(); active != nil {
		resp.ActiveOperation = active.Operation()
		resp.ActivePath = active.Path()
	}
	return resp
}

func (d *daemon) populateLatestIndexStatus(ctx context.Context, resp *statusResponse) error {
	latest, err := loadLatestIndex(ctx, d.db)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	resp.NumDirs = latest.NumDirs
	resp.NumFiles = latest.NumFiles
	resp.TotalSize = latest.TotalSize
	if latest.LastIndexed.Valid && latest.LastIndexed.Int64 > 0 {
		resp.LastIndexed = time.Unix(latest.LastIndexed.Int64, 0).UTC().Format(time.RFC3339)
	}
	return nil
}

func (d *daemon) populateDatabaseStatus(ctx context.Context, resp *statusResponse) error {
	stats, err := d.store.GetStats(ctx)
	if err != nil {
		return err
	}
	resp.TotalIndexes = stats.TotalIndexes
	resp.TotalEntries = stats.TotalEntries
	resp.DatabaseSize = stats.DatabaseSize
	resp.WALSize = stats.WALSize
	resp.SHMSize = stats.SHMSize
	resp.TotalOnDisk = stats.TotalOnDisk
	return nil
}

func populateMemoryStatus(resp *statusResponse, running bool) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	resp.GoAllocBytes = ms.Alloc
	resp.GoHeapInuseBytes = ms.HeapInuse
	resp.GoHeapIdleBytes = ms.HeapIdle
	resp.GoHeapReleasedBytes = ms.HeapReleased
	resp.GoSysBytes = ms.Sys
	resp.GoNumGC = ms.NumGC

	if rss, err := procSelfRSSBytes(); err != nil {
		resp.AddWarning(fmt.Sprintf("rss unavailable: %v", err))
	} else {
		resp.RSSBytes = rss
	}
	if cg, err := cgroupV2Memory(); err == nil {
		resp.CgroupCurrent = cg.Current
		resp.CgroupAnon = cg.Anon
		resp.CgroupFile = cg.File
	} else if !running {
		// cgroup v2 is commonly unavailable outside systemd; warn only while idle.
		resp.AddWarning(fmt.Sprintf("cgroup mem unavailable: %v", err))
	}
}

func procSelfRSSBytes() (int64, error) {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		// Format: VmRSS:\t  12345 kB
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("unexpected VmRSS format: %q", line)
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, err
		}
		return kb * 1024, nil
	}
	if err := sc.Err(); err != nil {
		return 0, err
	}
	return 0, fmt.Errorf("VmRSS not found")
}

type cgroupMemInfo struct {
	Current int64
	Anon    int64
	File    int64
}

func cgroupV2Memory() (cgroupMemInfo, error) {
	// cgroup v2 path is in /proc/self/cgroup as: 0::/some/path
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return cgroupMemInfo{}, err
	}

	var rel string
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := sc.Text()
		if after, ok := strings.CutPrefix(line, "0::"); ok {
			rel = after
			break
		}
	}
	if scanErr := sc.Err(); scanErr != nil {
		return cgroupMemInfo{}, scanErr
	}
	if rel == "" {
		return cgroupMemInfo{}, fmt.Errorf("cgroup v2 path not found")
	}

	base := "/sys/fs/cgroup" + rel
	current, err := readInt64File(base + "/memory.current")
	if err != nil {
		return cgroupMemInfo{}, err
	}

	stat, err := os.ReadFile(base + "/memory.stat")
	if err != nil {
		return cgroupMemInfo{}, err
	}

	var anon, file int64
	sc = bufio.NewScanner(bytes.NewReader(stat))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		v, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		switch fields[0] {
		case "anon":
			anon = v
		case "file":
			file = v
		}
	}
	if err := sc.Err(); err != nil {
		return cgroupMemInfo{}, err
	}

	return cgroupMemInfo{Current: current, Anon: anon, File: file}, nil
}

func readInt64File(path string) (int64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(b))
	return strconv.ParseInt(s, 10, 64)
}

func (d *daemon) handleSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > maxSearchQueryBytes {
		http.Error(w, "search query too long", http.StatusBadRequest)
		return
	}
	limits := d.apiLimits()
	limit := queryBoundedInt(r.URL.Query().Get("limit"), limits.SearchDefault, 1, limits.SearchMax)
	basePath, ok := queryPathOrRoot(r.URL.Query().Get("base"))
	if !ok {
		http.Error(w, "invalid base path: path traversal not allowed", http.StatusBadRequest)
		return
	}
	results, err := d.store.SearchEntriesUnder(ctx, q, basePath, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, results)
}

func (d *daemon) handleEntries(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path, ok := queryPathOrRoot(r.URL.Query().Get("path"))
	if !ok {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}
	recursive := r.URL.Query().Get("recursive") == "true"
	limits := d.apiLimits()
	limit := queryBoundedInt(r.URL.Query().Get("limit"), limits.EntriesDefault, 1, limits.EntriesMax)
	offset := queryInt(r.URL.Query().Get("offset"), 0, 0)
	after := r.URL.Query().Get("after")

	results, err := d.store.QueryPath(ctx, path, recursive, limit, offset, after)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, results)
}

func (d *daemon) apiLimits() apiLimitSettings {
	cfg := d.configSnapshot()
	searchDefault, searchMax := normalizeLimitPair(cfg.SearchDefaultLimit, cfg.SearchMaxLimit, defaultSearchLimit, maxSearchLimit)
	entriesDefault, entriesMax := normalizeLimitPair(cfg.EntriesDefaultLimit, cfg.EntriesMaxLimit, defaultEntriesLimit, maxEntriesLimit)
	return apiLimitSettings{
		SearchDefault:  searchDefault,
		SearchMax:      searchMax,
		EntriesDefault: entriesDefault,
		EntriesMax:     entriesMax,
	}
}

func normalizeLimitPair(defaultLimit, maxLimit, fallbackDefault, fallbackMax int) (int, int) {
	if defaultLimit < 1 {
		defaultLimit = fallbackDefault
	}
	if maxLimit < 1 {
		maxLimit = fallbackMax
	}
	if defaultLimit > maxLimit {
		maxLimit = defaultLimit
	}
	return defaultLimit, maxLimit
}

func (d *daemon) handleDirSize(w http.ResponseWriter, r *http.Request) {
	path, ok := queryPathOrRoot(r.URL.Query().Get("path"))
	if !ok {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}
	total, err := d.store.DirSize(r.Context(), path)
	if err != nil {
		if errors.Is(err, storage.ErrDirectoryNotFound) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, api.DirSizeResponse{Path: path, Size: total})
}

func (d *daemon) handleEntryCount(w http.ResponseWriter, r *http.Request) {
	path, ok := queryPathOrRoot(r.URL.Query().Get("path"))
	if !ok {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}
	files, dirs, err := d.store.EntryCount(r.Context(), path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, api.EntryCountResponse{Path: path, Files: files, Dirs: dirs})
}

func (d *daemon) handleSubfolders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	path, ok := queryPathOrRoot(r.URL.Query().Get("path"))
	if !ok {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}

	results, err := d.store.GetDirectSubfolders(ctx, path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, results)
}

func (d *daemon) handleAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "use POST", http.StatusMethodNotAllowed)
		return
	}
	var payload api.EntryRequest
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, fmt.Sprintf("invalid body: %v", err), http.StatusBadRequest)
		return
	}
	if payload.Path == "" || payload.Name == "" {
		http.Error(w, "path and name are required", http.StatusBadRequest)
		return
	}
	if !indexing.ValidateRelativePath(payload.Path) {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}
	switch payload.Type {
	case "", "file", "directory":
	default:
		http.Error(w, `type must be "file" or "directory"`, http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	lock, err := tryAcquireOperationLock(d.configSnapshot().DBPath)
	if err != nil {
		if errors.Is(err, errOperationAlreadyRunning) {
			http.Error(w, "indexer already running", http.StatusConflict)
			return
		}
		http.Error(w, fmt.Sprintf("operation lock failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer func() {
		if closeErr := lock.Close(); closeErr != nil {
			slog.Warn("failed to release operation lock", "err", closeErr)
		}
	}()

	indexID, err := d.store.LatestIndexID(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("no index present: %v", err), http.StatusBadRequest)
		return
	}

	relPath := indexing.NormalizeIndexPath(payload.Path)
	cfg := d.configSnapshot()
	if indexing.IsPathExcluded(cfg.IndexPath, cfg.ExcludePaths, relPath) {
		writeJSON(w, api.OperationResponse{Status: "ok"})
		return
	}
	absPath := payload.AbsPath
	if absPath == "" {
		absPath = payload.Path
	}
	modUnix := payload.ModUnix
	if modUnix == 0 {
		modUnix = time.Now().UTC().Unix()
	}
	entryType := payload.Type
	if entryType == "" {
		entryType = "file" // default to file if not specified
	}

	entry := indexing.IndexEntry{
		RelativePath: relPath,
		AbsolutePath: absPath,
		Name:         payload.Name,
		Size:         payload.Size,
		ModTime:      time.Unix(modUnix, 0),
		Type:         entryType,
		Hidden:       payload.Hidden,
		Inode:        payload.Inode,
	}

	if err := storage.UpsertEntryWithSizeUpdate(ctx, d.db, indexID, entry); err != nil {
		http.Error(w, fmt.Sprintf("upsert failed: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, api.OperationResponse{Status: "ok"})
}

func (d *daemon) handleDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "use DELETE", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}

	// Reject path traversal attempts
	if !indexing.ValidateRelativePath(path) {
		http.Error(w, "invalid path: path traversal not allowed", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	lock, err := tryAcquireOperationLock(d.configSnapshot().DBPath)
	if err != nil {
		if errors.Is(err, errOperationAlreadyRunning) {
			http.Error(w, "indexer already running", http.StatusConflict)
			return
		}
		http.Error(w, fmt.Sprintf("operation lock failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer func() {
		if closeErr := lock.Close(); closeErr != nil {
			slog.Warn("failed to release operation lock", "err", closeErr)
		}
	}()

	indexID, err := d.store.LatestIndexID(ctx)
	if err != nil {
		http.Error(w, fmt.Sprintf("no index present: %v", err), http.StatusBadRequest)
		return
	}

	relPath := indexing.NormalizeIndexPath(path)
	if relPath == "/" {
		http.Error(w, "path must not be root; use /prune or /reindex to clear the index", http.StatusBadRequest)
		return
	}
	if err := storage.DeletePathRecursive(ctx, d.db, indexID, relPath); err != nil {
		http.Error(w, fmt.Sprintf("delete failed: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, api.OperationResponse{Status: "ok"})
}

func (d *daemon) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, d.savedConfigSnapshot())
	case http.MethodPut:
		d.handleConfigPut(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "use GET or PUT", http.StatusMethodNotAllowed)
	}
}

func (d *daemon) handleConfigPut(w http.ResponseWriter, r *http.Request) {
	if !requestFromUnixSocket(r) {
		http.Error(w, "config writes require the Unix socket", http.StatusForbidden)
		return
	}
	if d.running.Load() {
		http.Error(w, "indexer already running", http.StatusConflict)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, fmt.Sprintf("read body: %v", err), http.StatusBadRequest)
		return
	}
	patch, err := configfile.DecodePatchJSON(body)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid config JSON: %v", err), http.StatusBadRequest)
		return
	}
	next, err := configfile.ApplyPatch(d.savedConfigSnapshot(), patch)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	configPath := d.configSnapshot().ConfigPath
	if saveErr := configfile.Save(configPath, next); saveErr != nil {
		http.Error(w, fmt.Sprintf("write config file: %v", saveErr), http.StatusInternalServerError)
		return
	}
	restartRequired, err := d.applySavedConfig(next)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if restartRequired {
		w.Header().Set(api.RestartRequiredHeader, "true")
	}
	writeJSON(w, next)
}

func writeJSON(w http.ResponseWriter, v any) {
	writeJSONStatus(w, http.StatusOK, v)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("failed to encode JSON response", "err", err)
	}
}

// queryInt parses an integer query parameter with default and minimum value.
func queryInt(q string, def int, min int) int {
	if q == "" {
		return def
	}
	v, err := strconv.Atoi(q)
	if err != nil || v < min {
		return def
	}
	return v
}

// queryBoundedInt parses an integer query parameter and clamps it to max.
func queryBoundedInt(q string, def, min, max int) int {
	v := queryInt(q, def, min)
	if v > max {
		return max
	}
	return v
}

// queryPathOrRoot returns the path query parameter or "/" if empty.
func queryPathOrRoot(path string) (string, bool) {
	if path == "" {
		return "/", true
	}
	// Validate path to prevent traversal attempts
	if !indexing.ValidateRelativePath(path) {
		return "", false
	}
	return indexing.NormalizeIndexPath(path), true
}

// Minimal OpenAPI spec served at /openapi.json.
func serveOpenapi(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(openapiSpec)); err != nil {
		slog.Warn("failed to write OpenAPI response", "err", err)
	}
}

const openapiSpec = `{
  "openapi": "3.0.0",
  "info": { "title": "Indexer API", "version": "2.3.0", "description": "The optional TCP listener exposes only unauthenticated GET and HEAD routes. Mutations require UID 0 over the local Unix socket." },
  "paths": {
    "/index": { "post": { "summary": "Trigger full index", "responses": { "202": {"description": "Started"}, "409": {"description": "Already running"} } } },
    "/reindex": { "post": { "summary": "Reindex a specific path", "parameters": [{ "in": "query", "name": "path", "required": true, "schema": {"type": "string"}, "description": "Path to reindex (e.g., /home/user)" }], "responses": { "202": {"description": "Started"}, "400": {"description": "Path required"}, "409": {"description": "Already running"} } } },
    "/vacuum": { "post": { "summary": "Reclaim disk space (VACUUM)", "responses": { "202": {"description": "Started"}, "409": {"description": "Already running"} } } },
    "/prune": { "post": { "summary": "Prune old index records", "description": "Remove old index records and their entries to reclaim space", "parameters": [{ "in": "query", "name": "keep_latest", "schema": {"type": "integer", "default": 1}, "description": "Number of most recent indexes to keep (minimum 1)" }, { "in": "query", "name": "max_age_days", "schema": {"type": "integer", "default": 30}, "description": "Maximum age in days for indexes to keep" }], "responses": { "202": {"description": "Started"}, "409": {"description": "Already running"} } } },
    "/status": { "get": { "summary": "Get status or attach to active work SSE stream", "parameters": [{ "in": "query", "name": "stream", "schema": {"type": "boolean"}, "description": "Set true (or send Accept: text/event-stream) to attach to active work stream; falls back to JSON when idle" }], "responses": { "200": {"description": "Status JSON (includes fts_active: the actual FTS5 index state in the DB, vs. the desired fts_search in /config) or SSE stream", "content": {"application/json": {}, "text/event-stream": {}}} } } },
    "/search": { "get": { "summary": "Search entries (returns type: folder/file)", "description": "Always authentication-free when served by the optional read-only TCP listener.", "parameters": [{ "in": "query", "name": "q", "schema": {"type": "string", "maxLength": 256}, "description": "Search pattern; at most 10 |-separated terms are used, extra terms are ignored" }, { "in": "query", "name": "limit", "schema": {"type": "integer", "default": 100, "minimum": 1}, "description": "Maximum results to return; capped by configured search_max_limit (default 100)" }], "responses": { "200": {"description": "Results with type field indicating folder or file"}, "400": {"description": "Query too long"} } } },
    "/subfolders": { "get": { "summary": "Get direct subfolders with sizes", "parameters": [{ "in": "query", "name": "path", "schema": {"type": "string"}, "description": "Parent path (defaults to /)" }], "responses": { "200": {"description": "Array of direct subfolders with their sizes"} } } },
    "/dirsize": { "get": { "summary": "Directory size", "parameters": [{ "in": "query", "name": "path", "schema": {"type": "string"} }], "responses": { "200": {"description": "Size"} } } },
    "/entrycount": { "get": { "summary": "Count files and directories at and under a path (recursive, includes the path itself)", "description": "Returns counts of all entries with relative_path equal to or under the given path. The path itself is included: for path=/foo, /foo counts in dirs; for path=/, the root / entry counts in dirs. Because indexes.num_dirs excludes the root directory, dirs from this endpoint at path=/ will be indexes.num_dirs + 1.", "parameters": [{ "in": "query", "name": "path", "schema": {"type": "string"}, "description": "Path to count (defaults to /)" }], "responses": { "200": {"description": "{path, files, dirs}"}, "400": {"description": "Invalid path"} } } },
    "/entries": { "get": { "summary": "List entries (returns type: folder/file)", "parameters": [{ "in": "query", "name": "path", "schema": {"type": "string"} }, { "in": "query", "name": "recursive", "schema": {"type": "boolean"} }, { "in": "query", "name": "limit", "schema": {"type": "integer", "default": 200, "minimum": 1}, "description": "Maximum entries to return; capped by configured entries_max_limit (default 200)" }, { "in": "query", "name": "offset", "schema": {"type": "integer", "minimum": 0} }, { "in": "query", "name": "after", "schema": {"type": "string"}, "description": "Keyset cursor for recursive listings: return entries whose path sorts after this value (use the last path of the previous page); overrides offset and stays fast at any depth" }], "responses": { "200": {"description": "Entries with type field indicating folder or file"} } } },
    "/add": { "post": { "summary": "Upsert entry", "responses": { "200": {"description": "OK"} } } },
    "/delete": { "delete": { "summary": "Delete entry; for directories, deletes the whole subtree", "parameters": [{ "in": "query", "name": "path", "schema": {"type": "string"} }], "responses": { "200": {"description": "OK"}, "400": {"description": "Bad request (invalid path or root)"} } } },
    "/config": { "get": { "summary": "Get persisted daemon configuration", "responses": { "200": {"description": "Configuration JSON (see PUT schema)"} } }, "put": { "summary": "Update persisted daemon configuration over the Unix socket as root", "description": "Validates, persists atomically, applies runtime-safe fields immediately, and returns the normalized config. Omitted fields keep their current values. include_network_mounts, fts_search, fresh_index, include_hidden, keep_indexes and integrity_check take effect on the next scan without a restart; db_path and db_* settings need a daemon restart (response carries X-Indexer-Restart-Required: true). Listener configuration is owned by systemd and exposed by the LinuxIO bridge rather than this daemon endpoint.", "requestBody": { "content": { "application/json": { "schema": { "type": "object", "properties": {
      "index_path": {"type": "string", "description": "Filesystem root to index"},
      "index_name": {"type": "string"},
      "include_hidden": {"type": "boolean", "default": true, "description": "Index dotfiles and dot-directories (next scan)"},
      "include_network_mounts": {"type": "boolean", "default": false, "description": "Traverse NFS/CIFS/SMB mounts; false skips them and their entries drop out after the next fresh scan (next scan)"},
      "fresh_index": {"type": "boolean", "default": true, "description": "Rebuild in a new generation each scan, published atomically on completion (next scan)"},
      "fts_search": {"type": "boolean", "default": true, "description": "Maintain the FTS5 trigram index: millisecond substring search at the cost of ~2x slower full scans and ~15% more DB size. false drops the index and /search falls back to LIKE; re-enabling rebuilds it automatically. Ignored by binaries built without sqlite_fts5 (next scan)"},
      "keep_indexes": {"type": "integer", "default": 1, "description": "Index generations to retain automatically after a successful index; 0 retains all generations until /prune is run (next scan)"},
      "integrity_check": {"type": "string", "enum": ["full", "quick", "off"], "default": "full", "description": "SQLite database check before indexing an existing database. full performs PRAGMA integrity_check; quick performs the faster PRAGMA quick_check; off skips the check (next scan)"},
      "idle_timeout": {"type": "string", "description": "Exit after this idle duration so socket activation can start the daemon on demand; 0 disables"},
      "search_default_limit": {"type": "integer", "default": 100}, "search_max_limit": {"type": "integer", "default": 100},
      "entries_default_limit": {"type": "integer", "default": 200}, "entries_max_limit": {"type": "integer", "default": 200},
      "db_path": {"type": "string", "description": "Restart required"}
    } } } } }, "responses": { "200": {"description": "Normalized configuration JSON", "headers": {"X-Indexer-Restart-Required": {"schema": {"type": "string"}, "description": "Present (\"true\") when a persisted change only takes effect after the daemon restarts"}}}, "403": {"description": "Root Unix socket caller required"}, "409": {"description": "Indexer running"} } } }
  }
}`
