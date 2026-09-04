package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	monitoringapi "github.com/mordilloSan/LinuxIO/backend/monitoring/api"
	apimodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/api/model"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/smart"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/domain/system"
	storepkg "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/store/storetest"
)

type fakeSmartRefresher struct {
	current *fakeCurrentReader
}

func (r fakeSmartRefresher) RefreshSmartNow(context.Context) error {
	r.current.plugins[storepkg.PluginSmart] = json.RawMessage(`[]`)
	return nil
}

type fakeCommandExecutor struct{}

func (fakeCommandExecutor) ExecuteCommand(_ context.Context, req apimodel.CommandRequest) CommandResult {
	return CommandResult{
		Status: http.StatusOK,
		Response: apimodel.CommandResponse{
			OK:        true,
			Command:   req.Command,
			RequestID: req.RequestID,
			Data:      map[string]string{"handled": "yes"},
		},
	}
}

type fakeCurrentReader struct {
	capturedAt   int64
	plugins      map[string]json.RawMessage
	pluginErrors map[string]error
	calls        map[string]int
	summary      system.Summary
	summaryErr   error
}

type batchCurrentReader struct {
	base         fakeCurrentReader
	batchCalls   int
	currentCalls int
	batchPlugins []string
}

func (r *batchCurrentReader) CurrentPlugin(ctx context.Context, plugin string) (int64, json.RawMessage, error) {
	r.currentCalls++
	return r.base.CurrentPlugin(ctx, plugin)
}

func (r *batchCurrentReader) CurrentPlugins(_ context.Context, plugins []string) (int64, map[string]json.RawMessage, map[string]error) {
	r.batchCalls++
	r.batchPlugins = append([]string(nil), plugins...)
	payloads := make(map[string]json.RawMessage, len(plugins))
	errorsByPlugin := make(map[string]error)
	for _, plugin := range plugins {
		if err := r.base.pluginErrors[plugin]; err != nil {
			errorsByPlugin[plugin] = err
			continue
		}
		if payload, ok := r.base.plugins[plugin]; ok {
			payloads[plugin] = payload
		}
	}
	return r.base.capturedAt, payloads, errorsByPlugin
}

func (r *batchCurrentReader) SystemSummary(ctx context.Context) (int64, system.Summary, error) {
	return r.base.SystemSummary(ctx)
}

func (r fakeCurrentReader) CurrentPlugin(_ context.Context, plugin string) (int64, json.RawMessage, error) {
	if r.calls != nil {
		r.calls[plugin]++
	}
	if err := r.pluginErrors[plugin]; err != nil {
		return 0, nil, err
	}
	if raw, ok := r.plugins[plugin]; ok {
		return r.capturedAt, raw, nil
	}
	return r.capturedAt, json.RawMessage(`{}`), nil
}

func (r fakeCurrentReader) SystemSummary(_ context.Context) (int64, system.Summary, error) {
	if r.summaryErr != nil {
		return 0, system.Summary{}, r.summaryErr
	}
	return r.capturedAt, r.summary, nil
}

