// Package httpapi serves the go-monitoring REST API.
package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mordilloSan/LinuxIO/backend/common/version"
	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	apimodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/api/model"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/utils"
)

const (
	maxHistoryLimit   = 1000
	apiRequestTimeout = 5 * time.Second
)

type MetricsReader interface {
	Path() string
	PluginHistory(context.Context, string, string, int64, int64, int) ([]store.HistoryRecord[json.RawMessage], error)
	HistoryEnabled(plugin string) bool
}

type retentionReader interface{ RetentionStrings() map[string]string }

type CurrentReader interface {
	CurrentPlugin(context.Context, string) (int64, json.RawMessage, error)
	SystemSummary(context.Context) (int64, system.Summary, error)
}

// CurrentBatchReader optionally provides a request-scoped read of multiple
// plugins, allowing samples to be shared within one collection pass. Registries
// retain the per-plugin CurrentReader path for readers without batching.
type CurrentBatchReader interface {
	CurrentPlugins(context.Context, []string) (int64, map[string]json.RawMessage, map[string]error)
}

type SmartRefresher interface {
	RefreshSmartNow(context.Context) error
}

type CommandExecutor interface {
	ExecuteCommand(context.Context, apimodel.CommandRequest) CommandResult
}

type CommandResult struct {
	Status   int
	Response apimodel.CommandResponse
}

type Options struct {
	Metrics              MetricsReader
	Current              CurrentReader
	SmartRefresher       SmartRefresher
	CommandExecutor      CommandExecutor
	DataDir              string
	Listeners            func() []apimodel.ListenerMeta
	SmartRefreshInterval func() string
	ConfigInfo           func() apimodel.ConfigMeta
	LastCollected        func() (time.Time, bool)
	Live                 func(context.Context) (monitoringapi.Live, error)
	RequestLogging       bool
}

type Server struct {
	metrics              MetricsReader
	current              CurrentReader
	smartRefresher       SmartRefresher
	commandExecutor      CommandExecutor
	dataDir              string
	listeners            func() []apimodel.ListenerMeta
	smartRefreshInterval func() string
	configInfo           func() apimodel.ConfigMeta
	lastCollected        func() (time.Time, bool)
	live                 func(context.Context) (monitoringapi.Live, error)
	requestLogging       bool
}

func NewServer(opts Options) *Server {
	current := opts.Current
	if current == nil {
		current = missingCurrentReader{}
	}
	return &Server{
		metrics:              opts.Metrics,
		current:              current,
		smartRefresher:       opts.SmartRefresher,
		commandExecutor:      opts.CommandExecutor,
		dataDir:              opts.DataDir,
		listeners:            opts.Listeners,
		smartRefreshInterval: opts.SmartRefreshInterval,
		configInfo:           opts.ConfigInfo,
		lastCollected:        opts.LastCollected,
		live:                 opts.Live,
		requestLogging:       opts.RequestLogging,
	}
}

// HandlerFor builds the mux for one listener. plugins is the per-listener
// plugin allowlist; nil serves every plugin plus the cross-plugin summary.
func (s *Server) HandlerFor(collectorInterval func() time.Duration, apis []string, plugins []string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth(collectorInterval))
	if hasAPI(apis, "metrics") {
		mux.HandleFunc("/api/v1/meta", s.handleMeta(collectorInterval))
		if plugins == nil {
			mux.HandleFunc("/api/v1/system/summary", s.handleSystemSummary)
		}
		mux.HandleFunc(monitoringapi.RouteLive, s.handleLive(plugins))
		// The refresh route forces smartctl scans as root and writes the store,
		// so only the command-capable control listener mounts it.
		refresher := s.smartRefresher
		if !hasAPI(apis, "commands") {
			refresher = nil
		}
		NewRegistry(s.current, s.metrics, refresher, plugins).Mount(mux, "/api/v1/")
	}
	if hasAPI(apis, "commands") {
		mux.HandleFunc("/api/v1/command", s.handleCommand)
	}
	if s.requestLogging {
		return logRequests(mux)
	}
	return mux
}

func hasAPI(apis []string, api string) bool {
	for _, value := range apis {
		if strings.EqualFold(strings.TrimSpace(value), api) {
			return true
		}
	}
	return false
}

