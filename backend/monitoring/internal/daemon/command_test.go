package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apimodel "github.com/mordilloSan/LinuxIO/backend/monitoring/internal/api/model"
	"github.com/mordilloSan/LinuxIO/backend/monitoring/internal/config"
)

func TestApplyConfigSetParamsUpdatesSmartRefreshInterval(t *testing.T) {
	cfg := config.Default()
	raw := "2h"

	restartRequired, err := applyConfigSetParams(&cfg, configSetParams{
		SmartRefreshInterval: &raw,
	})

	require.NoError(t, err)
	assert.False(t, restartRequired)
	assert.Equal(t, 2*time.Hour, cfg.Collector.SmartRefreshInterval.Duration())
}

func TestApplyConfigSetParamsUpdatesHistoryRetention(t *testing.T) {
	cfg := config.Default()
	raw := "336h"

	restartRequired, err := applyConfigSetParams(&cfg, configSetParams{
		HistoryRetention: &raw,
	})

	require.NoError(t, err)
	assert.False(t, restartRequired)
	assert.Equal(t, 14*24*time.Hour, cfg.History.Retention.Duration())
}

func TestApplyConfigSetParamsUpdatesDiskUsageCache(t *testing.T) {
	cfg := config.Default()
	raw := "30m"

	restartRequired, err := applyConfigSetParams(&cfg, configSetParams{
		DiskUsageCache: &raw,
	})

	require.NoError(t, err)
	assert.False(t, restartRequired)
	assert.Equal(t, 30*time.Minute, cfg.Collector.DiskUsageCache.Duration())
}

func TestApplyConfigSetParamsRejectsNegativeDiskUsageCache(t *testing.T) {
	cfg := config.Default()
	raw := "-1s"

	_, err := applyConfigSetParams(&cfg, configSetParams{DiskUsageCache: &raw})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk_usage_cache")
}

func TestApplyConfigSetParamsSplitsHistoryList(t *testing.T) {
	cfg := config.Default()
	raw := " cpu , mem ,, "

	restartRequired, err := applyConfigSetParams(&cfg, configSetParams{History: &raw})

	require.NoError(t, err)
	assert.False(t, restartRequired)
	assert.Equal(t, []string{"cpu", "mem"}, cfg.History.Plugins)
}

func TestApplyConfigSetParamsListenersRequireRestart(t *testing.T) {
	cfg := config.Default()
	listeners := []config.Listener{{Name: "homepage", Address: "0.0.0.0:45876", Plugins: []string{"cpu"}}}

	restartRequired, err := applyConfigSetParams(&cfg, configSetParams{Listeners: &listeners})

	require.NoError(t, err)
	assert.True(t, restartRequired)
	assert.Equal(t, listeners, cfg.Listeners)
}

func TestApplyConfigSetParamsRejectsInvalidSmartRefreshInterval(t *testing.T) {
	cfg := config.Default()
	raw := "0"

	_, err := applyConfigSetParams(&cfg, configSetParams{
		SmartRefreshInterval: &raw,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "smart_refresh_interval")
}

func TestApplyConfigSetParamsRejectsUnknownHistoryPlugin(t *testing.T) {
	cfg := config.Default()
	raw := "cpu,does-not-exist"

	_, err := applyConfigSetParams(&cfg, configSetParams{History: &raw})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "does-not-exist")
}

// The commands exercised here never touch the app, so the executor is built
// without one; app-backed commands are covered by the app package tests.
func newConfigOnlyExecutor(t *testing.T, cfg config.Config) *commandExecutor {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, config.Save(path, cfg))
	executor := NewCommandExecutor(nil, path)
	return executor
}

func TestExecuteCommandConfigGetReturnsFlatView(t *testing.T) {
	cfg := config.Default()
	cfg.Listeners = []config.Listener{{Name: "homepage", Address: "0.0.0.0:45876"}}
	executor := newConfigOnlyExecutor(t, cfg)

	result := executor.ExecuteCommand(context.Background(), apimodel.CommandRequest{Command: " Config.Get "})

	require.Equal(t, http.StatusOK, result.Status)
	require.True(t, result.Response.OK)
	assert.Equal(t, "config.get", result.Response.Command)
	assert.NotEmpty(t, result.Response.RequestID)

	encoded, err := json.Marshal(result.Response.Data)
	require.NoError(t, err)
	var view config.View
	require.NoError(t, json.Unmarshal(encoded, &view))
	assert.Equal(t, config.CurrentVersion, view.Version)
	assert.Equal(t, "1m0s", view.CollectorInterval)
	assert.Equal(t, "1h0m0s", view.SmartRefreshInterval)
	assert.Equal(t, "0s", view.DiskUsageCache)
	assert.Equal(t, "720h0m0s", view.HistoryRetention)
	assert.Equal(t, cfg.HistoryString(), view.History)
	assert.Equal(t, cfg.Listeners, view.Listeners)
}

func TestExecuteCommandListsCommands(t *testing.T) {
	executor := newConfigOnlyExecutor(t, config.Default())

	result := executor.ExecuteCommand(context.Background(), apimodel.CommandRequest{Command: "commands.list", RequestID: "abc"})

	require.Equal(t, http.StatusOK, result.Status)
	assert.Equal(t, "abc", result.Response.RequestID)
	assert.Equal(t, commandList(), result.Response.Data)
}