func newHTTPTestServer(t *testing.T) *Server {
	t.Helper()

	tmpDir := t.TempDir()
	store, err := storepkg.OpenStore(tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	capturedAt := time.Now().UTC().UnixMilli()
	data := storetest.SampleCombinedData(55)
	require.NoError(t, store.WriteSnapshot(capturedAt, data))

	plugins := make(map[string]json.RawMessage)
	for name, payload := range storepkg.SnapshotPluginPayloads(data) {
		raw, marshalErr := json.Marshal(payload)
		require.NoError(t, marshalErr)
		plugins[name] = raw
	}
	plugins[storepkg.PluginProcesses], err = json.Marshal(map[string]any{"count": data.ProcessCount, "items": data.Processes})
	require.NoError(t, err)
	plugins[storepkg.PluginPrograms], err = json.Marshal(data.Programs)
	require.NoError(t, err)
	plugins[storepkg.PluginSmart], err = json.Marshal([]storepkg.SmartDeviceRecord{{
		ID:  "/dev/sdb",
		Key: "/dev/sdb",
		Data: smart.SmartData{
			ModelName:   "disk-b",
			DiskName:    "/dev/sdb",
			SmartStatus: "passed",
		},
	}})
	require.NoError(t, err)
	current := &fakeCurrentReader{
		capturedAt: capturedAt,
		plugins:    plugins,
		summary:    system.NewSummary(data),
	}

	return NewServer(Options{
		Metrics:              store,
		Current:              current,
		SmartRefresher:       fakeSmartRefresher{current: current},
		DataDir:              tmpDir,
		SmartRefreshInterval: func() string { return "" },
		LastCollected:        func() (time.Time, bool) { return time.UnixMilli(capturedAt), true },
		RequestLogging:       true,
	})
}

func TestHandlerForSeparatesMetricsAndCommandAPIs(t *testing.T) {
	server := NewServer(Options{CommandExecutor: fakeCommandExecutor{}})

	commandHandler := server.HandlerFor(func() time.Duration { return time.Minute }, []string{"commands"}, nil)
	commandReq := httptest.NewRequest(http.MethodPost, "/api/v1/command", strings.NewReader(`{"command":"status.get","request_id":"test"}`))
	commandRec := httptest.NewRecorder()
	commandHandler.ServeHTTP(commandRec, commandReq)
	require.Equal(t, http.StatusOK, commandRec.Code)
	assert.Contains(t, commandRec.Body.String(), `"command":"status.get"`)
	assert.Contains(t, commandRec.Body.String(), `"request_id":"test"`)

	metaReq := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	metaRec := httptest.NewRecorder()
	commandHandler.ServeHTTP(metaRec, metaReq)
	assert.Equal(t, http.StatusNotFound, metaRec.Code)

	metricsHandler := server.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)
	blockedCommandReq := httptest.NewRequest(http.MethodPost, "/api/v1/command", strings.NewReader(`{"command":"status.get"}`))
	blockedCommandRec := httptest.NewRecorder()
	metricsHandler.ServeHTTP(blockedCommandRec, blockedCommandReq)
	assert.Equal(t, http.StatusMethodNotAllowed, blockedCommandRec.Code)
	assert.NotContains(t, blockedCommandRec.Body.String(), `"ok"`)
}

func TestCurrentRoutesUseCurrentReaderAndHistoryUsesStore(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storepkg.OpenStore(tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	capturedAt := time.Now().UTC().UnixMilli()
	require.NoError(t, store.WriteSnapshot(capturedAt, storetest.SampleCombinedData(55)))

	current := &fakeCurrentReader{
		capturedAt: 123,
		plugins: map[string]json.RawMessage{
			storepkg.PluginCPU: json.RawMessage(`{"cpu_percent":99}`),
		},
		summary: system.Summary{Hostname: "live-host", CPUPercent: 99},
	}
	server := NewServer(Options{
		Metrics:        store,
		Current:        current,
		SmartRefresher: fakeSmartRefresher{current: current},
		DataDir:        tmpDir,
	})
	handler := server.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)

	cpuReq := httptest.NewRequest(http.MethodGet, "/api/v1/cpu", nil)
	cpuRec := httptest.NewRecorder()
	handler.ServeHTTP(cpuRec, cpuReq)
	require.Equal(t, http.StatusOK, cpuRec.Code)
	assert.Contains(t, cpuRec.Body.String(), `"cpu_percent":99`)
	assert.NotContains(t, cpuRec.Body.String(), `"cpu_percent":55`)

	summaryReq := httptest.NewRequest(http.MethodGet, "/api/v1/system/summary", nil)
	summaryRec := httptest.NewRecorder()
	handler.ServeHTTP(summaryRec, summaryReq)
	require.Equal(t, http.StatusOK, summaryRec.Code)
	assert.Contains(t, summaryRec.Body.String(), `"hostname":"live-host"`)

	historyReq := httptest.NewRequest(http.MethodGet, "/api/v1/cpu/history?resolution=1m&from=0&to=9999999999999&limit=10", nil)
	historyRec := httptest.NewRecorder()
	handler.ServeHTTP(historyRec, historyReq)
	require.Equal(t, http.StatusOK, historyRec.Code)
	assert.Contains(t, historyRec.Body.String(), `"cpu_percent":55`)
}