func RequestLoggingEnabled() bool {
	value, exists := utils.GetEnv("HTTP_LOG")
	if !exists {
		value, exists = utils.GetEnv("REQUEST_LOG")
	}
	if !exists {
		return true
	}

	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		slog.Warn("Invalid HTTP_LOG value; defaulting to enabled", "value", value)
		return true
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.status != 0 {
		return
	}
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(data)
	r.bytes += n
	return n, err
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}

		slog.Info("HTTP request",
			"method", r.Method,
			"path", r.URL.RequestURI(),
			"status", rec.status,
			"bytes", rec.bytes,
			"duration", time.Since(start),
		)
	})
}

func (s *Server) handleHealth(collectorInterval func() time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		if s.lastCollected == nil {
			writeError(w, http.StatusServiceUnavailable, errors.New("collector state unavailable"))
			return
		}
		last, ok := s.lastCollected()
		if !ok {
			writeError(w, http.StatusServiceUnavailable, errors.New("no collector sample yet"))
			return
		}
		interval := time.Minute
		if collectorInterval != nil {
			if configured := collectorInterval(); configured > 0 {
				interval = configured
			}
		}
		age := time.Since(last)
		healthy := age <= 2*interval
		code := http.StatusOK
		if !healthy {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, map[string]any{
			"healthy":      healthy,
			"last_updated": last.UTC(),
			"age_seconds":  age.Seconds(),
		})
	}
}

func (s *Server) handleCommand(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w, http.MethodPost)
		return
	}
	if s.commandExecutor == nil {
		http.NotFound(w, r)
		return
	}
	var req apimodel.CommandRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, apimodel.CommandResponse{
			OK: false,
			Error: &apimodel.CommandError{
				Code:    "invalid_json",
				Message: err.Error(),
			},
		})
		return
	}
	result := s.commandExecutor.ExecuteCommand(r.Context(), req)
	status := result.Status
	if status == 0 {
		status = http.StatusOK
	}
	writeJSON(w, status, result.Response)
}

func (s *Server) handleMeta(collectorInterval func() time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		interval := time.Duration(0)
		if collectorInterval != nil {
			interval = collectorInterval()
		}
		writeJSON(w, http.StatusOK, s.metaResponse(interval))
	}
}

func (s *Server) handleSystemSummary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w, http.MethodGet)
		return
	}

	ctx, cancel := requestContext(r)
	defer cancel()

	capturedAt, summary, err := s.current.SystemSummary(ctx)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, apimodel.SystemSummaryResponse{
		CapturedAt: capturedAt,
		Summary:    summary,
	})
}

// liveSectionPlugins maps live payload sections to the plugin allowlist a
// configured listener may restrict.
var liveSectionPlugins = map[string][]string{
	"cpu":        {store.PluginCPU},
	"memory":     {store.PluginMem, store.PluginSwap},
	"disks":      {store.PluginDiskIO},
	"interfaces": {store.PluginNetwork},
	"containers": {store.PluginContainers, store.PluginContainerTelemetry},
}

func (s *Server) handleLive(plugins []string) http.HandlerFunc {
	// Normalize the allowlist the way NewRegistry does, so a listener naming
	// "CPU" both mounts the cpu routes and keeps the cpu live section.
	allowed := map[string]bool{}
	for _, name := range plugins {
		allowed[strings.ToLower(strings.TrimSpace(name))] = true
	}
	permitted := func(section string) bool {
		if plugins == nil {
			return true
		}
		for _, plugin := range liveSectionPlugins[section] {
			if allowed[plugin] {
				return true
			}
		}
		return false
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeMethodNotAllowed(w, http.MethodGet)
			return
		}
		if s.live == nil {
			http.NotFound(w, r)
			return
		}
		ctx, cancel := requestContext(r)
		defer cancel()
		live, err := s.live(ctx)
		if err != nil {
			writeStoreError(w, err)
			return
		}
		redactLiveSections(&live, permitted)
		writeJSON(w, http.StatusOK, live)
	}
}