func TestExecuteCommandRejectsEmptyAndUnknownCommands(t *testing.T) {
	executor := newConfigOnlyExecutor(t, config.Default())

	empty := executor.ExecuteCommand(context.Background(), apimodel.CommandRequest{})
	assert.Equal(t, http.StatusBadRequest, empty.Status)
	require.NotNil(t, empty.Response.Error)
	assert.Equal(t, "invalid_command", empty.Response.Error.Code)

	unknown := executor.ExecuteCommand(context.Background(), apimodel.CommandRequest{Command: "reboot"})
	assert.Equal(t, http.StatusNotFound, unknown.Status)
	require.NotNil(t, unknown.Response.Error)
	assert.Equal(t, "unknown_command", unknown.Response.Error.Code)
}

func TestListenersRestartRequiredSkipsFixedSockets(t *testing.T) {
	cfg := config.Default()
	cfg.Listeners = []config.Listener{{Name: "homepage", Address: "0.0.0.0:45876", Plugins: []string{"cpu"}}}
	active := []apimodel.ListenerMeta{
		{Name: "api", Address: "unix:/run/linuxio/monitoring/api.sock"},
		{Name: "control", Address: "unix:/run/linuxio/monitoring/control.sock"},
		{Name: "homepage", Address: "0.0.0.0:45876", Plugins: []string{"cpu"}},
	}

	assert.False(t, listenersRestartRequired(active, cfg))
	cfg.Listeners[0].Plugins = []string{"mem"}
	assert.True(t, listenersRestartRequired(active, cfg))
	cfg.Listeners[0].Plugins = []string{" CPU "}
	assert.False(t, listenersRestartRequired(active, cfg))
	assert.True(t, listenersRestartRequired(active[:2], cfg))
	assert.True(t, listenersRestartRequired(active, config.Default()))
}

// runWhileConfigLocked holds the executor's config lock, starts fn, and
// reports whether fn finished before the lock was released.
func runWhileConfigLocked[T any](t *testing.T, executor *commandExecutor, fn func() T) (finishedWhileLocked bool, response T) {
	t.Helper()
	executor.configMu.Lock()
	done := make(chan T, 1)
	go func() { done <- fn() }()
	select {
	case response = <-done:
		executor.configMu.Unlock()
		return true, response
	case <-time.After(50 * time.Millisecond):
	}
	executor.configMu.Unlock()
	select {
	case response = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not finish after the config lock was released")
	}
	return false, response
}

func TestHandleConfigSetWaitsForTheConfigLock(t *testing.T) {
	executor := newConfigOnlyExecutor(t, config.Default())
	// An interval of zero fails validation after the file is loaded, so the
	// handler exercises its locked section without reaching the runtime reload.
	req := apimodel.CommandRequest{Command: "config.set", Params: json.RawMessage(`{"collector_interval":"0s"}`)}

	finishedWhileLocked, response := runWhileConfigLocked(t, executor, func() apimodel.CommandResponse {
		return executor.handleConfigSet(context.Background(), req).Response
	})

	assert.False(t, finishedWhileLocked, "config.set must not touch the config while another caller holds the lock")
	require.NotNil(t, response.Error)
	assert.Equal(t, "invalid_config", response.Error.Code)
}

func TestHandleConfigReloadWaitsForTheConfigLock(t *testing.T) {
	// An unknown key fails the strict decode after the file is read, so the
	// handler exercises its locked section without reaching the runtime reload.
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("not_a_key: true\n"), 0o600))
	executor := NewCommandExecutor(nil, path)
	req := apimodel.CommandRequest{Command: "config.reload"}

	finishedWhileLocked, response := runWhileConfigLocked(t, executor, func() apimodel.CommandResponse {
		return executor.handleConfigReload(req).Response
	})

	assert.False(t, finishedWhileLocked, "config.reload must not touch the config while another caller holds the lock")
	require.NotNil(t, response.Error)
	assert.Equal(t, "invalid_config", response.Error.Code)
}

func TestReloadFromFileWaitsForTheConfigLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("not_a_key: true\n"), 0o600))
	executor := NewCommandExecutor(nil, path)

	finishedWhileLocked, err := runWhileConfigLocked(t, executor, executor.reloadFromFile)

	assert.False(t, finishedWhileLocked, "SIGHUP reload must not touch the config while another caller holds the lock")
	require.Error(t, err)
}

func TestApplyConfigSetParamsReplacesHistoryIntervals(t *testing.T) {
	cfg := config.Default()
	cfg.History.Intervals = map[string]config.Duration{"cpu": config.Duration(2 * time.Minute)}
	intervals := map[string]string{"containers": "5m"}

	restartRequired, err := applyConfigSetParams(&cfg, configSetParams{HistoryIntervals: &intervals})

	require.NoError(t, err)
	assert.False(t, restartRequired)
	assert.Equal(t, map[string]config.Duration{"containers": config.Duration(5 * time.Minute)}, cfg.History.Intervals)

	empty := map[string]string{}
	_, err = applyConfigSetParams(&cfg, configSetParams{HistoryIntervals: &empty})
	require.NoError(t, err)
	assert.Empty(t, cfg.History.Intervals)
}

func TestApplyConfigSetParamsRejectsHistoryIntervalBelowTick(t *testing.T) {
	cfg := config.Default()
	intervals := map[string]string{"cpu": "30s"}

	_, err := applyConfigSetParams(&cfg, configSetParams{HistoryIntervals: &intervals})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "history.intervals")
}