func TestMetaReportsActiveStoreRetention(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storepkg.OpenStore(tmpDir, storepkg.Options{
		HistoryPlugins:   storepkg.DefaultHistoryPluginNames(),
		HistoryRetention: 14 * 24 * time.Hour,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	server := NewServer(Options{Metrics: store, Current: fakeCurrentReader{}, DataDir: tmpDir})
	handler := server.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"retention":{"1m":"336h0m0s"}`)
}

func TestHistoryRoutesReflectUpdatedHistoryAllowlist(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storepkg.OpenStore(tmpDir, storepkg.Options{HistoryPlugins: []string{storepkg.PluginCPU}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	capturedAt := time.Now().UTC().UnixMilli()
	require.NoError(t, store.WriteSnapshot(capturedAt, storetest.SampleCombinedData(55)))

	server := NewServer(Options{
		Metrics: store,
		Current: fakeCurrentReader{},
		DataDir: tmpDir,
	})
	handler := server.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)

	pluginsReq := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	pluginsRec := httptest.NewRecorder()
	handler.ServeHTTP(pluginsRec, pluginsReq)
	require.Equal(t, http.StatusOK, pluginsRec.Code)
	assert.Contains(t, pluginsRec.Body.String(), `"name":"swap","has_history":false`)

	disabledReq := httptest.NewRequest(http.MethodGet, "/api/v1/swap/history?resolution=1m", nil)
	disabledRec := httptest.NewRecorder()
	handler.ServeHTTP(disabledRec, disabledReq)
	require.Equal(t, http.StatusNotFound, disabledRec.Code)

	store.SetHistoryPlugins([]string{storepkg.PluginCPU, storepkg.PluginSwap})
	require.NoError(t, store.WriteSnapshot(capturedAt+1, storetest.SampleCombinedData(60)))

	pluginsReq = httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	pluginsRec = httptest.NewRecorder()
	handler.ServeHTTP(pluginsRec, pluginsReq)
	require.Equal(t, http.StatusOK, pluginsRec.Code)
	assert.Contains(t, pluginsRec.Body.String(), `"name":"swap","has_history":true`)

	enabledReq := httptest.NewRequest(http.MethodGet, "/api/v1/swap/history?resolution=1m&from=0&to=9999999999999&limit=10", nil)
	enabledRec := httptest.NewRecorder()
	handler.ServeHTTP(enabledRec, enabledReq)
	require.Equal(t, http.StatusOK, enabledRec.Code)
	assert.Contains(t, enabledRec.Body.String(), `"resolution":"1m"`)
}

func TestHistoryQueryValidationAndLimit(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storepkg.OpenStore(tmpDir, storepkg.Options{HistoryPlugins: []string{storepkg.PluginCPU}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	start := time.Now().UTC().Truncate(time.Millisecond)
	for i := range 3 {
		require.NoError(t, store.WriteSnapshot(start.Add(time.Duration(i)*time.Millisecond).UnixMilli(), storetest.SampleCombinedData(float64(i+1))))
	}

	server := NewServer(Options{Metrics: store, Current: fakeCurrentReader{}, DataDir: tmpDir})
	handler := server.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)

	tests := []struct {
		name   string
		method string
		path   string
		status int
		body   string
	}{
		{name: "negative limit", method: http.MethodGet, path: "/api/v1/cpu/history?resolution=1m&limit=-1", status: http.StatusBadRequest, body: `"error":"invalid limit"`},
		{name: "oversized limit", method: http.MethodGet, path: "/api/v1/cpu/history?resolution=1m&limit=1001", status: http.StatusBadRequest, body: `"error":"invalid limit"`},
		{name: "from after to", method: http.MethodGet, path: "/api/v1/cpu/history?resolution=1m&from=20&to=10", status: http.StatusBadRequest, body: `"error":"from must be \u003c= to"`},
		{name: "unknown plugin", method: http.MethodGet, path: "/api/v1/nope/history?resolution=1m", status: http.StatusNotFound, body: "404 page not found"},
		{name: "method not allowed", method: http.MethodPost, path: "/api/v1/cpu/history?resolution=1m", status: http.StatusMethodNotAllowed, body: `"error":"method not allowed"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assert.Equal(t, tt.status, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.body)
		})
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cpu/history?resolution=1m&from=0&to=9999999999999&limit=2", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var response rawHistoryResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.Len(t, response.Items, 2)
}

func TestEmptyStoreCurrentRouteReturnsNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storepkg.OpenStore(tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	server := NewServer(Options{
		Metrics: store,
		Current: fakeCurrentReader{
			pluginErrors: map[string]error{storepkg.PluginCPU: sql.ErrNoRows},
			plugins:      map[string]json.RawMessage{storepkg.PluginSmart: json.RawMessage(`[]`)},
		},
		DataDir: tmpDir,
	})
	handler := server.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cpu", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `"error":"not found"`)

	// smart serves an empty item list even before the first snapshot, so the
	// all route responds with partial data plus per-plugin errors.
	allReq := httptest.NewRequest(http.MethodGet, "/api/v1/all", nil)
	allRec := httptest.NewRecorder()
	handler.ServeHTTP(allRec, allReq)

	assert.Equal(t, http.StatusOK, allRec.Code)
	assert.Contains(t, allRec.Body.String(), `"smart"`)
	assert.Contains(t, allRec.Body.String(), `"cpu":"not found"`)
}

func TestAllRouteReturnsNotFoundWhenEveryPluginHasNoRows(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storepkg.OpenStore(tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	pluginErrors := map[string]error{}
	for _, name := range storepkg.PluginNames() {
		if !storepkg.IsLiveOnlyPlugin(name) {
			pluginErrors[name] = sql.ErrNoRows
		}
	}
	calls := map[string]int{}
	server := NewServer(Options{
		Metrics: store,
		Current: fakeCurrentReader{pluginErrors: pluginErrors, calls: calls},
		DataDir: tmpDir,
	})
	handler := server.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/all", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `"error":"not found"`)
	assert.Zero(t, calls[storepkg.PluginProcesses])
	assert.Zero(t, calls[storepkg.PluginPrograms])
}

func TestAllRouteReturnsInternalErrorWhenEveryPluginFails(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storepkg.OpenStore(tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	pluginErrors := map[string]error{}
	for _, name := range storepkg.PluginNames() {
		pluginErrors[name] = errors.New("private failure details")
	}
	server := NewServer(Options{
		Metrics: store,
		Current: fakeCurrentReader{pluginErrors: pluginErrors},
		DataDir: tmpDir,
	})
	handler := server.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/all", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), `"error":"internal server error"`)
	assert.NotContains(t, rec.Body.String(), "private failure")
}

func TestInternalErrorsUseGenericBodies(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storepkg.OpenStore(tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	server := NewServer(Options{
		Metrics: store,
		Current: fakeCurrentReader{
			pluginErrors: map[string]error{
				storepkg.PluginCPU: errors.New("secret database path leaked"),
			},
		},
		DataDir: tmpDir,
	})
	handler := server.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/cpu", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), `"error":"internal server error"`)
	assert.NotContains(t, rec.Body.String(), "secret")
}

func TestAllRouteReturnsPartialDataAndPluginErrors(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storepkg.OpenStore(tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	server := NewServer(Options{
		Metrics: store,
		Current: fakeCurrentReader{
			capturedAt: 123,
			plugins: map[string]json.RawMessage{
				storepkg.PluginMem: json.RawMessage(`{"memory_gb":16}`),
			},
			pluginErrors: map[string]error{
				storepkg.PluginCPU: errors.New("private failure details"),
			},
		},
		DataDir: tmpDir,
	})
	handler := server.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/all", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, `"mem"`)
	assert.Contains(t, body, `"errors"`)
	assert.Contains(t, body, `"cpu":"internal server error"`)
	assert.NotContains(t, body, "private failure")
}

func TestAllRouteBatchesCurrentReads(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := storepkg.OpenStore(tmpDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	current := &batchCurrentReader{base: fakeCurrentReader{
		capturedAt: 123,
		plugins: map[string]json.RawMessage{
			storepkg.PluginMem: json.RawMessage(`{"memory_gb":16}`),
		},
		pluginErrors: map[string]error{
			storepkg.PluginCPU: errors.New("private failure details"),
		},
	}}
	server := NewServer(Options{Metrics: store, Current: current, DataDir: tmpDir})
	handler := server.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/all", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 1, current.batchCalls)
	assert.Zero(t, current.currentCalls)
	assert.NotContains(t, current.batchPlugins, storepkg.PluginProcesses)
	assert.NotContains(t, current.batchPlugins, storepkg.PluginPrograms)
	body := rec.Body.String()
	assert.Contains(t, body, `"mem":{"captured_at":123,"data":{"memory_gb":16}}`)
	assert.Contains(t, body, `"cpu":"internal server error"`)
	assert.NotContains(t, body, "private failure")
}

func TestHTTPRoutes(t *testing.T) {
	server := newHTTPTestServer(t)
	handler := server.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)

	tests := []struct {
		name   string
		method string
		path   string
		status int
		body   string
	}{
		{name: "health", method: http.MethodGet, path: "/healthz", status: http.StatusOK, body: `"healthy":true`},
		{name: "meta", method: http.MethodGet, path: "/api/v1/meta", status: http.StatusOK, body: `"collector_interval":"1m0s"`},
		{name: "system summary", method: http.MethodGet, path: "/api/v1/system/summary", status: http.StatusOK, body: `"hostname":"host-a"`},
		{name: "plugins", method: http.MethodGet, path: "/api/v1/plugins", status: http.StatusOK, body: `"name":"cpu"`},
		{name: "plugins container telemetry history", method: http.MethodGet, path: "/api/v1/plugins", status: http.StatusOK, body: `"name":"container_telemetry","has_history":true`},
		{name: "plugins process live only", method: http.MethodGet, path: "/api/v1/plugins", status: http.StatusOK, body: `"name":"processes","has_history":false`},
		{name: "plugins program live only", method: http.MethodGet, path: "/api/v1/plugins", status: http.StatusOK, body: `"name":"programs","has_history":false`},
		{name: "all", method: http.MethodGet, path: "/api/v1/all", status: http.StatusOK, body: `"containers"`},
		{name: "cpu", method: http.MethodGet, path: "/api/v1/cpu", status: http.StatusOK, body: `"cpu_percent":55`},
		{name: "mem", method: http.MethodGet, path: "/api/v1/mem", status: http.StatusOK, body: `"memory_available_gb":9.4`},
		{name: "swap", method: http.MethodGet, path: "/api/v1/swap", status: http.StatusOK, body: `"swap_gb":5.5`},
		{name: "load", method: http.MethodGet, path: "/api/v1/load", status: http.StatusOK, body: `"load_average":[1,2,3]`},
		{name: "diskio", method: http.MethodGet, path: "/api/v1/diskio", status: http.StatusOK, body: `"disk_total_gb":100`},
		{name: "fs", method: http.MethodGet, path: "/api/v1/fs", status: http.StatusOK, body: `"data":{}`},
		{name: "network", method: http.MethodGet, path: "/api/v1/network", status: http.StatusOK, body: `"bandwidth_bytes_per_second":[1000,2000]`},
		{name: "gpu", method: http.MethodGet, path: "/api/v1/gpu", status: http.StatusOK, body: `"data":{}`},
		{name: "sensors", method: http.MethodGet, path: "/api/v1/sensors", status: http.StatusOK, body: `"data":{}`},
		{name: "containers", method: http.MethodGet, path: "/api/v1/containers", status: http.StatusOK, body: `"image":"nginx"`},
		{name: "container telemetry", method: http.MethodGet, path: "/api/v1/container_telemetry", status: http.StatusOK, body: `"disk_read_bytes_per_second":400`},
		{name: "processes", method: http.MethodGet, path: "/api/v1/processes", status: http.StatusOK, body: `"total":2`},
		{name: "programs", method: http.MethodGet, path: "/api/v1/programs", status: http.StatusOK, body: `"name":"nginx"`},
		{name: "connections", method: http.MethodGet, path: "/api/v1/connections", status: http.StatusOK, body: `"nf_conntrack_count":7`},
		{name: "irq", method: http.MethodGet, path: "/api/v1/irq", status: http.StatusOK, body: `"irq":"0"`},
		{name: "smart", method: http.MethodGet, path: "/api/v1/smart", status: http.StatusOK, body: `"disk_name":"/dev/sdb"`},
		{name: "cpu history", method: http.MethodGet, path: "/api/v1/cpu/history?resolution=1m&from=0&to=9999999999999&limit=10", status: http.StatusOK, body: `"resolution":"1m"`},
		{name: "mem history", method: http.MethodGet, path: "/api/v1/mem/history?resolution=1m&from=0&to=9999999999999&limit=10", status: http.StatusOK, body: `"memory_cached_gb":7.8`},
		{name: "swap history", method: http.MethodGet, path: "/api/v1/swap/history?resolution=1m&from=0&to=9999999999999&limit=10", status: http.StatusOK, body: `"swap_used_gb":2.75`},
		{name: "container history", method: http.MethodGet, path: "/api/v1/containers/history?resolution=1m&from=0&to=9999999999999&limit=10", status: http.StatusOK, body: `"cpu_percent":27.5`},
		{name: "container telemetry history", method: http.MethodGet, path: "/api/v1/container_telemetry/history?resolution=1m&from=0&to=9999999999999&limit=10", status: http.StatusOK, body: `"disk_write_bytes_per_second":200`},
		{name: "process history disabled", method: http.MethodGet, path: "/api/v1/processes/history?resolution=1m", status: http.StatusNotFound, body: "404 page not found"},
		{name: "disabled history", method: http.MethodGet, path: "/api/v1/fs/history?resolution=1m", status: http.StatusNotFound, body: "404 page not found"},
		{name: "invalid history", method: http.MethodGet, path: "/api/v1/cpu/history?resolution=bad", status: http.StatusBadRequest, body: `"error":"invalid resolution"`},
		{name: "smart refresh is not mounted on metrics listeners", method: http.MethodPost, path: "/api/v1/smart/refresh", status: http.StatusNotFound, body: "404 page not found"},
		{name: "old summary removed", method: http.MethodGet, path: "/api/v1/summary", status: http.StatusNotFound, body: "404 page not found"},
		{name: "old system history removed", method: http.MethodGet, path: "/api/v1/history/system?resolution=1m", status: http.StatusNotFound, body: "404 page not found"},
		{name: "old processlist removed", method: http.MethodGet, path: "/api/v1/processlist", status: http.StatusNotFound, body: "404 page not found"},
		{name: "old processcount removed", method: http.MethodGet, path: "/api/v1/processcount", status: http.StatusNotFound, body: "404 page not found"},
		{name: "old programlist removed", method: http.MethodGet, path: "/api/v1/programlist", status: http.StatusNotFound, body: "404 page not found"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			assert.Equal(t, tt.status, rec.Code)
			assert.Contains(t, rec.Body.String(), tt.body)
			if strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
				assert.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			}
		})
	}

	pluginsReq := httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil)
	pluginsRec := httptest.NewRecorder()
	handler.ServeHTTP(pluginsRec, pluginsReq)
	require.Equal(t, http.StatusOK, pluginsRec.Code)
	assert.Contains(t, pluginsRec.Body.String(), `"routes":["GET /api/v1/processes"]`)
	assert.Contains(t, pluginsRec.Body.String(), `"routes":["GET /api/v1/programs"]`)

	allReq := httptest.NewRequest(http.MethodGet, "/api/v1/all", nil)
	allRec := httptest.NewRecorder()
	handler.ServeHTTP(allRec, allReq)
	require.Equal(t, http.StatusOK, allRec.Code)
	assert.NotContains(t, allRec.Body.String(), `"processes"`)
	assert.NotContains(t, allRec.Body.String(), `"programs"`)
}

func TestRequestLoggingEnabled(t *testing.T) {
	tests := []struct {
		name  string
		env   string
		set   bool
		want  bool
		alias bool
	}{
		{name: "default", want: true},
		{name: "true", env: "true", set: true, want: true},
		{name: "one", env: "1", set: true, want: true},
		{name: "false", env: "false", set: true, want: false},
		{name: "zero", env: "0", set: true, want: false},
		{name: "alias false", env: "off", set: true, want: false, alias: true},
		{name: "invalid defaults true", env: "sometimes", set: true, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.set {
				if tt.alias {
					t.Setenv("REQUEST_LOG", tt.env)
				} else {
					t.Setenv("HTTP_LOG", tt.env)
				}
			}

			assert.Equal(t, tt.want, RequestLoggingEnabled())
		})
	}
}