// redactLiveSections empties the live payload sections a listener's plugin
// allowlist does not cover, keeping the response shape byte-stable.
func redactLiveSections(live *monitoringapi.Live, permitted func(string) bool) {
	if !permitted("cpu") {
		live.CPU = monitoringapi.LiveCPU{PerCorePercent: []float64{}}
	}
	if !permitted("memory") {
		live.Memory = monitoringapi.LiveMemory{}
	}
	if !permitted("disks") {
		live.Disks, live.DiskIO = map[string]monitoringapi.LiveDiskRates{}, monitoringapi.LiveDiskRates{}
	}
	if !permitted("interfaces") {
		live.Interfaces = map[string]monitoringapi.LiveInterface{}
	}
	if !permitted("containers") {
		live.Containers = monitoringapi.LiveContainers{Items: []monitoringapi.LiveContainer{}}
	}
}

type missingCurrentReader struct{}

func (missingCurrentReader) CurrentPlugin(context.Context, string) (int64, json.RawMessage, error) {
	return 0, nil, errors.New("current provider not configured")
}

func (missingCurrentReader) SystemSummary(context.Context) (int64, system.Summary, error) {
	return 0, system.Summary{}, errors.New("current provider not configured")
}

func (s *Server) metaResponse(collectorInterval time.Duration) apimodel.MetaResponse {
	smartRefreshInterval := ""
	if s.smartRefreshInterval != nil {
		smartRefreshInterval = s.smartRefreshInterval()
	}
	var listeners []apimodel.ListenerMeta
	if s.listeners != nil {
		listeners = s.listeners()
	}
	configInfo := apimodel.ConfigMeta{}
	if s.configInfo != nil {
		configInfo = s.configInfo()
	}
	interval := collectorInterval.String()
	if configInfo.CollectorInterval != "" {
		interval = configInfo.CollectorInterval
	}
	retention := store.RetentionStrings()
	if reader, ok := s.metrics.(retentionReader); ok {
		retention = reader.RetentionStrings()
	}
	return apimodel.MetaResponse{
		Version:              version.Version,
		DataDir:              s.dataDir,
		DBPath:               s.metrics.Path(),
		Listeners:            listeners,
		CollectorInterval:    interval,
		SmartRefreshInterval: smartRefreshInterval,
		Config:               configInfo,
		Retention:            retention,
	}
}

func parseHistoryQuery(w http.ResponseWriter, r *http.Request) (resolution string, from int64, to int64, limit int, ok bool) {
	query := r.URL.Query()
	resolution = query.Get("resolution")
	if !store.ValidResolution(resolution) {
		writeError(w, http.StatusBadRequest, errors.New("invalid resolution"))
		return "", 0, 0, 0, false
	}

	var err error
	from = 0
	if raw := query.Get("from"); raw != "" {
		from, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("invalid from"))
			return "", 0, 0, 0, false
		}
	}

	to = time.Now().UTC().UnixMilli()
	if raw := query.Get("to"); raw != "" {
		to, err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("invalid to"))
			return "", 0, 0, 0, false
		}
	}
	if from > to {
		writeError(w, http.StatusBadRequest, errors.New("from must be <= to"))
		return "", 0, 0, 0, false
	}

	limit = 100
	if raw := query.Get("limit"); raw != "" {
		limit, err = strconv.Atoi(raw)
		if err != nil || limit <= 0 || limit > maxHistoryLimit {
			writeError(w, http.StatusBadRequest, errors.New("invalid limit"))
			return "", 0, 0, 0, false
		}
	}
	return resolution, from, to, limit, true
}

func writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeErrorMessage(w, http.StatusNotFound, "not found")
	case errors.Is(err, context.DeadlineExceeded):
		writeErrorMessage(w, http.StatusGatewayTimeout, "request timed out")
	case errors.Is(err, context.Canceled):
		writeErrorMessage(w, http.StatusRequestTimeout, "request canceled")
	default:
		writeInternalError(w, err)
	}
}

func writeMethodNotAllowed(w http.ResponseWriter, method string) {
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, errors.New("method not allowed"))
}

func requestContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), apiRequestTimeout)
}

func publicErrorMessage(err error) string {
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return "not found"
	case errors.Is(err, context.DeadlineExceeded):
		return "request timed out"
	case errors.Is(err, context.Canceled):
		return "request canceled"
	default:
		return "internal server error"
	}
}

func writeInternalError(w http.ResponseWriter, err error) {
	slog.Error("Internal API error", "err", err)
	writeErrorMessage(w, http.StatusInternalServerError, "internal server error")
}

func writeError(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, apimodel.ErrorResponse{Error: err.Error()})
}

func writeErrorMessage(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, apimodel.ErrorResponse{Error: message})
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}