func TestLogRequestsWritesRequestLog(t *testing.T) {
	var buf bytes.Buffer
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	handler := logRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/smart/refresh?force=true", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	logLine := buf.String()
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, logLine, "msg=\"HTTP request\"")
	assert.Contains(t, logLine, "method=POST")
	assert.Contains(t, logLine, "path=\"/api/v1/smart/refresh?force=true\"")
	assert.Contains(t, logLine, "status=201")
	assert.Contains(t, logLine, "bytes=7")
	assert.NotContains(t, logLine, "remote=")
	assert.NotContains(t, logLine, "user_agent=")
}

func TestRoutesCanDisableRequestLogging(t *testing.T) {
	var buf bytes.Buffer
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	server := newHTTPTestServer(t)
	server.requestLogging = false
	handler := server.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, buf.String(), "HTTP request")
}

func TestHealthzUsesLastCollectedAge(t *testing.T) {
	last := time.Now().Add(-30 * time.Second)
	srv := NewServer(Options{
		LastCollected: func() (time.Time, bool) { return last, true },
	})
	handler := srv.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}

	stale := NewServer(Options{
		LastCollected: func() (time.Time, bool) { return time.Now().Add(-5 * time.Minute), true },
	})
	rec = httptest.NewRecorder()
	stale.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("stale status = %d, want 503", rec.Code)
	}

	never := NewServer(Options{LastCollected: func() (time.Time, bool) { return time.Time{}, false }})
	rec = httptest.NewRecorder()
	never.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("no-sample status = %d, want 503", rec.Code)
	}
}

func TestPluginAllowlistFiltersRoutes(t *testing.T) {
	srv := newHTTPTestServer(t)
	handler := srv.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, []string{"cpu"})

	for path, want := range map[string]int{
		"/api/v1/cpu":            http.StatusOK,
		"/api/v1/mem":            http.StatusNotFound,
		"/api/v1/system/summary": http.StatusNotFound,
		"/api/v1/plugins":        http.StatusOK,
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want {
			t.Fatalf("%s: status = %d, want %d", path, rec.Code, want)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil))
	if body := rec.Body.String(); strings.Contains(body, `"name":"mem"`) {
		t.Fatalf("plugins listing leaks mem: %s", body)
	}
}

func TestLiveRouteFiltersSectionsByAllowlist(t *testing.T) {
	tests := []struct {
		name           string
		plugins        []string
		wantInterfaces int
	}{
		{name: "lowercase", plugins: []string{"cpu"}},
		// NewRegistry lower-cases and trims the allowlist, so the live
		// sections must be matched the same way.
		{name: "mixed case", plugins: []string{"CPU"}},
		// daemon.Listeners turns a configured empty plugin list into nil, which
		// is the "all metrics plugins" allowlist.
		{name: "nil means all plugins", plugins: nil, wantInterfaces: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := NewServer(Options{Live: func(context.Context) (monitoringapi.Live, error) {
				return monitoringapi.Live{
					CPU:        monitoringapi.LiveCPU{Percent: 50},
					Interfaces: map[string]monitoringapi.LiveInterface{"eth0": {RxBytesPerSec: 1}},
				}, nil
			}})
			handler := srv.HandlerFor(func() time.Duration { return time.Minute }, []string{"metrics"}, tt.plugins)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, monitoringapi.RouteLive, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d", rec.Code)
			}
			var live monitoringapi.Live
			if err := json.Unmarshal(rec.Body.Bytes(), &live); err != nil {
				t.Fatal(err)
			}
			if live.CPU.Percent != 50 || len(live.Interfaces) != tt.wantInterfaces {
				t.Fatalf("filtering failed: %+v", live)
			}
		})
	}
}

func TestSmartRefreshRouteOnlyOnCommandListener(t *testing.T) {
	server := newHTTPTestServer(t)
	interval := func() time.Duration { return time.Minute }

	control := server.HandlerFor(interval, []string{"metrics", "commands"}, nil)
	rec := httptest.NewRecorder()
	control.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/smart/refresh", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"items":[]`)

	metrics := server.HandlerFor(interval, []string{"metrics"}, nil)
	rec = httptest.NewRecorder()
	metrics.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/smart/refresh", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)

	rec = httptest.NewRecorder()
	metrics.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/plugins", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "/api/v1/smart/refresh")
}
